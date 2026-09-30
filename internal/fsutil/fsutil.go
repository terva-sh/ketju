// Package fsutil replaces files durably: write a new file beside the old one,
// fsync it, rename it over the old one, and fsync the directory, so a power
// cut leaves either the old file or the whole new one, never half of each
// (docs/03 "Hard redaction"). The rename is atomic but not durable on its
// own; the directory fsync is what makes it survive.
package fsutil

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// SyncDir makes a directory's entries durable: a file created, renamed or
// removed in it survives a power cut once this returns.
func SyncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	if err := d.Sync(); err != nil {
		return fmt.Errorf("fsutil: syncing %s: %w", dir, err)
	}
	return nil
}

// ReplaceFile replaces path with what write puts in tmp, a new file beside
// it: tmp is created (a leftover one is removed first), written, fsynced and
// closed, then renamed over path, and the directory is fsynced.
//
// renamed, when not nil, runs between the rename and the directory fsync,
// which is the window a crash test needs to reach. An error from it is
// returned, and the directory is not fsynced.
//
// On any failure before the rename, tmp is removed and path is untouched.
func ReplaceFile(path, tmp string, perm fs.FileMode, write func(*os.File) error, renamed func() error) (err error) {
	if filepath.Dir(tmp) != filepath.Dir(path) {
		return fmt.Errorf("fsutil: %s is not beside %s, so the rename would not be atomic", tmp, path)
	}
	if err := os.Remove(tmp); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return err
	}
	done := false
	defer func() {
		if !done {
			f.Close()
			os.Remove(tmp)
		}
	}()
	if err := write(f); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("fsutil: syncing %s: %w", tmp, err)
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	done = true
	if renamed != nil {
		if err := renamed(); err != nil {
			return err
		}
	}
	return SyncDir(filepath.Dir(path))
}

// WriteFile is ReplaceFile with the bytes to write.
func WriteFile(path, tmp string, perm fs.FileMode, data []byte) error {
	return ReplaceFile(path, tmp, perm, func(f *os.File) error {
		_, err := f.Write(data)
		return err
	}, nil)
}

// Remove removes path and fsyncs its directory, so the removal survives a
// power cut. A path already gone is not an error.
func Remove(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return SyncDir(filepath.Dir(path))
}
