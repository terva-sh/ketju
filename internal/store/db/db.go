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
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("db: creating %s: %w", dir, err)
	}

	write, err := openPool(opts.Path, commonQuery+writeQuery)
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

	read, err := openPool(opts.Path, commonQuery+readQuery)
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

// Write runs fn in a write transaction on the single write connection. The
// transaction starts with BEGIN IMMEDIATE and commits if fn returns nil.
func (d *DB) Write(ctx context.Context, fn func(q *dbq.Queries, tx *sql.Tx) error) error {
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
func (d *DB) Close() error {
	readErr := d.read.Close()
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
