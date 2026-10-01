package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/service/threads"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// Tests for the unread lens (TKT-01M29CTY4J, docs/06 "the unread lens holds
// its boundary"). Its rows in the access matrix are in
// TestLensPostsMatchCanonical.

// readTo sets user's read position in id to ordinal, as a beacon would have.
func (w *world) readTo(user int64, id threadid.ID, ordinal int64) {
	w.t.Helper()
	w.write(func(q *dbq.Queries) error {
		return q.UpsertReadPosition(w.t.Context(), dbq.UpsertReadPositionParams{
			UserID: user, ThreadID: int64(id), LastReadOrdinal: ordinal, LastReadAt: 1})
	})
}

// span is the ordinals from..to.
func span(from, to int) []int {
	out := []int{}
	for o := from; o <= to; o++ {
		out = append(out, o)
	}
	return out
}

// unreadLens asserts p is an unread lens page with value, shown and the
// posts want.
func unreadLens(t *testing.T, name string, p dto.ThreadPage, value string, shown int, want []int) {
	t.Helper()
	if p.Lens == nil || p.Lens.Kind != "unread" || p.Lens.Value != value || p.Lens.Shown != shown ||
		p.Lens.Hidden != p.Thread.PostCount-shown {
		t.Errorf("%s: lens %+v, want unread %q showing %d of %d", name, p.Lens, value, shown, p.Thread.PostCount)
	}
	if got := ordinalsOf(p.Posts); !slices.Equal(got, want) {
		t.Errorf("%s: posts %v, want %v", name, got, want)
	}
}

// TestUnreadLensHoldsItsBoundary: a bare ?lens=unread answers unread:B, B the
// viewer's last_read_ordinal, and page 2 of unread:B is unchanged after the
// viewer reads past it. Asked again, the bare lens answers the new B.
func TestUnreadLensHoldsItsBoundary(t *testing.T) {
	w := newWorld(t)
	id := w.quickThread(1, 60)
	carol := w.viewer("carol")
	w.readTo(carol.UserID, id, 10)

	bare := page(t, w.get(carol, threadPath(id)+"?lens=unread"))
	unreadLens(t, "bare", bare, "10", 50, span(11, 35))
	if bare.Page != (dto.Pagination{Number: 1, Size: 25, TotalPages: 2}) {
		t.Errorf("bare: page %+v, want 1 of 2", bare.Page)
	}
	held := threadPath(id) + "?lens=unread:" + bare.Lens.Value + "&page=2"
	before := page(t, w.get(carol, held))
	unreadLens(t, "page 2 before reading", before, "10", 50, span(36, 60))

	if rec := w.sendAs(carol, http.MethodPut, readPath(id), `{"ordinal":40}`); rec.Code != http.StatusOK {
		t.Fatalf("PUT /read: %d %s", rec.Code, rec.Body)
	}
	if got := w.position("carol", id); got != 40 {
		t.Fatalf("the position is %d after reading to 40", got)
	}

	after := page(t, w.get(carol, held))
	unreadLens(t, "page 2 after reading", after, "10", 50, span(36, 60))
	if after.Page != before.Page || !slices.EqualFunc(after.Posts, before.Posts, func(a, b dto.Post) bool {
		return fmt.Sprint(a) == fmt.Sprint(b)
	}) {
		t.Errorf("page 2 of unread:10 moved when the position did: %+v then %+v", before.Page, after.Page)
	}
	if after.Viewer.LastReadOrdinal != 40 {
		t.Errorf("the viewer block says %d, want the new position 40", after.Viewer.LastReadOrdinal)
	}

	again := page(t, w.get(carol, threadPath(id)+"?lens=unread"))
	unreadLens(t, "bare, asked again", again, "40", 20, span(41, 60))
}

