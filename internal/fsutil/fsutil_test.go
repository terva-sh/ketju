package fsutil

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestReplaceFile(t *testing.T) {
	dir := t.TempDir()
	path, tmp := filepath.Join(dir, "a.log"), filepath.Join(dir, "a.log.new")
	if err := os.WriteFile(path, []byte("old"), 0o640); err != nil {
		t.Fatal(err)
	}
	// A leftover temp file from an interrupted run is replaced, not refused.
	if err := os.WriteFile(tmp, []byte("leftover"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(path, tmp, 0o640, []byte("new")); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); string(b) != "new" {
		t.Errorf("path holds %q", b)
	}
	if _, err := os.Stat(tmp); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the temp file is left: %v", err)
	}
}

func TestReplaceFileFailureLeavesTheOld(t *testing.T) {
	dir := t.TempDir()
	path, tmp := filepath.Join(dir, "a.log"), filepath.Join(dir, "a.log.new")
	if err := os.WriteFile(path, []byte("old"), 0o640); err != nil {
		t.Fatal(err)
	}
	boom := errors.New("boom")
	if err := ReplaceFile(path, tmp, 0o640, func(f *os.File) error {
		f.Write([]byte("half"))
		return boom
	}, nil); !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	if b, _ := os.ReadFile(path); string(b) != "old" {
		t.Errorf("path holds %q after a failed write", b)
	}
	if _, err := os.Stat(tmp); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the temp file is left: %v", err)
	}
	// renamed runs after the rename, so the new file is in place when it
	// fails.
	if err := ReplaceFile(path, tmp, 0o640, func(f *os.File) error {
		_, err := f.Write([]byte("new"))
		return err
	}, func() error { return boom }); !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	if b, _ := os.ReadFile(path); string(b) != "new" {
		t.Errorf("path holds %q after the rename", b)
	}
	if err := ReplaceFile(path, filepath.Join(t.TempDir(), "x"), 0o640, nil, nil); err == nil {
		t.Error("a temp file in another directory was accepted")
	}
}

func TestRemove(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a")
	if err := os.WriteFile(path, nil, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := Remove(path); err != nil {
		t.Errorf("removing what is gone: %v", err)
	}
}
