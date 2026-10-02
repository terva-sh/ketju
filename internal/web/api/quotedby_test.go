package api

import (
	"fmt"
	"slices"
	"testing"

	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// Tests for quoted_by, a post's quote backlinks (docs/06 "Post anatomy",
// TKT-01M3YME9AZ).

// quoteOf is source quoting each ordinal, as the composer writes a quote.
func quoteOf(ordinals ...int) string {
	src := ""
	for _, o := range ordinals {
		src += fmt.Sprintf("> [quote=#%d]\n> the quoted line\n\n", o)
	}
	return src + "my answer"
}

// quotedThread is a thread whose #1 is quoted by a post in every state a
// reader can be shown it in:
//
//	#1 carol's, quoted by every post below
//	#2 carol's, which she deleted; #3 quotes it too
//	#3 scribe's, visible
//	#4 carol's, which she deleted
//	#5 alice's, which a moderator hid
//	#6 carol's, hidden pending review
//	#7 carol's, hard-redacted
//	#8 carol's, moved out of the thread
//
// The moved flag is set in the sidecar index alone, since no record moves a
// post yet (TKT-01M29CXJZZ), so post_meta still lists #8 as there.
func (w *world) quotedThread() threadid.ID {
	w.t.Helper()
	id, _ := w.thread(1, "Quoted")
	bob := uint64(w.users["bob"])
	w.post(id, "carol", "the post everyone quotes", 0)
	w.post(id, "carol", "a post its author deleted", 0)
	w.post(id, "scribe", quoteOf(1, 2), 0)
	w.post(id, "carol", quoteOf(1), 0)
	w.del(id, 4, "carol")
	w.del(id, 2, "carol")
	w.post(id, "alice", quoteOf(1), 0)
	w.append(id, threadlog.Append{Type: threadlog.TypeDelete, Author: bob, Ordinal: 5,
		Body: &threadlog.DeleteBody{Reason: "off topic", ByModerator: true}})
	w.post(id, "carol", quoteOf(1), 0)
	w.append(id, threadlog.Append{Type: threadlog.TypeDelete, Author: bob, Ordinal: 6,
		Body: &threadlog.DeleteBody{Reason: "flagged", Auto: true}})
	w.post(id, "carol", quoteOf(1), 0)
	w.post(id, "carol", quoteOf(1), 0)
	w.redactOffline(id, 7)
	w.flagOffline(id, 8, threadlog.EntryMoved)
	for o, want := range map[uint32]threadlog.EntryFlags{
		2: threadlog.EntryDeleted, 4: threadlog.EntryDeleted, 5: threadlog.EntryHiddenByMod,
		6: threadlog.EntryHiddenByMod | threadlog.EntryAutoHidden, 7: threadlog.EntryRedacted, 8: threadlog.EntryMoved,
	} {
		if e := w.entry(id, o); !e.Flags.Has(want) {
			w.t.Fatalf("#%d has flags %b, want %b", o, e.Flags, want)
		}
	}
	return id
}

// postNamed is ordinal n among posts.
func postNamed(t *testing.T, where string, posts []dto.Post, n int) dto.Post {
	t.Helper()
	for _, p := range posts {
		if p.Ordinal == n {
			return p
		}
	}
	t.Fatalf("%s: no #%d among %v", where, n, ordinalsOf(posts))
	return dto.Post{}
}

// TestQuotedByListsOnlyVisibleQuotingPosts: #1's quoted_by is [3] for anon,
// a member, a moderator and the author of the hidden #5, on the canonical
// page, a lens page and the single post: no deleted, hidden, auto-hidden,
// redacted or moved post is listed, moderators included. A post served
// without html, the deleted #2, has an empty quoted_by, though the visible #3
// quotes it; a moderator, who is served #2's body, sees #3 there too. The
// list is always an array.
func TestQuotedByListsOnlyVisibleQuotingPosts(t *testing.T) {
	w := newWorld(t)
	id := w.quotedThread()
	for _, who := range []string{"", "carol", "bob", "alice"} {
		v := w.viewer(who)
		pages := map[string][]dto.Post{
			"the thread page": page(t, w.get(v, threadPath(id))).Posts,
			"a lens page":     page(t, w.get(v, threadPath(id)+"?lens=author:carol")).Posts,
			"the single post": {decode[dto.Post](t, w.get(v, postPathN(id, 1)))},
		}
		// The second request for a page is a response cache hit, and must
		// say the same.
		pages["the thread page, cached"] = page(t, w.get(v, threadPath(id))).Posts
		for where, posts := range pages {
			where = where + " as " + who
			if p := postNamed(t, where, posts, 1); !slices.Equal(p.QuotedBy, []int{3}) {
				t.Errorf("%s: #1 quoted_by %v, want [3]", where, p.QuotedBy)
			}
			for _, p := range posts {
				if p.QuotedBy == nil {
					t.Errorf("%s: #%d quoted_by is null, want an array", where, p.Ordinal)
				}
				if p.HTML == "" && len(p.QuotedBy) != 0 {
					t.Errorf("%s: #%d carries no html and quoted_by %v", where, p.Ordinal, p.QuotedBy)
				}
			}
		}
		two := postNamed(t, who, pages["the thread page"], 2)
		want := []int{}
		if who == "bob" {
			if two.HTML == "" {
				t.Fatalf("the moderator is served #2 without its body")
			}
			want = []int{3}
		}
		if !slices.Equal(two.QuotedBy, want) {
			t.Errorf("as %q: the deleted #2 has quoted_by %v, want %v", who, two.QuotedBy, want)
		}
	}
}
