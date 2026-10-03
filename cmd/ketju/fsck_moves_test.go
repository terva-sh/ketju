package main

import (
	"os"
	"strings"
	"testing"

	"github.com/terva-sh/ketju/internal/store/threadcache"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// moveDir writes two threads of two posts each, edits the source's post 2,
// and returns the root, the two ids, and a cache over them to append with.
func moveDir(t *testing.T) (string, threadid.ID, threadid.ID, *threadcache.Cache) {
	t.Helper()
	root := t.TempDir()
	src, dst := threadid.ID(0x3f7a9c21e0000001), threadid.ID(0x3f7a9c21e0000002)
	threadAt(t, root, src, 2)
	threadAt(t, root, dst, 2)
	c, err := threadcache.New(root, threadcache.Options{Sync: threadlog.SyncNever})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	appendRun(t, c, src, threadlog.Append{Type: threadlog.TypeEdit, Ordinal: 2, Author: 2,
		Body: &threadlog.EditBody{Source: "edited"}})
	return root, src, dst, c
}

func appendRun(t *testing.T, c *threadcache.Cache, id threadid.ID, run ...threadlog.Append) {
	t.Helper()
	if err := c.With(id, func(th *threadcache.Thread) error {
		_, err := th.Actor.AppendRun(run)
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

// TestFsckReportsUnfinishedMoveAsWarning (TKT-01M29CXJZZ): a data directory
// read between a move's source record and the end of its target's run, by
// a crash or a backup taken mid-move, passes fsck, and the move is reported
// as unfinished until its target holds every copy at its original's
// revision.
func TestFsckReportsUnfinishedMoveAsWarning(t *testing.T) {
	root, src, dst, c := moveDir(t)
	appendRun(t, c, src, threadlog.Append{Type: threadlog.TypePostsSplitOut, Author: 9,
		Body: &threadlog.PostsSplitOutBody{Ordinals: []uint32{2}, TargetThreadID: uint64(dst)}})

	check := func(step string, want int) {
		t.Helper()
		sum, out, err := fsckJSON(t, "--root", root)
		if err != nil {
			t.Fatalf("%s: fsck failed: %v\n%s", step, err, out)
		}
		if sum.UnfinishedMoves != want || sum.MoveWarnings != 0 || sum.Damaged != 0 {
			t.Errorf("%s: %d unfinished moves, %d warnings, %d damaged; want %d unfinished\n%s",
				step, sum.UnfinishedMoves, sum.MoveWarnings, sum.Damaged, want, out)
		}
		text, err := fsckText(t, "--root", root)
		if err != nil {
			t.Fatalf("%s: fsck failed: %v", step, err)
		}
		if got := strings.Contains(text, "MOVING   "+src.String()); got != (want > 0) {
			t.Errorf("%s: the text report names the move %v, want %v:\n%s", step, got, want > 0, text)
		}
	}
	check("after the source record", 1)

	appendRun(t, c, dst,
		threadlog.Append{Type: threadlog.TypePostsMergedIn, Author: 9,
			Body: &threadlog.PostsMergedInBody{SourceThreadID: uint64(src), Ordinals: []uint32{2}}},
		threadlog.Append{Type: threadlog.TypePost, Author: 2,
			Body: &threadlog.PostBody{Source: "a message of ordinary length",
				Origin: &threadlog.PostOrigin{ThreadID: uint64(src), Ordinal: 2}}})
	check("with the copy but not its Edit", 1)

	appendRun(t, c, dst, threadlog.Append{Type: threadlog.TypeEdit, Ordinal: 3, Author: 2,
		Body: &threadlog.EditBody{Source: "edited"}})
	check("finished", 0)

	// A later edit of the copy leaves the move finished.
	appendRun(t, c, dst, threadlog.Append{Type: threadlog.TypeEdit, Ordinal: 3, Author: 2,
		Body: &threadlog.EditBody{Source: "edited again"}})
	check("edited past the source", 0)

	// Another Post naming the same origin in the target, at revision 1:
	// no move's copy, so the move stays finished (Terva review 2020).
	appendRun(t, c, dst, threadlog.Append{Type: threadlog.TypePost, Author: 2,
		Body: &threadlog.PostBody{Source: "a stray copy",
			Origin: &threadlog.PostOrigin{ThreadID: uint64(src), Ordinal: 2}}})
	for range 8 {
		check("a stray copy in the target", 0)
	}
}

// TestFsckWarnsOfASplitNoMoveCouldWrite: a split naming a post that was not
// shown, written without the actor's checks, is a warning and not damage,
// since the index folds it anyway.
func TestFsckWarnsOfASplitNoMoveCouldWrite(t *testing.T) {
	root, src, dst, c := moveDir(t)
	appendRun(t, c, src, threadlog.Append{Type: threadlog.TypeDelete, Ordinal: 1, Author: 1,
		Body: &threadlog.DeleteBody{}})
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	// Written past the actor, which refuses it.
	writeRaw(t, src.LogPath(root), threadlog.FrameHeader{Type: threadlog.TypePostsSplitOut, TimestampMS: 1_700_000_100_000, Actor: 9},
		&threadlog.PostsSplitOutBody{Ordinals: []uint32{1}, TargetThreadID: uint64(dst)})
	sum, out, err := fsckJSON(t, "--root", root)
	if err != nil {
		t.Fatalf("fsck failed: %v\n%s", err, out)
	}
	if sum.MoveWarnings != 1 || sum.Damaged != 0 || !strings.Contains(out, "not shown as itself") {
		t.Errorf("%d move warnings, %d damaged; want one warning\n%s", sum.MoveWarnings, sum.Damaged, out)
	}
}

// writeRaw appends one frame to a log file directly.
func writeRaw(t *testing.T, path string, h threadlog.FrameHeader, body any) {
	t.Helper()
	h.Encoding = threadlog.EncodingCBOR
	enc, err := threadlog.EncodeBody(h.Encoding, body)
	if err != nil {
		t.Fatal(err)
	}
	frame, err := threadlog.AppendFrame(nil, h, enc)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.Write(frame); err != nil {
		t.Fatal(err)
	}
}
