package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// Tests for the reply chain lens (TKT-01M29CTY5T, docs/06 "Lenses"). Its
// rows in the access matrix are in TestLensPostsMatchCanonical, and the
// walk's own tests in package lens.

// chainLens asserts p is chain:n's page with the posts want.
func chainLens(t *testing.T, name string, p dto.ThreadPage, n int, want []int) {
	t.Helper()
	if p.Lens == nil || p.Lens.Kind != "chain" || p.Lens.Value != fmt.Sprint(n) || p.Lens.Shown != len(want) ||
		p.Lens.Hidden != p.Thread.PostCount-len(want) {
		t.Errorf("%s: lens %+v, want chain %d showing %d of %d", name, p.Lens, n, len(want), p.Thread.PostCount)
	}
	if got := ordinalsOf(p.Posts); !slices.Equal(got, want) {
		t.Errorf("%s: posts %v, want %v", name, got, want)
	}
}

// replyThread is a thread of n posts by alice, each replying to replyTo's
// entry for its ordinal (none when absent).
func (w *world) replyThread(seq uint64, n int, replyTo map[int]uint32) threadid.ID {
	w.t.Helper()
	id, _ := w.thread(seq, "A branching one")
	for o := 1; o <= n; o++ {
		w.post(id, "alice", fmt.Sprintf("post %d", o), replyTo[o])
	}
	return id
}

// TestChainLensOrdinalOrderWithGaps is AC2: a branch 3, 9, 20 with replies 22
// and 31 to 20 serves exactly those posts in ordinal order, with lens.gaps
// counting the posts skipped before, between and after them.
func TestChainLensOrdinalOrderWithGaps(t *testing.T) {
	w := newWorld(t)
	id := w.replyThread(1, 35, map[int]uint32{9: 3, 20: 9, 22: 20, 31: 20, 25: 22, 30: 9})
	for _, who := range []string{"", "carol", "bob"} {
		p := page(t, w.get(w.viewer(who), threadPath(id)+"?lens=chain:20"))
		chainLens(t, who, p, 20, []int{3, 9, 20, 22, 31})
		if want := gaps(0, 2, 3, 5, 9, 10, 20, 1, 22, 8, 31, 4); !slices.Equal(p.Lens.Gaps, want) {
			t.Errorf("%q: gaps %v, want %v", who, p.Lens.Gaps, want)
		}
		// Each post carries its reply link, so the client can draw the
		// chain's shape from the page alone.
		for _, post := range p.Posts {
			if post.Ordinal != 3 && (post.ReplyTo == nil || *post.ReplyTo == 0) {
				t.Errorf("%q: #%d has no reply_to", who, post.Ordinal)
			}
		}
	}
	// #9's chain is its ancestor, itself, and its two replies, not #20's.
	chainLens(t, "chain:9", page(t, w.get(w.viewer("carol"), threadPath(id)+"?lens=chain:9")), 9, []int{3, 9, 20, 30})
}

// TestChainThroughRemovedPost is AC2's removed post: an ancestor its author
// deleted keeps its place as a tombstone, with its reply_to kept, and no
// body for a member or for its author. A moderator reads its body, as on the
// canonical page.
func TestChainThroughRemovedPost(t *testing.T) {
	w := newWorld(t)
	w.addUsers("active", "erin")
	id, _ := w.thread(1, "A chain with a hole")
	w.post(id, "carol", "the root", 0)
	w.post(id, "alice", "a reply alice regrets", 1)
	w.post(id, "erin", "a reply to the regretted one", 2)
	w.append(id, threadlog.Append{Type: threadlog.TypeDelete, Author: uint64(w.users["alice"]), Ordinal: 2,
		Body: &threadlog.DeleteBody{Reason: "never mind"}})
	for who, body := range map[string]bool{"carol": false, "alice": false, "": false, "bob": true} {
		p := page(t, w.get(w.viewer(who), threadPath(id)+"?lens=chain:3"))
		chainLens(t, who, p, 3, []int{1, 2, 3})
		hole := p.Posts[1]
		if !hole.Deleted || hole.ReplyTo == nil || *hole.ReplyTo != 1 || (hole.HTML != "") != body {
			t.Errorf("%q: the deleted ancestor is %+v; want a tombstone replying to #1, body %v", who, hole, body)
		}
		canonical := page(t, w.get(w.viewer(who), threadPath(id)+"?ordinal=2"))
		lensed, _ := json.Marshal(hole)
		drawn, _ := json.Marshal(canonical.Posts[1])
		if string(lensed) != string(drawn) {
			t.Errorf("%q: the lens draws #2 as\n%s\nthe canonical page as\n%s", who, lensed, drawn)
		}
	}
}

