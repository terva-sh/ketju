package dbtest

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/terva-sh/ketju/internal/store/db"
)

// Open returns a new database that is exactly what db.Open followed by
// Migrate makes, without running a migration. The test's cleanup closes it.
//
// Every migration runs once per test binary, into a template database that
// is then closed, so that its WAL is checkpointed into the main file. Open
// writes a copy of that file to opts.Path and opens the copy. A fixture that
// migrated a fresh database for every test paid for every migration each
// time, under -race, and that is what ran internal/perm into Go's ten-minute
// package timeout on a loaded CI runner (TKT-01M3R5P3CN).
//
// The template is built from the migrations compiled into this binary, so a
// migration added to internal/store/db/migrations is in it with nothing to
// regenerate. TestTemplateIsAFreshMigration holds the copy to a fresh
// migration, row for row.
//
// An empty opts.Path becomes t.TempDir()/db/ketju.db. Open refuses a path
// where a database or its WAL already exists, rather than overwrite it.
// Every other option, Trace included, is passed to db.Open unchanged; the
// trace sees none of the migrations, because none run.
func Open(t testing.TB, opts db.Options) *db.DB {
	t.Helper()
	image, err := templateImage()
	if err != nil {
		t.Fatalf("dbtest: building the template database: %v", err)
	}
	if opts.Path == "" {
		opts.Path = filepath.Join(t.TempDir(), "db", "ketju.db")
	}
	if err := writeImage(opts.Path, image); err != nil {
		t.Fatalf("dbtest: %v", err)
	}
	d, err := db.Open(t.Context(), opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

// writeImage creates path with the template's bytes. The directory mode
// matches what db.Open would create.
func writeImage(path string, image []byte) error {
	if _, err := os.Lstat(path + "-wal"); err == nil {
		return fmt.Errorf("%s-wal already exists; a stale WAL would be replayed onto the new database", path)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("creating the database: %w", err)
	}
	if _, err := f.Write(image); err != nil {
		f.Close()
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return f.Close()
}

var template struct {
	once  sync.Once
	image []byte
	err   error
}

// builds counts template builds, for TestTemplateMigratesOnce.
var builds atomic.Int32

// templateImage is the migrated database file's bytes, built on first use.
// A failed build is kept and reported by every Open, rather than retried.
func templateImage() ([]byte, error) {
	template.once.Do(func() {
		builds.Add(1)
		template.image, template.err = buildTemplate()
	})
	return template.image, template.err
}

// buildTemplate migrates a new database in a directory of its own, closes it,
// and reads its main file. Closing checkpoints the WAL with TRUNCATE (see
// db.DB.Close), so the main file then holds the whole database; a WAL left
// with frames in it would mean the copy is missing them, and fails the build.
//
// It uses a directory from os.MkdirTemp rather than a test's t.TempDir,
// because the template outlives the test that happened to build it.
func buildTemplate() (image []byte, err error) {
	dir, err := os.MkdirTemp("", "ketju-dbtest-")
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, os.RemoveAll(dir)) }()
	path := filepath.Join(dir, "ketju.db")
	ctx := context.Background()
	d, err := db.Open(ctx, db.Options{Path: path})
	if err != nil {
		return nil, err
	}
	if err := d.Migrate(ctx); err != nil {
		return nil, errors.Join(err, d.Close())
	}
	if err := d.Close(); err != nil {
		return nil, err
	}
	switch fi, err := os.Stat(path + "-wal"); {
	case err == nil && fi.Size() != 0:
		return nil, fmt.Errorf("the WAL still holds %d bytes after closing, so the main file is not the whole database", fi.Size())
	case err != nil && !errors.Is(err, fs.ErrNotExist):
		return nil, err
	}
	return os.ReadFile(path)
}
