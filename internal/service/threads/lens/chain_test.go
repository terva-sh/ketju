package lens_test

import (
	"slices"
	"testing"

	"github.com/terva-sh/ketju/internal/service/threads/lens"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// Tests for the reply chain's walk (TKT-01M29CTY5T). The endpoint's are in
// internal/web/api.

// replyIndex is an in-memory index whose post i (from 1) replies to
// replyTo[i-1], 0 for none.
func replyIndex(replyTo ...uint32) *threadlog.Index {
	entries := make([]threadlog.IndexEntry, len(replyTo))
	for i, r := range replyTo {
		entries[i] = threadlog.IndexEntry{Offset: uint64(64 + i), Author: 1, Revision: 1, ReplyTo: r}
	}
	return threadlog.NewIndex(threadlog.IndexHeader{ThreadID: 1, PostCount: uint32(len(replyTo))}, entries)
}

// chainOf is the reply chain of n in ix, as one page.
func chainOf(t *testing.T, ix *threadlog.Index, n uint32) []uint32 {
	t.Helper()
	f, err := lens.ReplyChain(ix, n)
	if err != nil {
		t.Fatalf("chain:%d: %v", n, err)
	}
	page, err := lens.Apply(ix, f).Page(1, ix.Len())
	if err != nil {
		t.Fatalf("chain:%d: its one page: %v", n, err)
	}
	return page
}

// TestChainWalkBoundedOnMalformedIndex is AC1: on indexes holding a
// reply_to cycle, a self-reference, a forward reference and one past the
// last post, the ancestor walk stops at the bad link, within the thread's
// length, and the lens serves a page.
func TestChainWalkBoundedOnMalformedIndex(t *testing.T) {
	for _, c := range []struct {
		name    string
		replyTo []uint32
		n       uint32
		want    []uint32
	}{
		// #3 replies to #2, #2 to #1, and #1 claims to reply to #3.
		{"a three-post cycle", []uint32{3, 1, 2}, 3, []uint32{1, 2, 3}},
		// #1 and #2 reply to each other. #2's ancestor is #1, whose
		// reply_to points forward and ends the walk. #1 is also listed as
		// a reply to #2, which is what its corrupt entry claims.
		{"a two-post cycle", []uint32{2, 1}, 2, []uint32{1, 2}},
		{"a self-reference", []uint32{0, 2, 2}, 2, []uint32{2, 3}},
		{"a forward reference", []uint32{0, 5, 2, 0, 0}, 3, []uint32{2, 3}},
		{"a reply_to past the last post", []uint32{0, 99, 2}, 3, []uint32{2, 3}},
		{"the largest reply_to", []uint32{0, 1<<32 - 1}, 2, []uint32{2}},
	} {
		if got := chainOf(t, replyIndex(c.replyTo...), c.n); !slices.Equal(got, c.want) {
			t.Errorf("%s: chain:%d = %v, want %v", c.name, c.n, got, c.want)
		}
	}

	// A well-formed line of 5000 posts, each replying to the one before, is
	// walked to its root, whatever its length.
	line := make([]uint32, 5000)
	for i := range line {
		line[i] = uint32(i)
	}
	if got := chainOf(t, replyIndex(line...), 5000); len(got) != 5000 || got[0] != 1 || got[4999] != 5000 {
		t.Fatalf("the 5000-post line's chain has %d posts", len(got))
	}
}

// TestChainKeepsAncestorsAndReplies is AC2's selection: n's ancestors, n and
// its direct replies, and neither its replies' replies nor its siblings. A
// post past the last, or none, is not-found.
func TestChainKeepsAncestorsAndReplies(t *testing.T) {
	// #2 and #6 reply to #1, #3 and #4 to #2, #5 to #3; #7 to nothing.
	ix := replyIndex(0, 1, 2, 2, 3, 1, 0)
	for n, want := range map[uint32][]uint32{
		1: {1, 2, 6},
		2: {1, 2, 3, 4},
		3: {1, 2, 3, 5},
		5: {1, 2, 3, 5},
		6: {1, 6},
		7: {7},
	} {
		if got := chainOf(t, ix, n); !slices.Equal(got, want) {
			t.Errorf("chain:%d = %v, want %v", n, got, want)
		}
	}
	for _, n := range []uint32{0, 8, 1000} {
		if _, err := lens.ReplyChain(ix, n); !notFound(err) {
			t.Errorf("chain:%d: %v, want not-found", n, err)
		}
	}
}