// TestChainLensSinglePost is AC4's first half: a post that replies to
// nothing and has no replies is its own chain, shown 1.
func TestChainLensSinglePost(t *testing.T) {
	w := newWorld(t)
	id := w.replyThread(1, 5, map[int]uint32{3: 1})
	chainLens(t, "chain:4", page(t, w.get(w.viewer("carol"), threadPath(id)+"?lens=chain:4")), 4, []int{4})
	chainLens(t, "chain:1", page(t, w.get(w.viewer("carol"), threadPath(id)+"?lens=chain:1")), 1, []int{1, 3})
}

// TestChainPastEndNotFound is AC4's second half: chain: past the last post is
// not-found, as ?ordinal= is, for every viewer, and a malformed chain: is the
// canonical page.
func TestChainPastEndNotFound(t *testing.T) {
	w := newWorld(t)
	id := w.replyThread(1, 5, nil)
	for _, who := range []string{"", "carol", "bob"} {
		for _, q := range []string{"?lens=chain:6", "?lens=chain:4294967295", "?lens=chain:6&ordinal=1"} {
			if rec := w.get(w.viewer(who), threadPath(id)+q); rec.Code != http.StatusNotFound {
				t.Errorf("%q %s: %d %s", who, q, rec.Code, rec.Body)
			}
		}
		want := w.get(w.viewer(who), threadPath(id)).Body.String()
		for _, q := range []string{"?lens=chain:0", "?lens=chain:x", "?lens=chain:", "?lens=chain:-1"} {
			if got := w.get(w.viewer(who), threadPath(id)+q).Body.String(); got != want {
				t.Errorf("%q %s: not the canonical page:\n%s", who, q, got)
			}
		}
	}
}

// TestChainOverCorruptIndex is AC1 through the endpoint: with a reply_to
// cycle written into the thread's sidecar index, the chain lens still
// answers, with the walk stopped at the bad link.
func TestChainOverCorruptIndex(t *testing.T) {
	w := newWorld(t)
	id := w.replyThread(1, 6, map[int]uint32{2: 1, 3: 2, 4: 3})
	// #1 now claims to reply to #4, closing the loop 4, 3, 2, 1, 4; and #6
	// claims to reply to itself.
	w.entryOffline(id, 1, func(e *threadlog.IndexEntry) { e.ReplyTo = 4 })
	w.entryOffline(id, 6, func(e *threadlog.IndexEntry) { e.ReplyTo = 6 })
	chainLens(t, "the cycle", page(t, w.get(w.viewer("carol"), threadPath(id)+"?lens=chain:4")), 4, []int{1, 2, 3, 4})
	chainLens(t, "the self-reference", page(t, w.get(w.viewer("carol"), threadPath(id)+"?lens=chain:6")), 6, []int{6})
}

// TestChainLensStatements is AC3: a chain page runs exactly the statements
// the canonical page runs, on a thread of 10 posts and one of 2000, for an
// anonymous viewer and a member. The chain is read from the index.
func TestChainLensStatements(t *testing.T) {
	st := &statements{}
	w := newWorldTraced(t, threadlog.SyncNever, st.trace)
	counts := map[string][]int{}
	for seq, n := range map[uint64]int{1: 10, 2: 2000} {
		id := w.quickThread(seq, n)
		canonical, lensed := threadPath(id)+"?page=1", threadPath(id)+"?lens=chain:3&page=1"
		for _, who := range []string{"", "carol"} {
			v := w.viewer(who)
			w.get(v, canonical)
			w.get(v, lensed)
			var c, l *httptest.ResponseRecorder
			ranC := st.during(func() { c = w.get(v, canonical) })
			ranL := st.during(func() { l = w.get(v, lensed) })
			if c.Code != http.StatusOK || l.Code != http.StatusOK {
				t.Fatalf("%d posts, %q: %d and %d", n, who, c.Code, l.Code)
			}
			if !slices.Equal(ranC, ranL) {
				t.Errorf("%d posts, %q: the canonical page ran %d statements, the chain %d\ncanonical %q\nlens      %q",
					n, who, len(ranC), len(ranL), ranC, ranL)
			}
			if p := page(t, l); p.Lens == nil || p.Lens.Shown != 1 {
				t.Fatalf("%d posts: lens %+v", n, p.Lens)
			}
			counts[who] = append(counts[who], len(ranL))
		}
	}
	for who, c := range counts {
		if len(c) != 2 || c[0] != c[1] {
			t.Errorf("%q: the chain page ran %v statements on the two threads; the length changed the count", who, c)
		}
	}
}
