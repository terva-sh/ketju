package db

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// openTest opens and migrates a fresh database in a temporary directory.
func openTest(t *testing.T) (*DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "db", "ketju.db")
	d, err := Open(t.Context(), Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if err := d.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	return d, path
}

// openUnmigrated opens a fresh database without running the embedded
// migrations, for tests that drive the runner with their own sets.
func openUnmigrated(t *testing.T) (*DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "db", "ketju.db")
	d, err := Open(t.Context(), Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d, path
}

// TestOpenSetsPragmas is AC1: WAL and an explicit busy_timeout, read back
// from a live connection in each pool rather than trusted from the DSN.
func TestOpenSetsPragmas(t *testing.T) {
	d, _ := openTest(t)
	want := map[string]int64{
		"busy_timeout": busyTimeoutMS,
		"foreign_keys": 1,
		"synchronous":  2, // FULL
	}
	for name, pool := range map[string]*sql.DB{"read": d.read, "write": d.write} {
		t.Run(name, func(t *testing.T) {
			var mode string
			if err := pool.QueryRowContext(t.Context(), "PRAGMA journal_mode").Scan(&mode); err != nil {
				t.Fatal(err)
			}
			if mode != "wal" {
				t.Errorf("journal_mode = %q, want wal", mode)
			}
			for pragma, v := range want {
				var got int64
				if err := pool.QueryRowContext(t.Context(), "PRAGMA "+pragma).Scan(&got); err != nil {
					t.Fatal(err)
				}
				if got != v {
					t.Errorf("%s = %d, want %d", pragma, got, v)
				}
			}
			var queryOnly int64
			if err := pool.QueryRowContext(t.Context(), "PRAGMA query_only").Scan(&queryOnly); err != nil {
				t.Fatal(err)
			}
			if wantRO := int64(map[string]int{"read": 1, "write": 0}[name]); queryOnly != wantRO {
				t.Errorf("query_only = %d, want %d", queryOnly, wantRO)
			}
		})
	}
}

func TestReadPoolClamp(t *testing.T) {
	for _, c := range []struct{ in, want int }{{0, 4}, {1, 4}, {6, 6}, {64, 8}} {
		path := filepath.Join(t.TempDir(), "ketju.db")
		d, err := Open(t.Context(), Options{Path: path, ReadConns: c.in})
		if err != nil {
			t.Fatal(err)
		}
		if got := d.read.Stats().MaxOpenConnections; got != c.want {
			t.Errorf("ReadConns %d gave a read pool of %d, want %d", c.in, got, c.want)
		}
		if got := d.write.Stats().MaxOpenConnections; got != 1 {
			t.Errorf("write pool has %d connections, want 1", got)
		}
		d.Close()
	}
}

// TestReadRefusesWrites checks that a write through Read fails rather than
// taking the write lock from the read pool.
func TestReadRefusesWrites(t *testing.T) {
	d, _ := openTest(t)
	err := d.Read(t.Context(), func(q *dbq.Queries, tx *sql.Tx) error {
		_, err := tx.ExecContext(t.Context(), "CREATE TABLE nope (x INTEGER)")
		return err
	})
	if err == nil {
		t.Fatal("a CREATE TABLE through Read succeeded")
	}
}

