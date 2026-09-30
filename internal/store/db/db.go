// Package db is ketju's access to the embedded SQLite database: opening it,
// running migrations, and the read and write transactions everything else goes
// through.
//
// The database holds coordination and projections, never post content
// (docs/04). Queries are written as .sql files under queries/ and compiled by
// sqlc into package dbq, which speaks only database/sql. The view queries whose
// shape depends on a lens or a cursor are the one exception, and they live in
// views/. Nothing outside this package imports a SQLite driver.
package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// ErrBusy is returned when SQLite reports the database locked for longer than
// the busy timeout. With a single write connection this means another process
// holds the file, or a read transaction ran long enough to block a checkpoint.
// It is retryable.
var ErrBusy = errors.New("db: database is busy")

// busyTimeoutMS is how long a connection waits on a lock before SQLITE_BUSY.
// Five seconds was settled in grooming (TKT-01M29BW9Q1). In-process writers
// queue on the single write connection rather than here, so this bounds only
// contention with another process and read transactions against checkpoints.
const busyTimeoutMS = 5000

// Every connection gets these, in this order: busy_timeout first so the
// others can wait on a lock, then WAL, foreign keys, and FULL sync. FULL
// rather than NORMAL because in WAL mode NORMAL can lose the latest commits on
// power loss, and this file holds authoritative tables (docs/04, "Engine").
var commonQuery = fmt.Sprintf("?_pragma=busy_timeout(%d)"+
	"&_pragma=journal_mode(wal)"+
	"&_pragma=foreign_keys(on)"+
	"&_pragma=synchronous(full)", busyTimeoutMS)

const (
	// writeQuery makes every BEGIN on the write pool a BEGIN IMMEDIATE, so a
	// write transaction holds the lock from its first statement and never
	// fails partway through trying to upgrade a read lock.
	writeQuery = "&_txlock=immediate"

	// readQuery makes a write through Read fail at once, instead of taking
	// the write lock from a pool that was never meant to hold it.
	readQuery = "&_pragma=query_only(1)"
)

// Read pool bounds. docs/04 caps the pool at 4 to 8 connections.
const (
	minReadConns     = 4
	maxReadConns     = 8
	defaultReadConns = minReadConns
)

// Options configures Open.
type Options struct {
	// Path is the database file, conventionally data/db/ketju.db. Its
	// directory is created if it does not exist.
	Path string

	// ReadConns is the size of the read pool. Zero means 4; anything else is
	// clamped to 4..8.
	ReadConns int

	// Trace, when set, is called with the text of every statement SQLite
	// starts on either pool, BEGIN and COMMIT included. It is for tests that
	// hold a handler to a query budget: counting what SQLite actually ran is
	// the only count a refactor cannot quietly get wrong. Nil costs nothing.
	Trace func(sql string)
}

// DB is an open database. It is safe for concurrent use.
type DB struct {
	path  string
	read  *sql.DB
	write *sql.DB
}

