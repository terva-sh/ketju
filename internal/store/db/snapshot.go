package db

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// snapshotQuery opens the database read-only for VACUUM INTO. It sets no
// query_only pragma, which could refuse the statement, and no journal mode,
// which would be a write.
var snapshotQuery = fmt.Sprintf("?mode=ro&_pragma=busy_timeout(%d)", busyTimeoutMS)

// SnapshotInto writes a consistent copy of the database to dest with
// SQLite's VACUUM INTO (TKT-01M29CXK35). The copy is one read transaction's
// view, so it is consistent with itself whatever other connections commit
// meanwhile, and it holds no WAL: it opens on its own.
//
// It runs on a connection opened for it alone, read-only and outside any
// transaction. The write connection would make every writer wait for the
// copy, and the read pool's query_only connections may refuse the
// statement. The connection registers FTS5, as every pool does, so the
// search tables are copied too. It works on a database from OpenReadOnly.
//
// dest must not exist, and its directory must. The file and its directory
// are fsynced before SnapshotInto returns. Cancelling ctx interrupts the
// copy, and a partial file is removed.
func (d *DB) SnapshotInto(ctx context.Context, dest string) (err error) {
	if _, err := os.Lstat(dest); err == nil {
		return fmt.Errorf("db: snapshot: %s already exists", dest)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("db: snapshot: %w", err)
	}
	pool, err := openPool(d.path, snapshotQuery, nil)
	if err != nil {
		return fmt.Errorf("db: snapshot: opening %s: %w", d.path, err)
	}
	defer pool.Close()
	pool.SetMaxOpenConns(1)
	defer func() {
		if err != nil {
			os.Remove(dest)
		}
	}()
	if _, err := pool.ExecContext(ctx, "VACUUM INTO ?", dest); err != nil {
		return fmt.Errorf("db: snapshot into %s: %w", dest, wrap(err))
	}
	f, err := os.Open(dest)
	if err != nil {
		return fmt.Errorf("db: snapshot: %w", err)
	}
	serr := f.Sync()
	if cerr := f.Close(); serr == nil {
		serr = cerr
	}
	if serr != nil {
		return fmt.Errorf("db: snapshot: syncing %s: %w", dest, serr)
	}
	if err := syncDir(filepath.Dir(dest)); err != nil {
		return fmt.Errorf("db: snapshot: syncing %s: %w", filepath.Dir(dest), err)
	}
	return nil
}

// CheckpointPassive copies what it can of the WAL into the main file without
// waiting on any reader or writer (wal_checkpoint(PASSIVE)). A checkpoint
// that stops short costs a snapshot nothing, because VACUUM INTO reads
// through the WAL; it only keeps the WAL from growing while appends wait.
func (d *DB) CheckpointPassive(ctx context.Context) error {
	if d.write == nil {
		return ErrReadOnly
	}
	var busy, logFrames, checkpointed int64
	return wrap(d.write.QueryRowContext(ctx, "PRAGMA wal_checkpoint(PASSIVE)").Scan(&busy, &logFrames, &checkpointed))
}