// TestConcurrentWritersSerialize is AC2. Read-modify-write increments race
// unless every transaction holds the write lock from BEGIN, so an exact final
// count means BEGIN IMMEDIATE did its job; any SQLITE_BUSY fails the test.
//
// Two variants. "one handle" is the normal case, where writers queue for the
// single write connection in Go. "two handles" opens the file twice, so two
// write connections really do contend inside SQLite, and only BEGIN IMMEDIATE
// plus busy_timeout keep the count right.
func TestConcurrentWritersSerialize(t *testing.T) {
	increment := func(ctx context.Context, d *DB) error {
		return d.Write(ctx, func(q *dbq.Queries, tx *sql.Tx) error {
			var n int64
			if err := tx.QueryRowContext(ctx, "SELECT n FROM counter").Scan(&n); err != nil {
				return err
			}
			_, err := tx.ExecContext(ctx, "UPDATE counter SET n = ?", n+1)
			return err
		})
	}
	setup := func(t *testing.T, d *DB) {
		err := d.Write(t.Context(), func(q *dbq.Queries, tx *sql.Tx) error {
			_, err := tx.ExecContext(t.Context(), "CREATE TABLE counter (n INTEGER NOT NULL); INSERT INTO counter VALUES (0)")
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	count := func(t *testing.T, d *DB) int64 {
		var n int64
		err := d.Read(t.Context(), func(q *dbq.Queries, tx *sql.Tx) error {
			return tx.QueryRowContext(t.Context(), "SELECT n FROM counter").Scan(&n)
		})
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	run := func(t *testing.T, handles []*DB, perHandle, perWorker int) {
		var wg sync.WaitGroup
		errs := make(chan error, len(handles)*perHandle)
		for _, d := range handles {
			for range perHandle {
				wg.Go(func() {
					for range perWorker {
						if err := increment(t.Context(), d); err != nil {
							errs <- err
							return
						}
					}
				})
			}
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			t.Fatalf("writer failed: %v (busy: %v)", err, errors.Is(err, ErrBusy))
		}
	}

	t.Run("one handle", func(t *testing.T) {
		d, _ := openTest(t)
		setup(t, d)
		run(t, []*DB{d}, 32, 10)
		if got := count(t, d); got != 32*10 {
			t.Fatalf("counter = %d, want %d", got, 32*10)
		}
	})

	t.Run("two handles", func(t *testing.T) {
		d1, path := openTest(t)
		setup(t, d1)
		d2, err := Open(t.Context(), Options{Path: path})
		if err != nil {
			t.Fatal(err)
		}
		defer d2.Close()
		run(t, []*DB{d1, d2}, 4, 10)
		if got := count(t, d1); got != 2*4*10 {
			t.Fatalf("counter = %d, want %d", got, 2*4*10)
		}
	})
}

// TestBusyIsWrapped checks the busy classification against a real
// SQLITE_BUSY: a second write connection with no busy timeout, while the first
// holds the lock.
func TestBusyIsWrapped(t *testing.T) {
	_, path := openTest(t)
	holder, err := openPool(path, "?_txlock=immediate")
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Close()
	tx, err := holder.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	other, err := openPool(path, "?_pragma=busy_timeout(0)&_txlock=immediate")
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	_, err = other.BeginTx(t.Context(), nil)
	if err == nil {
		t.Fatal("a second BEGIN IMMEDIATE succeeded while the first was open")
	}
	if !errors.Is(wrap(err), ErrBusy) {
		t.Fatalf("wrap(%v) is not ErrBusy", err)
	}
	if !errors.Is(wrap(wrap(err)), ErrBusy) || wrap(errors.New("x")) == nil {
		t.Fatal("wrap is not idempotent, or swallowed an unrelated error")
	}
	if errors.Is(wrap(errors.New("disk I/O error")), ErrBusy) {
		t.Fatal("an unrelated error was classified as busy")
	}
}

// TestOpenSyncsDirectoryAfterWAL is AC8: before Open returns, and so before
// any authoritative write, the data directory is fsynced, and the WAL file
// already exists when that happens. It also checks that the WAL survives a
// close, which is what makes one sync at open enough (TKT-01M392DP19).
func TestOpenSyncsDirectoryAfterWAL(t *testing.T) {
	// The directory exists already, so the only sync is the one after the
	// WAL. TestOpenSyncsCreatedParents covers a directory Open creates.
	dir := t.TempDir()
	path := filepath.Join(dir, "ketju.db")

	var synced []string
	var walAtSync []bool
	orig := syncDir
	syncDir = func(d string) error {
		synced = append(synced, d)
		_, err := os.Stat(path + "-wal")
		walAtSync = append(walAtSync, err == nil)
		return orig(d)
	}
	t.Cleanup(func() { syncDir = orig })

	d, err := Open(t.Context(), Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	if len(synced) != 1 || synced[0] != dir {
		t.Fatalf("directories synced during Open = %v, want exactly [%s]", synced, dir)
	}
	if !walAtSync[0] {
		t.Fatal("the directory was synced before the WAL file existed, so the sync did not cover it")
	}

	if err := d.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + "-wal"); err != nil {
		t.Fatalf("the WAL file did not survive Close, so a later open would recreate it unsynced: %v", err)
	}
}

func TestOpenPropagatesSyncFailure(t *testing.T) {
	orig := syncDir
	syncDir = func(string) error { return errors.New("injected") }
	t.Cleanup(func() { syncDir = orig })
	if _, err := Open(t.Context(), Options{Path: filepath.Join(t.TempDir(), "ketju.db")}); err == nil {
		t.Fatal("Open succeeded although the directory sync failed")
	}
}

// TestOpenTakesAnyPath: characters that mean something in a URI are part of
// the filename, and the database lands exactly where Options.Path says.
func TestOpenTakesAnyPath(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "odd ?dir#name%41")
	path := filepath.Join(dir, "k?e#y%2F.db")
	d, err := Open(t.Context(), Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{path, path + "-wal"} {
		if _, err := os.Stat(f); err != nil {
			t.Errorf("expected %q to exist: %v", f, err)
		}
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if n := e.Name(); n != filepath.Base(path) && n != filepath.Base(path)+"-wal" && n != filepath.Base(path)+"-shm" {
			t.Errorf("unexpected file %q in %s: the path was misread", n, dir)
		}
	}
}

// TestCloseReportsABlockedCheckpoint: a reader on another handle blocks the
// TRUNCATE checkpoint, which SQLite reports in a result row, not an error.
// Close must say so rather than claim the WAL was folded in. It waits out the
// busy timeout first, so this test takes about five seconds.
func TestCloseReportsABlockedCheckpoint(t *testing.T) {
	d, path := openTest(t)
	err := d.Write(t.Context(), func(q *dbq.Queries, tx *sql.Tx) error {
		_, err := tx.ExecContext(t.Context(), "CREATE TABLE t (x INTEGER); INSERT INTO t VALUES (1)")
		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	reader, err := openPool(path, "")
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	tx, err := reader.BeginTx(t.Context(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var x int
	if err := tx.QueryRowContext(t.Context(), "SELECT x FROM t").Scan(&x); err != nil {
		t.Fatal(err)
	}

	err = d.Close()
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("Close with a reader holding the WAL = %v, want ErrBusy", err)
	}
}

// TestCloseCheckpointsTheWAL: with no reader in the way, Close leaves the
// -wal file empty, so everything written is in the main file.
func TestCloseCheckpointsTheWAL(t *testing.T) {
	d, path := openTest(t)
	err := d.Write(t.Context(), func(q *dbq.Queries, tx *sql.Tx) error {
		_, err := tx.ExecContext(t.Context(), "CREATE TABLE t (x INTEGER); INSERT INTO t VALUES (1)")
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path + "-wal")
	if err != nil {
		t.Fatal(err)
	}
	if fi.Size() != 0 {
		t.Fatalf("-wal is %d bytes after Close, want 0", fi.Size())
	}
}

// TestOpenSyncsCreatedParents: when Open creates the data directory, the
// parent of each directory it made is fsynced first, so the new entries are
// durable, and the data directory itself is synced last, after the WAL.
func TestOpenSyncsCreatedParents(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "a", "b")
	path := filepath.Join(dir, "ketju.db")

	var synced []string
	orig := syncDir
	syncDir = func(d string) error { synced = append(synced, d); return orig(d) }
	t.Cleanup(func() { syncDir = orig })

	d, err := Open(t.Context(), Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	want := []string{root, filepath.Join(root, "a"), dir}
	if !slices.Equal(synced, want) {
		t.Fatalf("synced %v, want %v", synced, want)
	}
}
