package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// TestSnapshotIntoBesideAWriter: while another goroutine commits on the
// write connection, SnapshotInto writes a file that opens on its own with
// no -wal beside it, passes integrity_check, and holds the search tables
// and every row committed before it began (TKT-01M29CXK35).
func TestSnapshotIntoBesideAWriter(t *testing.T) {
	d, _ := openTest(t)
	ctx := context.Background()
	insert := func(n int64) error {
		return d.Write(ctx, func(_ *dbq.Queries, tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, `INSERT INTO settings (key, value_json, updated_at) VALUES (?, '1', 1)`,
				fmt.Sprintf("k%d", n))
			return err
		})
	}
	for i := range int64(10) {
		if err := insert(i); err != nil {
			t.Fatal(err)
		}
	}
	stop := make(chan struct{})
	var wrote atomic.Int64
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := int64(10); ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			if err := insert(i); err != nil {
				t.Error(err)
				return
			}
			wrote.Add(1)
		}
	}()
	if err := d.CheckpointPassive(ctx); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "snap.db")
	if err := d.SnapshotInto(ctx, dest); err != nil {
		t.Fatal(err)
	}
	close(stop)
	wg.Wait()
	if wrote.Load() == 0 {
		t.Log("the writer committed nothing during the snapshot; the test still checks the copy")
	}
	if _, err := os.Stat(dest + "-wal"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("the snapshot has a WAL beside it (%v); it must open on its own", err)
	}
	if err := d.SnapshotInto(ctx, dest); err == nil {
		t.Error("SnapshotInto overwrote an existing file")
	}

	s, err := OpenReadOnly(ctx, dest)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var check string
	var settings, search int
	if err := s.Read(ctx, func(_ *dbq.Queries, tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&check); err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM settings WHERE key LIKE 'k%'`).Scan(&settings); err != nil {
			return err
		}
		return tx.QueryRowContext(ctx, `SELECT count(*) FROM search`).Scan(&search)
	}); err != nil {
		t.Fatal(err)
	}
	if check != "ok" {
		t.Errorf("integrity_check = %q", check)
	}
	if settings < 10 {
		t.Errorf("the snapshot holds %d of the 10 rows committed before it", settings)
	}
}

// TestSnapshotIntoFromReadOnly: a database opened read-only, as `ketju
// backup` opens a stopped data directory's, snapshots too, and its file is
// not written.
func TestSnapshotIntoFromReadOnly(t *testing.T) {
	d, path := openTest(t)
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	r, err := OpenReadOnly(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	dest := filepath.Join(t.TempDir(), "snap.db")
	if err := r.SnapshotInto(t.Context(), dest); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Error("snapshotting changed the source file")
	}
	if err := r.CheckpointPassive(t.Context()); !errors.Is(err, ErrReadOnly) {
		t.Errorf("CheckpointPassive on a read-only database = %v, want ErrReadOnly", err)
	}
}

// TestSnapshotIntoCancelled: a cancelled context leaves no file behind.
func TestSnapshotIntoCancelled(t *testing.T) {
	d, _ := openTest(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	dest := filepath.Join(t.TempDir(), "snap.db")
	if err := d.SnapshotInto(ctx, dest); err == nil {
		t.Fatal("a cancelled snapshot succeeded")
	}
	if _, err := os.Stat(dest); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("a cancelled snapshot left %s (%v)", dest, err)
	}
}