// syncDir makes a directory's entries durable. It is a variable so a test can
// observe the call; see TestOpenSyncsDirectoryAfterWAL.
var syncDir = func(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

// Open opens the database at opts.Path, creating it if needed. It does not run
// migrations; call Migrate before the first query.
//
// Open returns only once the WAL file exists and the data directory has been
// fsynced, so that no authoritative write can land in a WAL whose directory
// entry is not yet durable (TKT-01M392DP19).
func Open(ctx context.Context, opts Options) (*DB, error) {
	if opts.Path == "" {
		return nil, errors.New("db: Options.Path is empty")
	}
	dir := filepath.Dir(opts.Path)
	if err := makeDirDurable(dir); err != nil {
		return nil, err
	}

	write, err := openPool(opts.Path, commonQuery+writeQuery, opts.Trace)
	if err != nil {
		return nil, fmt.Errorf("db: opening %s: %w", opts.Path, err)
	}
	// One write connection, kept open for the life of the DB. In-process
	// writers queue for it in Go. TKT-01M29BSPE8 found that several ncruces
	// connections contending for the lock starve each other for seconds.
	write.SetMaxOpenConns(1)
	write.SetMaxIdleConns(1)
	write.SetConnMaxLifetime(0)
	write.SetConnMaxIdleTime(0)

	if err := ensureWAL(ctx, write, opts.Path); err != nil {
		write.Close()
		return nil, err
	}
	if err := syncDir(dir); err != nil {
		write.Close()
		return nil, fmt.Errorf("db: syncing %s: %w", dir, err)
	}

	read, err := openPool(opts.Path, commonQuery+readQuery, opts.Trace)
	if err != nil {
		write.Close()
		return nil, fmt.Errorf("db: opening %s for reading: %w", opts.Path, err)
	}
	n := opts.ReadConns
	switch {
	case n == 0:
		n = defaultReadConns
	case n < minReadConns:
		n = minReadConns
	case n > maxReadConns:
		n = maxReadConns
	}
	read.SetMaxOpenConns(n)
	read.SetMaxIdleConns(n)

	return &DB{path: opts.Path, read: read, write: write}, nil
}

// ErrReadOnly is Write on a database opened with OpenReadOnly.
var ErrReadOnly = errors.New("db: the database was opened read-only")

// readOnlyQuery opens the file read-only and keeps every statement a query.
// It sets no journal_mode, synchronous or foreign_keys pragma: setting a
// journal mode is a write, and the other two govern writes this handle never
// makes.
var readOnlyQuery = fmt.Sprintf("?mode=ro&_pragma=busy_timeout(%d)", busyTimeoutMS) + readQuery

// OpenReadOnly opens an existing database for reading only, for a tool such
// as `ketju fsck` whose contract is that it writes nothing (TKT-01M3A054TR).
//
// Unlike Open it creates no directory and no file, fsyncs nothing, has no
// write connection, and does not checkpoint when closed. A path that does
// not exist is refused rather than created. It runs no migrations and does
// not check the schema: a query against a table the file lacks fails.
//
// ketju.db itself is never written. SQLite may create the -wal and -shm files
// beside a database that has none, because it cannot read a WAL database
// without them; they hold no data. Write on the result returns ErrReadOnly.
func OpenReadOnly(ctx context.Context, path string) (*DB, error) {
	if path == "" {
		return nil, errors.New("db: OpenReadOnly: the path is empty")
	}
	fi, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("db: opening %s read-only: %w", path, err)
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("db: opening %s read-only: not a regular file", path)
	}
	read, err := openPool(path, readOnlyQuery, nil)
	if err != nil {
		return nil, fmt.Errorf("db: opening %s read-only: %w", path, err)
	}
	read.SetMaxOpenConns(defaultReadConns)
	read.SetMaxIdleConns(defaultReadConns)
	d := &DB{path: path, read: read}
	// Opening a pool connects nothing; this is where a file that is not a
	// database says so.
	if err := d.Ping(ctx); err != nil {
		read.Close()
		return nil, fmt.Errorf("db: opening %s read-only: %w", path, err)
	}
	return d, nil
}

// makeDirDurable creates dir and any missing parents, then fsyncs the parent
// of each directory it created, so the new directory entries survive a power
// loss along with whatever is later written inside them. A directory that
// already existed is left alone.
func makeDirDurable(dir string) error {
	dir = filepath.Clean(dir)
	var created []string
	for d := dir; ; d = filepath.Dir(d) {
		if _, err := os.Stat(d); err == nil {
			break
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("db: checking %s: %w", d, err)
		}
		created = append(created, d)
		if filepath.Dir(d) == d {
			break
		}
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("db: creating %s: %w", dir, err)
	}
	// Outermost first, so each parent is durable before its child is relied on.
	for i := len(created) - 1; i >= 0; i-- {
		parent := filepath.Dir(created[i])
		if err := syncDir(parent); err != nil {
			return fmt.Errorf("db: syncing %s: %w", parent, err)
		}
	}
	return nil
}

