package threadcache

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// deletedAfterCopy builds a thread with two posts and a Delete of post 1 that
// its index does not show: the old index is put back over it, which is the
// Extended crash. It returns the index as it was before the Delete.
func deletedAfterCopy(t *testing.T, root string, id threadid.ID) []byte {
	t.Helper()
	c := reopen(t, root, threadlog.SyncGroup)
	create(t, c, id, 2)
	c.Close()
	before, err := os.ReadFile(id.IndexPath(root))
	if err != nil {
		t.Fatal(err)
	}
	c = reopen(t, root, threadlog.SyncGroup)
	appendTo(t, c, id, threadlog.Append{Type: threadlog.TypeDelete, Ordinal: 1, Author: 7, Body: &threadlog.DeleteBody{}})
	c.Close()
	if err := os.WriteFile(id.IndexPath(root), before, 0o644); err != nil {
		t.Fatal(err)
	}
	return before
}

// Criterion: the write-back alone makes the index trusted. An Extended open
// closed with no append reopens trusted, with nothing replayed.
func TestAWriteBackIsTrustedWithoutAnAppend(t *testing.T) {
	root := t.TempDir()
	id := ids(1)[0]
	deletedAfterCopy(t, root, id)

	c := reopen(t, root, threadlog.SyncGroup)
	if err := c.With(id, func(th *Thread) error {
		if th.Outcome != threadlog.Extended {
			t.Errorf("the open after the crash is %v, want extended", th.Outcome)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	c.Close()

	c = reopen(t, root, threadlog.SyncGroup)
	th, err := c.Acquire(id)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Release(th)
	if th.Outcome != threadlog.Trusted || th.Replayed != 0 {
		t.Errorf("reopened %v with %d bytes replayed, want trusted with none", th.Outcome, th.Replayed)
	}
	sameEntries(t, th.Index, fullRebuild(t, root, id), "the reopen")
}

// Criterion: a write-back that fails never leads to a trusted index. The
// thread is served from memory, the file keeps its old header, and the next
// open replays again.
func TestAFailedWriteBackNeverMarksTheIndexTrusted(t *testing.T) {
	root := t.TempDir()
	id := ids(1)[0]
	before := deletedAfterCopy(t, root, id)

	// A directory, not empty, where the write-back's temporary file goes:
	// it cannot be removed or written, whoever runs the test. A read-only
	// shard directory would do the same and stop nothing when CI runs as
	// root.
	blocker := id.IndexPath(root) + ".writeback"
	if err := os.MkdirAll(filepath.Join(blocker, "x"), 0o755); err != nil {
		t.Fatal(err)
	}

	var logged bytes.Buffer
	c, err := New(root, Options{Sync: threadlog.SyncGroup, Logger: slog.New(slog.NewTextHandler(&logged, nil))})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.With(id, func(th *Thread) error {
		if th.Outcome != threadlog.Extended {
			t.Errorf("the open after the crash is %v, want extended", th.Outcome)
		}
		if tomb, _ := th.Index.Tombstoned(1); !tomb {
			t.Error("post 1 is served visible from memory after its Delete")
		}
		post(t, th, "after the crash")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logged.String(), "could not write a thread's index back") {
		t.Errorf("the failed write-back was not logged: %s", logged.String())
	}
	after, err := os.ReadFile(id.IndexPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, before) {
		t.Errorf("the index file changed although it could not be written back:\n got %x\nwant %x", after, before)
	}

	if err := os.RemoveAll(blocker); err != nil {
		t.Fatal(err)
	}
	c = reopen(t, root, threadlog.SyncGroup)
	th, err := c.Acquire(id)
	if err != nil {
		t.Fatal(err)
	}
	if th.Outcome != threadlog.Extended {
		t.Errorf("the next open is %v, want extended: nothing should have made the old index trusted", th.Outcome)
	}
	if tomb, _ := th.Index.Tombstoned(1); !tomb {
		t.Error("post 1 is visible after its Delete")
	}
	sameEntries(t, th.Index, fullRebuild(t, root, id), "the next open")
	c.Release(th)
	c.Close()

	c = reopen(t, root, threadlog.SyncGroup)
	th, err = c.Acquire(id)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Release(th)
	if th.Outcome != threadlog.Trusted {
		t.Errorf("the open after a successful write-back is %v, want trusted", th.Outcome)
	}
}

// poison zeroes post 1's entry flags in a trusted index, which is what a
// build before write-back could leave: a header covering the log over an
// entry that does not show the Delete.
func poison(t *testing.T, path string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	const flagsAt = threadlog.IndexHeaderSize + 34 // ordinal 1's flags, docs/03
	b[flagsAt], b[flagsAt+1] = 0, 0
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

// Criterion: every index goes once per data directory, so one an older build
// poisoned is rebuilt from its log, and the marker stops it happening again.
func TestDiscardUnverifiedIndexesRemovesEveryIndexOnce(t *testing.T) {
	root := t.TempDir()
	both := ids(2)
	bad := both[0]

	c := reopen(t, root, threadlog.SyncGroup)
	create(t, c, both[0], 2)
	create(t, c, both[1], 1)
	appendTo(t, c, bad, threadlog.Append{Type: threadlog.TypeDelete, Ordinal: 1, Author: 7, Body: &threadlog.DeleteBody{}})
	c.Close()
	poison(t, bad.IndexPath(root))

	// The poison is real: the index is trusted and calls the post visible.
	raw, _ := os.ReadFile(bad.LogPath(root))
	idx, _ := os.ReadFile(bad.IndexPath(root))
	rec, err := threadlog.Open(bytes.NewReader(raw), int64(len(raw)), idx)
	if err != nil {
		t.Fatal(err)
	}
	if e, _ := rec.Index.Entry(1); rec.Outcome != threadlog.Trusted || !e.Flags.Visible() {
		t.Fatalf("the poisoned index opened %v with flags %#x; the fixture is wrong", rec.Outcome, e.Flags)
	}

	n, err := DiscardUnverifiedIndexes(root)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("removed %d indexes, want 2", n)
	}
	for _, id := range both {
		if _, err := os.Stat(id.IndexPath(root)); !os.IsNotExist(err) {
			t.Errorf("the index of %s is still there: %v", id, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, WriteBackMarker)); err != nil {
		t.Errorf("no marker after the discard: %v", err)
	}

	c = reopen(t, root, threadlog.SyncGroup)
	if err := c.With(bad, func(th *Thread) error {
		if th.Outcome != threadlog.Rebuilt {
			t.Errorf("the open after the discard is %v, want rebuilt", th.Outcome)
		}
		if tomb, _ := th.Index.Tombstoned(1); !tomb {
			t.Error("post 1 is visible after its Delete")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	c.Close()

	// The marker is there now, so a second start leaves the written-back
	// indexes alone.
	n, err = DiscardUnverifiedIndexes(root)
	if err != nil || n != 0 {
		t.Fatalf("the second discard removed %d, %v; want none", n, err)
	}
	c = reopen(t, root, threadlog.SyncGroup)
	th, err := c.Acquire(bad)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Release(th)
	if th.Outcome != threadlog.Trusted {
		t.Errorf("the open after the second start is %v, want trusted", th.Outcome)
	}
	if tomb, _ := th.Index.Tombstoned(1); !tomb {
		t.Error("post 1 is visible after its Delete")
	}
}

// Criterion: an index that cannot be removed is an error and leaves no
// marker, so serve refuses to start rather than serve it, and the next start
// tries again.
func TestDiscardUnverifiedIndexesFailsWhenAnIndexStays(t *testing.T) {
	root := t.TempDir()
	id := ids(1)[0]
	c := reopen(t, root, threadlog.SyncGroup)
	create(t, c, id, 1)
	c.Close()

	// The index path holds a directory that is not empty, which os.Remove
	// refuses whoever runs the test.
	idx := id.IndexPath(root)
	if err := os.Remove(idx); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(idx, "x"), 0o755); err != nil {
		t.Fatal(err)
	}

	if _, err := DiscardUnverifiedIndexes(root); err == nil {
		t.Fatal("the discard succeeded although the index could not be removed")
	}
	if _, err := os.Stat(filepath.Join(root, WriteBackMarker)); !os.IsNotExist(err) {
		t.Errorf("a marker was written although an index stayed: %v", err)
	}
	if _, err := os.Stat(idx); err != nil {
		t.Errorf("the index path should still be there: %v", err)
	}
}
