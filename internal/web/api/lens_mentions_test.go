package api

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// Tests for the mentions lens (TKT-01M29CTY6E, docs/06 "the mentions
// lens"). Its rows in the access matrix are in TestLensPostsMatchCanonical.

// mentionsLens asserts p is a mentions lens page with the posts want.
func mentionsLens(t *testing.T, name string, p dto.ThreadPage, want []int) {
	t.Helper()
	if p.Lens == nil || p.Lens.Kind != "mentions" || p.Lens.Value != "" || p.Lens.Shown != len(want) ||
		p.Lens.Hidden != p.Thread.PostCount-len(want) {
		t.Errorf("%s: lens %+v, want mentions showing %d of %d", name, p.Lens, len(want), p.Thread.PostCount)
	}
	if got := ordinalsOf(p.Posts); !slices.Equal(got, want) {
		t.Errorf("%s: posts %v, want %v", name, got, want)
	}
}

// mentionsThread is alice's thread in which members write through the API,
// so the service records each post's mentions as it would for anyone:
//
//	#1 alice's opening post       #4 bob's reply to #3
//	#2 carol's, mentioning erin   #5 carol's, quoting a mention of erin
//	#3 erin's, mentioning herself #6 bob's, mentioning alice
func (w *world) mentionsThread() threadid.ID {
	w.t.Helper()
	w.addUsers("active", "erin")
	id, _ := w.thread(1, "Who said what")
	w.post(id, "alice", "Opening the thread.", 0)
	w.say(id, "carol", "Over to you, @erin.")
	w.say(id, "erin", "Noting this for @erin later.")
	w.clock += 60_000
	rec := w.sendAs(w.viewer("bob"), http.MethodPost, postsPath(id), `{"source":"Agreed.","reply_to":3}`)
	res := created[dto.CreatedPost](w.t, rec)
	if _, err := w.proj.ProjectThread(w.t.Context(), id, res.LogLength); err != nil {
		w.t.Fatal(err)
	}
	w.say(id, "carol", "> As @erin said\n\nquoted, not addressed.")
	w.say(id, "bob", "And @alice too.")
	return id
}

// TestMentionsLensMentionsAndReplies is AC2's selection: a post that
// mentions the viewer and a reply to the viewer's post are kept; the
// viewer's own post, though it mentions them, and a mention inside a
// quotation, which is never recorded, are not. Each member's lens is their
// own, through the response cache.
func TestMentionsLensMentionsAndReplies(t *testing.T) {
	w := newWorld(t)
	id := w.mentionsThread()
	for who, want := range map[string][]int{"erin": {2, 4}, "alice": {6}, "carol": {}, "bob": {}} {
		for range 2 {
			mentionsLens(t, who, page(t, w.get(w.viewer(who), threadPath(id)+"?lens=mentions")), want)
		}
	}
}

// TestMentionsLensLeavesOutRemoved is AC2's removed posts: a post that
// mentioned the viewer and was then deleted by its author, hidden by a
// moderator, hidden pending review or redacted is not listed, and neither
// is a removed reply to the viewer's post.
func TestMentionsLensLeavesOutRemoved(t *testing.T) {
	w := newWorld(t)
	w.addUsers("active", "erin")
	id, _ := w.thread(1, "Taken down")
	erin, bob, carol := uint64(w.users["erin"]), uint64(w.users["bob"]), uint64(w.users["carol"])
	w.post(id, "erin", "erin's post", 0)
	for range 5 {
		w.append(id, threadlog.Append{Type: threadlog.TypePost, Author: carol,
			Body: &threadlog.PostBody{Source: "@erin hello", Mentions: []uint64{erin}}})
	}
	w.post(id, "carol", "a reply to erin, then hidden", 1)
	w.append(id, threadlog.Append{Type: threadlog.TypeDelete, Author: carol, Ordinal: 2, Body: &threadlog.DeleteBody{}})
	w.append(id, threadlog.Append{Type: threadlog.TypeDelete, Author: bob, Ordinal: 3,
		Body: &threadlog.DeleteBody{Reason: "off topic", ByModerator: true}})
	w.append(id, threadlog.Append{Type: threadlog.TypeDelete, Author: bob, Ordinal: 4,
		Body: &threadlog.DeleteBody{Reason: "flagged", Auto: true}})
	w.append(id, threadlog.Append{Type: threadlog.TypeDelete, Author: bob, Ordinal: 7,
		Body: &threadlog.DeleteBody{Reason: "off topic", ByModerator: true}})
	w.redactOffline(id, 5)
	// #2 to #6 mention erin and #7 replies to her #1. #2 was deleted by
	// its author, #3 hidden by a moderator, #4 hidden pending review, #5
	// redacted and #7 hidden: only #6 is shown as itself.
	for _, e := range []uint32{2, 3, 4, 5, 7} {
		if w.entry(id, e).Flags.Visible() {
			t.Fatalf("#%d is still visible", e)
		}
	}
	mentionsLens(t, "erin", page(t, w.get(w.viewer("erin"), threadPath(id)+"?lens=mentions")), []int{6})
	if p := page(t, w.get(w.viewer("bob"), threadPath(id)+"?lens=mentions")); len(p.Posts) != 0 {
		t.Errorf("bob, who removed the posts, is listed %v", ordinalsOf(p.Posts))
	}
}