// ensureWAL confirms the write connection is in WAL mode and that the -wal
// file exists on disk, reading from the database if that is what it takes to
// make SQLite create it.
func ensureWAL(ctx context.Context, write *sql.DB, path string) error {
	var mode string
	if err := write.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode); err != nil {
		return fmt.Errorf("db: reading journal_mode: %w", wrap(err))
	}
	if mode != "wal" {
		return fmt.Errorf("db: journal_mode is %q, want wal", mode)
	}
	// SQLite opens the WAL on the first transaction, not on connect.
	var n int
	if err := write.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_schema").Scan(&n); err != nil {
		return fmt.Errorf("db: first read: %w", wrap(err))
	}
	if _, err := os.Stat(path + "-wal"); err != nil {
		return fmt.Errorf("db: WAL file not created: %w", err)
	}
	return nil
}

// Read runs fn in a read-only transaction on the read pool.
func (d *DB) Read(ctx context.Context, fn func(q *dbq.Queries, tx *sql.Tx) error) error {
	tx, err := d.read.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return wrap(err)
	}
	defer tx.Rollback()
	if err := fn(dbq.New(tx), tx); err != nil {
		return wrap(err)
	}
	return wrap(tx.Commit())
}

// Ping runs one statement on the read pool and reports whether the database
// answered it, for the health check (docs/02 "Observability").
//
// The statement reads sqlite_schema rather than being a bare SELECT 1. A
// constant is answered without opening a read transaction on the file, so it
// passes for a database whose file can no longer be read; reading the schema
// table cannot. It is one statement with no BEGIN or COMMIT around it, which
// is the whole of healthz's database budget.
func (d *DB) Ping(ctx context.Context) error {
	var n int
	return wrap(d.read.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_schema").Scan(&n))
}

// Write runs fn in a write transaction on the single write connection. The
// transaction starts with BEGIN IMMEDIATE and commits if fn returns nil.
func (d *DB) Write(ctx context.Context, fn func(q *dbq.Queries, tx *sql.Tx) error) error {
	if d.write == nil {
		return ErrReadOnly
	}
	tx, err := d.write.BeginTx(ctx, nil)
	if err != nil {
		return wrap(err)
	}
	defer tx.Rollback()
	if err := fn(dbq.New(tx), tx); err != nil {
		return wrap(err)
	}
	return wrap(tx.Commit())
}

// Close checkpoints the WAL into the main file and closes both pools. The
// -wal file is truncated, not removed, because every connection persists it.
//
// The read pool closes first, so none of this DB's own readers can hold the
// checkpoint back. A reader on another handle still can: SQLite reports that
// in the checkpoint's result row rather than as an error, and Close returns
// it as ErrBusy. The pools are closed either way, and nothing is lost: the
// WAL is replayed on the next open.
//
// A database from OpenReadOnly has no write connection and is not
// checkpointed: Close only closes its read pool.
func (d *DB) Close() error {
	readErr := d.read.Close()
	if d.write == nil {
		return readErr
	}
	var busy, logFrames, checkpointed int64
	err := d.write.QueryRow("PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &logFrames, &checkpointed)
	if err == nil && busy != 0 {
		err = fmt.Errorf("%w: closing: the WAL checkpoint was blocked by a reader (%d of %d frames checkpointed)",
			ErrBusy, checkpointed, logFrames)
	}
	return errors.Join(wrap(err), readErr, d.write.Close())
}

// wrap marks SQLITE_BUSY as ErrBusy, keeping the driver's error in the chain.
func wrap(err error) error {
	if err != nil && isBusy(err) && !errors.Is(err, ErrBusy) {
		return fmt.Errorf("%w: %w", ErrBusy, err)
	}
	return err
}
