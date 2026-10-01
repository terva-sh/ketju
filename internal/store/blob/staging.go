package blob

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/terva-sh/ketju/internal/fsutil"
)

// TmpDir is where uploads are staged under root.
func TmpDir(root string) string { return filepath.Join(root, Dir, Tmp) }

// staging is the staged files an upload in this process still holds, by
// base name, which CreateTemp makes unique within TmpDir. SweepTmp leaves
// them alone whatever their age: a client may pause mid-body for longer
// than the collector's hour, and an upload whose file was swept from under
// it would fail at Commit with valid input (Terva review 1735). One process
// holds the data directory (its lock), so a set in memory is all of them.
var staging = struct {
	sync.Mutex
	names map[string]bool
}{names: map[string]bool{}}

// Stage creates a new, empty file in TmpDir for an upload to be written
// into, and marks it held until Unstage. The caller removes it, unless
// Commit has moved it into place, and then calls Unstage.
func Stage(root string) (*os.File, error) {
	dir := TmpDir(root)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("blob: creating %s: %w", dir, err)
	}
	f, err := os.CreateTemp(dir, "upload-*")
	if err != nil {
		return nil, fmt.Errorf("blob: staging an upload: %w", err)
	}
	staging.Lock()
	staging.names[filepath.Base(f.Name())] = true
	staging.Unlock()
	return f, nil
}

// Unstage says the upload that staged path is finished with it, so SweepTmp
// may remove it once it is old. The upload calls it after removing the file
// or committing it, so there is never a moment when an open upload's file
// is old and unheld.
func Unstage(path string) {
	staging.Lock()
	delete(staging.names, filepath.Base(path))
	staging.Unlock()
}

func held(name string) bool {
	staging.Lock()
	defer staging.Unlock()
	return staging.names[name]
}

// SweepTmp removes every staged file in TmpDir last modified more than
// olderThan ago that no upload in this process holds (Stage), and returns
// how many it removed. Serve calls it with zero,
// under the data-directory lock and before it listens, so nothing is being
// staged; the collector calls it with an hour, for a crash mid-upload
// (docs/03). A missing directory has nothing to sweep.
func SweepTmp(root string, olderThan time.Duration, now time.Time) (int, error) {
	dir := TmpDir(root)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	var (
		n    int
		errs []error
	)
	for _, e := range entries {
		if !e.Type().IsRegular() {
			continue // nothing here makes anything else; leave it for a person
		}
		if held(e.Name()) {
			continue // an upload still writing it, however slowly
		}
		info, err := e.Info()
		if errors.Is(err, fs.ErrNotExist) {
			continue // its upload finished or failed while we looked
		}
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if olderThan > 0 && now.Sub(info.ModTime()) <= olderThan {
			continue
		}
		switch err := os.Remove(filepath.Join(dir, e.Name())); {
		case err == nil:
			n++
		case !errors.Is(err, fs.ErrNotExist):
			errs = append(errs, err)
		}
	}
	return n, errors.Join(errs...)
}

// syncDir is fsutil.SyncDir, a seam for the test that asks which
// directories Commit makes durable.
var syncDir = fsutil.SyncDir

// Commit moves a staged file to the blob path for hash, and makes the move
// durable: every directory from the shard up to root is synced before the
// rename, so a shard the first upload into it creates survives a power cut
// in its parent, and the shard is synced after it (Terva review 1732). An
// upload is rare, and four fsyncs on it are cheap beside a blob row whose
// file a power cut took. Syncing whether or not MkdirAll created anything
// also covers a shard a crashed process created and never synced.
// placed reports that this call put the file there. When a file is already at the path, the staged one is removed
// instead: the path names its content, so the two are identical.
//
// The caller syncs the staged file's contents first, and holds the database
// write lock, so no collector pass or other upload interleaves (docs/03
// "Blob garbage collection").
func Commit(staged, root, hash string) (placed bool, err error) {
	path, err := Path(root, hash)
	if err != nil {
		return false, err
	}
	switch _, err := os.Stat(path); {
	case err == nil:
		if err := os.Remove(staged); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return false, err
		}
		return false, nil
	case !errors.Is(err, fs.ErrNotExist):
		return false, err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return false, err
	}
	top := filepath.Clean(root)
	for d := dir; d != top && filepath.Dir(d) != d; d = filepath.Dir(d) {
		if err := syncDir(filepath.Dir(d)); err != nil {
			return false, err
		}
	}
	if err := os.Rename(staged, path); err != nil {
		return false, err
	}
	if err := syncDir(dir); err != nil {
		// The file is in place; whether the rename survives a power cut is
		// what failed. The caller treats it as placed, so a failed upload
		// removes it.
		return true, err
	}
	return true, nil
}

// Orphans lists the blob files under root's shard directories whose names
// are hashes and that were last modified more than olderThan ago. Whether
// one has a row is the caller's question, asked inside a database write.
// The collector's trash (".gc") and thumbnails (".thumb") are not hashes,
// and are left out.
func Orphans(root string, olderThan time.Duration, now time.Time) ([]string, error) {
	shards, err := filepath.Glob(filepath.Join(root, Dir, Algorithm, "*"))
	if err != nil {
		return nil, err
	}
	var out []string
	for _, shard := range shards {
		entries, err := os.ReadDir(shard)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) || errors.Is(err, os.ErrInvalid) {
				continue
			}
			if st, serr := os.Stat(shard); serr == nil && !st.IsDir() {
				continue
			}
			return nil, err
		}
		for _, e := range entries {
			name := e.Name()
			if !e.Type().IsRegular() || !ValidHash(name) || !strings.HasPrefix(name, filepath.Base(shard)) ||
				len(filepath.Base(shard)) != 2 {
				continue
			}
			info, err := e.Info()
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			if err != nil {
				return nil, err
			}
			if now.Sub(info.ModTime()) <= olderThan {
				continue
			}
			out = append(out, name)
		}
	}
	return out, nil
}
