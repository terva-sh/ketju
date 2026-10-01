package threadcache

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/terva-sh/ketju/internal/fsutil"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// writeBack replaces the index file at path with the whole of ix, so that the
// file and the index in memory agree before the actor writes a header over it.
//
// Whole and by rename, not entry by entry in place: a rebuilt index may be
// shorter than the file it replaces, and a crash part way through an in-place
// rewrite would leave the old header over a mix of entries. Under every
// policy but SyncNever the file and its directory are fsynced, so the index
// a later header covers is durable before that header can be. SyncNever skips
// the fsyncs, as the actor does.
func writeBack(path string, ix *threadlog.Index, policy threadlog.SyncPolicy) error {
	data := ix.AppendTo(nil)
	tmp := path + ".writeback"
	if policy != threadlog.SyncNever {
		return fsutil.WriteFile(path, tmp, 0o644, data)
	}
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

// discardIndex is the index writer for a thread whose index could not be
// written back. It accepts every write and keeps none, so the file on disk
// keeps a header that does not cover the log, and the next open replays.
type discardIndex struct{}

func (discardIndex) WriteAt(p []byte, _ int64) (int, error) { return len(p), nil }
func (discardIndex) Sync() error                            { return nil }

// WriteBackMarker names the file, in the data directory, that says every
// thread index beside it was written by a build that writes back a replayed
// or rebuilt index (TKT-01M3VHSYFR).
const WriteBackMarker = "index-writeback"

// DiscardUnverifiedIndexes removes every thread's index once per data
// directory, and reports how many it removed.
//
// A build before TKT-01M3VHSYFR could leave an index whose header covers the
// whole log over an entry that is stale: a Delete, a hide or a redaction
// applied in memory after a crash or with the index missing, and never
// written to the file. Such an index is trusted, and serves the post as
// visible. Nothing short of a full replay tells it apart from a good one, so
// every index is removed, and each thread rebuilds its own from its log on
// its first open and writes it back. The decision is in docs/03 "Durability
// and recovery".
//
// It does nothing when root holds [WriteBackMarker], and writes that marker
// once every removal is durable. A run that stops part way leaves no marker,
// so the next one removes again, which costs only rebuilds. Call it before
// anything opens a thread.
func DiscardUnverifiedIndexes(root string) (int, error) {
	marker := filepath.Join(root, WriteBackMarker)
	if _, err := os.Stat(marker); err == nil {
		return 0, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return 0, fmt.Errorf("threadcache: looking for %s: %w", marker, err)
	}

	logs, _, err := threadid.WalkLogs(root)
	if err != nil {
		return 0, fmt.Errorf("threadcache: listing threads: %w", err)
	}
	removed := 0
	dirs := map[string]bool{}
	for id := range logs {
		p := id.IndexPath(root)
		if err := os.Remove(p); errors.Is(err, fs.ErrNotExist) {
			continue
		} else if err != nil {
			return removed, fmt.Errorf("threadcache: removing %s: %w", p, err)
		}
		removed++
		dirs[filepath.Dir(p)] = true
	}
	for d := range dirs {
		if err := fsutil.SyncDir(d); err != nil {
			return removed, err
		}
	}
	note := []byte("Every thread index under threads/ was written by a ketju that writes back\n" +
		"a replayed or rebuilt index before trusting it (TKT-01M3VHSYFR). Removing\n" +
		"this file makes ketju serve discard every index once more at its next start.\n")
	if err := fsutil.WriteFile(marker, marker+".tmp", 0o644, note); err != nil {
		return removed, fmt.Errorf("threadcache: writing %s: %w", marker, err)
	}
	return removed, nil
}