// TestUnreadLensStartsAtFirstUnread: the lens's first post is
// last_read_ordinal + 1, which is the post the canonical landing page holds
// and the canonical first-unread permalink serves; with no position it is
// post 1.
func TestUnreadLensStartsAtFirstUnread(t *testing.T) {
	w := newWorld(t)
	id := w.quickThread(1, 60)
	w.addUsers("active", "erin", "fred")
	for who, pos := range map[string]int64{"carol": 7, "erin": 30, "fred": 59} {
		w.readTo(w.users[who], id, pos)
	}
	for who, first := range map[string]int{"alice": 1, "carol": 8, "erin": 31, "fred": 60} {
		v := w.viewer(who)
		p := page(t, w.get(v, threadPath(id)+"?lens=unread"))
		if len(p.Posts) == 0 || p.Posts[0].Ordinal != first || p.Lens.Value != fmt.Sprint(first-1) {
			t.Errorf("%s: the lens starts at %v with value %q, want #%d", who, ordinalsOf(p.Posts), p.Lens.Value, first)
			continue
		}
		landing := page(t, w.get(v, threadPath(id)))
		if !slices.Contains(ordinalsOf(landing.Posts), first) || landing.Viewer.LastReadOrdinal != first-1 {
			t.Errorf("%s: the landing page holds %v, not first unread #%d", who, ordinalsOf(landing.Posts), first)
		}
		permalink := page(t, w.get(v, fmt.Sprintf("%s?ordinal=%d", threadPath(id), first)))
		if !slices.Contains(ordinalsOf(permalink.Posts), first) {
			t.Errorf("%s: the permalink to #%d serves %v", who, first, ordinalsOf(permalink.Posts))
		}
	}
}

// TestUnreadLensAnonymous: a viewer with no position to read from, anonymous
// or suspended, gets a bare unread lens that is empty and 200, never an
// error. unread:B is a range, and serves the posts after B to them as to
// anyone.
func TestUnreadLensAnonymous(t *testing.T) {
	w := newWorld(t)
	id := w.quickThread(1, 30)
	w.addUsers("suspended", "dave")
	w.readTo(w.users["dave"], id, 3)
	for name, v := range map[string]domain.Viewer{"anonymous": domain.Anonymous(), "suspended": w.session("dave")} {
		rec := w.get(v, threadPath(id)+"?lens=unread")
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", name, rec.Code, rec.Body)
		}
		p := page(t, rec)
		unreadLens(t, name, p, "", 0, []int{})
		if p.Page != (dto.Pagination{Number: 1, Size: 25, TotalPages: 1}) || !slices.Equal(p.Lens.Gaps, gaps(0, 30)) {
			t.Errorf("%s: page %+v gaps %v, want one empty page", name, p.Page, p.Lens.Gaps)
		}
		ranged := page(t, w.get(v, threadPath(id)+"?lens=unread:4"))
		unreadLens(t, name+", unread:4", ranged, "4", 26, span(5, 29))
	}
}

// TestUnreadLensFullyRead: a member who has read everything gets kind
// unread, shown 0, and one empty page; a boundary past the last post is the
// same for anyone, and a page past that one is not-found.
func TestUnreadLensFullyRead(t *testing.T) {
	w := newWorld(t)
	id := w.quickThread(1, 30)
	carol := w.viewer("carol")
	w.readTo(carol.UserID, id, 30)
	p := page(t, w.get(carol, threadPath(id)+"?lens=unread"))
	unreadLens(t, "fully read", p, "30", 0, []int{})
	if p.Page != (dto.Pagination{Number: 1, Size: 25, TotalPages: 1}) {
		t.Errorf("fully read: page %+v, want one empty page", p.Page)
	}
	for _, who := range []string{"", "carol"} {
		past := page(t, w.get(w.viewer(who), threadPath(id)+"?lens=unread:500"))
		unreadLens(t, who+" unread:500", past, "500", 0, []int{})
		if rec := w.get(w.viewer(who), threadPath(id)+"?lens=unread:500&page=2"); rec.Code != http.StatusNotFound {
			t.Errorf("%q: page 2 of an empty lens is %d", who, rec.Code)
		}
	}
}

