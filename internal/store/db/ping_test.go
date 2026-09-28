package db

import (
	"os"
	"path/filepath"
	"testing"
)

// Ping reads the file, which is why it is not a bare SELECT 1: with the
// database's first page overwritten, SELECT 1 still answers and Ping does not.
// The WAL is checkpointed first, or SQLite would read the page from there.
func TestPingReadsTheFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "ketju.db")
	d, err := Open(t.Context(), Options{Path: p})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := d.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := d.Ping(t.Context()); err != nil {
		t.Fatalf("Ping on a healthy database: %v", err)
	}
	if _, err := d.write.Exec("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(p, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteAt(make([]byte, 4096), 0); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	var n int
	if err := d.read.QueryRowContext(t.Context(), "SELECT 1").Scan(&n); err != nil {
		t.Fatalf("SELECT 1 failed too, so this test no longer shows why Ping reads the schema: %v", err)
	}
	if err := d.Ping(t.Context()); err == nil {
		t.Error("Ping passed on a database whose first page is zeroes")
	}
}

func TestPingOnAClosedDatabase(t *testing.T) {
	d, err := Open(t.Context(), Options{Path: filepath.Join(t.TempDir(), "ketju.db")})
	if err != nil {
		t.Fatal(err)
	}
	d.Close()
	if err := d.Ping(t.Context()); err == nil {
		t.Error("Ping passed on a closed database")
	}
}