// TestMentionsLensFollowsEdits is AC2's edit: an edit that removes the
// mention removes the post from the lens once projected, and one that adds
// a mention adds it. No revision's text is the reason a post is listed.
func TestMentionsLensFollowsEdits(t *testing.T) {
	w := newWorld(t)
	id := w.mentionsThread()
	edit := func(who string, n int, source string) {
		t.Helper()
		w.clock += 60_000
		res := changed(t, w.sendAs(w.viewer(who), http.MethodPatch, postPathN(id, n), fmt.Sprintf(`{"source":%q}`, source)))
		if _, err := w.proj.ProjectThread(t.Context(), id, res.LogLength); err != nil {
			t.Fatal(err)
		}
	}
	edit("carol", 2, "Over to you, whoever.")
	mentionsLens(t, "erin after the edit", page(t, w.get(w.viewer("erin"), threadPath(id)+"?lens=mentions")), []int{4})
	edit("carol", 2, "Over to you after all, @erin.")
	mentionsLens(t, "erin after the second edit", page(t, w.get(w.viewer("erin"), threadPath(id)+"?lens=mentions")), []int{2, 4})
}

// TestMentionsLensAnonymous: a viewer who is not a member, anonymous or
// suspended, is mentioned by nobody here: 200, shown 0, never an error.
func TestMentionsLensAnonymous(t *testing.T) {
	w := newWorld(t)
	id := w.mentionsThread()
	w.addUsers("suspended", "dave")
	w.append(id, threadlog.Append{Type: threadlog.TypePost, Author: uint64(w.users["carol"]),
		Body: &threadlog.PostBody{Source: "@dave hi", Mentions: []uint64{uint64(w.users["dave"])}}})
	for name, v := range map[string]domain.Viewer{"anonymous": domain.Anonymous(), "suspended": w.session("dave")} {
		rec := w.get(v, threadPath(id)+"?lens=mentions")
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", name, rec.Code, rec.Body)
		}
		mentionsLens(t, name, page(t, rec), []int{})
	}
}

// TestMentionsLensCacheSeparatesViewers: two members' mentions lenses, read
// in both orders and twice, are each their own, and each equals a draw from
// a service with no cache.
func TestMentionsLensCacheSeparatesViewers(t *testing.T) {
	w := newWorld(t)
	id := w.mentionsThread()
	fresh := uncached(w)
	path := threadPath(id) + "?lens=mentions"
	for _, order := range [][]string{{"erin", "alice", "bob"}, {"bob", "alice", "erin"}} {
		for _, who := range order {
			got, draw := w.get(w.viewer(who), path), fresh.get(w.viewer(who), path)
			if got.Body.String() != draw.Body.String() {
				t.Fatalf("%s: the cached page differs from a fresh draw\n got %s\nwant %s", who, got.Body, draw.Body)
			}
			want := map[string][]int{"erin": {2, 4}, "alice": {6}, "bob": {}}[who]
			mentionsLens(t, who, page(t, got), want)
		}
	}
}

// TestMentionsLensStatements: a mentions page runs the canonical page's
// statements and one more, the mentions projection's lookup, on a thread of
// 10 posts and one of 2000.
func TestMentionsLensStatements(t *testing.T) {
	st := &statements{}
	w := newWorldTraced(t, threadlog.SyncNever, st.trace)
	for seq, n := range map[uint64]int{1: 10, 2: 2000} {
		id := w.quickThread(seq, n)
		canonical, lensed := threadPath(id)+"?page=1", threadPath(id)+"?lens=mentions&page=1"
		v := w.viewer("carol")
		w.get(v, canonical)
		w.get(v, lensed)
		ranC := st.during(func() { w.get(v, canonical) })
		ranL := st.during(func() { w.get(v, lensed) })
		var lookups int
		rest := slices.DeleteFunc(slices.Clone(ranL), func(s string) bool {
			found := strings.Contains(s, "name: MentionedOrdinals ")
			if found {
				lookups++
			}
			return found
		})
		if lookups != 1 || !slices.Equal(sameShape(ranC), sameShape(rest)) {
			t.Errorf("%d posts: the mentions page ran the canonical page's %d statements plus %d lookups, want 1\ncanonical %q\nlens      %q",
				n, len(ranC), lookups, ranC, ranL)
		}
	}
}