// TestUnreadLensCacheSeparatesViewers: two members at different positions
// each get the posts after their own, through the response cache, in either
// order and asked twice; every page equals a draw from a service with no
// cache. A third member at the first one's position may share their entry,
// because the posts depend on the boundary and the class, and nothing else.
func TestUnreadLensCacheSeparatesViewers(t *testing.T) {
	w := newWorld(t)
	id := w.quickThread(1, 40)
	w.addUsers("active", "erin", "fred")
	positions := map[string]int64{"carol": 10, "erin": 20, "fred": 10}
	for who, pos := range positions {
		w.readTo(w.users[who], id, pos)
	}
	fresh := uncached(w)
	path := threadPath(id) + "?lens=unread"
	check := func(who string) {
		t.Helper()
		v := w.viewer(who)
		got, draw := w.get(v, path), fresh.get(v, path)
		if got.Body.String() != draw.Body.String() {
			t.Fatalf("%s: the cached page differs from a fresh draw\n got %s\nwant %s", who, got.Body, draw.Body)
		}
		pos := int(positions[who])
		unreadLens(t, who, page(t, got), fmt.Sprint(pos), 40-pos, span(pos+1, min(pos+25, 40)))
	}
	for _, order := range [][]string{{"carol", "erin"}, {"erin", "carol"}} {
		for _, who := range order {
			check(who)
		}
	}
	before := w.svc.Responses.Stats()
	check("fred")
	if after := w.svc.Responses.Stats(); after.Hits != before.Hits+1 || after.Inserts != before.Inserts {
		t.Errorf("fred, at carol's position, was not served carol's entry: %+v then %+v", before, after)
	}
}

// TestUnreadLensStatements: an unread page runs exactly the statements the
// canonical page runs, bare and with a boundary, for an anonymous viewer and
// a member with a position. The boundary is the position View reads for the
// viewer block, so the lens costs no statement of its own. An anonymous
// viewer's bare lens has no posts, and still runs the one users lookup, for
// the thread's starter (TKT-01M29CTY58).
func TestUnreadLensStatements(t *testing.T) {
	st := &statements{}
	w := newWorldTraced(t, threadlog.SyncNever, st.trace)
	id := w.quickThread(1, 60)
	w.readTo(w.users["carol"], id, 30)
	for _, c := range []struct{ who, lens string }{{"", "unread"}, {"", "unread:30"}, {"carol", "unread"}, {"carol", "unread:5"}} {
		v := w.viewer(c.who)
		canonical, lensed := threadPath(id), threadPath(id)+"?lens="+c.lens
		w.get(v, canonical)
		w.get(v, lensed)
		var cr, l *httptest.ResponseRecorder
		ranC := st.during(func() { cr = w.get(v, canonical) })
		ranL := st.during(func() { l = w.get(v, lensed) })
		if cr.Code != http.StatusOK || l.Code != http.StatusOK {
			t.Fatalf("%q %s: %d and %d", c.who, c.lens, cr.Code, l.Code)
		}
		if who := c.who + " " + c.lens; !slices.Equal(ranC, ranL) {
			t.Errorf("%q: the canonical page ran %d statements, the unread lens %d\ncanonical %q\nlens      %q",
				who, len(ranC), len(ranL), ranC, ranL)
		}
	}
}

// TestUnreadLensOrdinal: under the lens a permalink serves the lens page
// holding the post, or the page holding the first unread post after it, so
// first unread at last_read + 1 lands on the lens's first page; an ordinal
// past the thread is not-found.
func TestUnreadLensOrdinal(t *testing.T) {
	w := newWorld(t)
	id := w.quickThread(1, 60)
	carol := w.viewer("carol")
	for _, c := range []struct {
		ordinal uint32
		page    int
	}{{1, 1}, {11, 1}, {35, 1}, {36, 2}, {60, 2}} {
		res, err := w.svc.View(t.Context(), carol, id, threads.Query{Lens: "unread:10", Ordinal: c.ordinal}, "")
		if err != nil || res.Body.Page.Number != c.page {
			t.Errorf("unread:10 at #%d: page %+v, %v; want page %d", c.ordinal, res.Body.Page, err, c.page)
		}
	}
	if _, err := w.svc.View(t.Context(), carol, id, threads.Query{Lens: "unread:10", Ordinal: 61}, ""); problem.FromError(err) != problem.CodeNotFound {
		t.Errorf("unread:10 at #61: %v, want not-found", err)
	}
}
