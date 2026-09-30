package db

import (
	"crypto/sha256"
	"database/sql"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// fileState is what a read-only open must leave unchanged about a file.
type fileState struct {
	size  int64
	mtime int64
	sum   [sha256.Size]byte
}

func stateOf(t *testing.T, path string) fileState {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return fileState{fi.Size(), fi.ModTime().UnixNano(), sha256.Sum256(b)}
}

// TestOpenReadOnly: OpenReadOnly reads what a writer committed, refuses
// every write, creates nothing where there is no database, and leaves the
// database file byte for byte as it was, over a WAL a live writer holds open
// as well as over a cleanly closed one (TKT-01M3A054TR).
func TestOpenReadOnly(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "db")
		if _, err := OpenReadOnly(t.Context(), filepath.Join(dir, "ketju.db")); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("err = %v, want one wrapping fs.ErrNotExist", err)
		}
		if _, err := os.Stat(dir); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("opening a missing database read-only created %s (stat err %v)", dir, err)
		}
	})

	t.Run("not a database", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "ketju.db")
		if err := os.WriteFile(path, []byte("not a database, and long enough to read a header from"), 0o600); err != nil {
			t.Fatal(err)
		}
		before := stateOf(t, path)
		if d, err := OpenReadOnly(t.Context(), path); err == nil {
			d.Close()
			t.Fatal("a file that is not a database opened")
		}
		if stateOf(t, path) != before {
			t.Error("a failed read-only open changed the file")
		}
	})

	for _, live := range []bool{false, true} {
		name := "closed"
		if live {
			name = "beside a live writer"
		}
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "db", "ketju.db")
			w, err := Open(t.Context(), Options{Path: path})
			if err != nil {
				t.Fatal(err)
			}
			if err := w.Migrate(t.Context()); err != nil {
				t.Fatal(err)
			}
			put := func(id, length int64) {
				t.Helper()
				err := w.Write(t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
					return q.SetProjectionState(t.Context(), dbq.SetProjectionStateParams{ThreadID: id, LogLength: length, UpdatedAt: 1})
				})
				if err != nil {
					t.Fatal(err)
				}
			}
			put(1, 100)
			if !live {
				if err := w.Close(); err != nil {
					t.Fatal(err)
				}
			} else {
				t.Cleanup(func() { w.Close() })
				put(2, 200) // left in the WAL: nothing checkpoints it
			}
			before := stateOf(t, path)

			d, err := OpenReadOnly(t.Context(), path)
			if err != nil {
				t.Fatal(err)
			}
			var n int
			err = d.Read(t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
				rows, err := q.ListProjectionLengths(t.Context())
				n = len(rows)
				return err
			})
			if err != nil {
				t.Fatal(err)
			}
			if want := map[bool]int{false: 1, true: 2}[live]; n != want {
				t.Errorf("read %d projection_state rows, want %d", n, want)
			}

			if err := d.Write(t.Context(), func(*dbq.Queries, *sql.Tx) error { return nil }); !errors.Is(err, ErrReadOnly) {
				t.Errorf("Write = %v, want ErrReadOnly", err)
			}
			if err := d.Migrate(t.Context()); !errors.Is(err, ErrReadOnly) {
				t.Errorf("Migrate = %v, want ErrReadOnly", err)
			}
			err = d.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
				_, err := tx.ExecContext(t.Context(), "DELETE FROM projection_state")
				return err
			})
			if err == nil {
				t.Error("a DELETE through a read-only database succeeded")
			}
			// query_only is a pragma a statement could turn off; mode=ro is
			// the open itself, and holds regardless.
			err = d.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
				if _, err := tx.ExecContext(t.Context(), "PRAGMA query_only = 0"); err != nil {
					return err
				}
				_, err := tx.ExecContext(t.Context(), "DELETE FROM projection_state")
				return err
			})
			if err == nil {
				t.Error("a DELETE succeeded once query_only was turned off; the file is not open read-only")
			}
			if err := d.Close(); err != nil {
				t.Errorf("Close: %v", err)
			}
			if got := stateOf(t, path); got != before {
				t.Errorf("the database file changed under a read-only open: %+v, was %+v", got, before)
			}
		})
	}
}
