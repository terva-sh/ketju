package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// Tests for the since lens (TKT-01M29CTY6E, docs/07 "the lens parameter").
// Its rows in the access matrix are in TestLensPostsMatchCanonical.

// dayThread is a thread whose posts were first written at the given UTC
// times, alice's each.
func (w *world) dayThread(seq uint64, at ...time.Time) threadid.ID {
	w.t.Helper()
	id, _ := w.thread(seq, "Across midnight")
	for i, t := range at {
		w.append(id, threadlog.Append{Type: threadlog.TypePost, Author: uint64(w.users["alice"]), TimeMS: t.UnixMilli(),
			Body: &threadlog.PostBody{Source: fmt.Sprintf("post %d", i+1)}})
	}
	return id
}

// TestSinceLensForms is AC1's grammar: a date read as midnight UTC, an RFC
// 3339 time with an offset naming the same moment, and Unix ms select the
// same posts, and the answer names the moment as Unix ms. An edit does not
// bring an older post in: the lens reads revision 1's time.
func TestSinceLensForms(t *testing.T) {
	w := newWorld(t)
	sept1 := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	id := w.dayThread(1, sept1.Add(-time.Hour), sept1.Add(-time.Millisecond), sept1, sept1.Add(time.Hour),
		sept1.Add(25*time.Hour))
	// #1 is edited after midnight; it stays out.
	w.append(id, threadlog.Append{Type: threadlog.TypeEdit, Author: uint64(w.users["alice"]), Ordinal: 1,
		TimeMS: sept1.Add(2 * time.Hour).UnixMilli(), Body: &threadlog.EditBody{Source: "edited later"}})
	ms := fmt.Sprint(sept1.UnixMilli())
	for _, form := range []string{"2026-09-01", "2026-09-01T05:30:00+05:30", "2026-08-31T20:00:00-04:00", ms,
		"2026-09-01T00:00:00Z"} {
		for _, who := range []string{"", "carol"} {
			// A query string reads + as a space, so the offset is escaped,
			// as a client building the address must.
			p := page(t, w.get(w.viewer(who), threadPath(id)+"?lens="+url.QueryEscape("since:"+form)))
			if p.Lens == nil || p.Lens.Kind != "since" || p.Lens.Value != ms || p.Lens.Shown != 3 || p.Lens.Hidden != 2 {
				t.Errorf("%q since:%s: lens %+v, want since %s showing 3 of 5", who, form, p.Lens, ms)
			}
			if got := ordinalsOf(p.Posts); !slices.Equal(got, []int{3, 4, 5}) {
				t.Errorf("%q since:%s: posts %v, want [3 4 5]", who, form, got)
			}
		}
	}
	// A moment past the last post is an empty lens, and one before the
	// first is the whole thread.
	for form, want := range map[string][]int{"2026-09-03": {}, "1970-01-01": {1, 2, 3, 4, 5}} {
		if got := ordinalsOf(page(t, w.get(w.viewer("carol"), threadPath(id)+"?lens=since:"+form)).Posts); !slices.Equal(got, want) {
			t.Errorf("since:%s: posts %v, want %v", form, got, want)
		}
	}
}

// TestSinceLensIncludesTombstones: a removed post keeps its place in the
// since lens, as the tombstone the canonical page draws for each viewer.
func TestSinceLensIncludesTombstones(t *testing.T) {
	w := newWorld(t)
	id := w.accessThread()
	from := w.entry(id, 3).CreatedMS
	for _, who := range []string{"", "carol", "bob"} {
		p := page(t, w.get(w.viewer(who), fmt.Sprintf("%s?lens=since:%d", threadPath(id), from)))
		if got := ordinalsOf(p.Posts); !slices.Equal(got, span(3, 10)) {
			t.Fatalf("%q: posts %v, want 3..10", who, got)
		}
		for _, post := range p.Posts {
			if removed := map[int]bool{3: true, 5: true, 6: true, 7: true, 8: true, 9: true}[post.Ordinal]; removed != post.Deleted && post.Ordinal != 9 {
				t.Errorf("%q: #%d deleted=%v", who, post.Ordinal, post.Deleted)
			}
		}
	}
}

// TestSinceLensStatements is AC1's cost: a since page runs exactly the
// canonical page's statements on a thread of 10 posts and one of 2000, for
// an anonymous viewer and a member.
func TestSinceLensStatements(t *testing.T) {
	st := &statements{}
	w := newWorldTraced(t, threadlog.SyncNever, st.trace)
	counts := map[string][]int{}
	for seq, n := range map[uint64]int{1: 10, 2: 2000} {
		id := w.quickThread(seq, n)
		from := w.entry(id, uint32(n-2)).CreatedMS
		canonical, lensed := threadPath(id)+"?page=1", fmt.Sprintf("%s?lens=since:%d&page=1", threadPath(id), from)
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
			if !slices.Equal(sameShape(ranC), sameShape(ranL)) {
				t.Errorf("%d posts, %q: the canonical page ran %d statements, the since lens %d\ncanonical %q\nlens      %q",
					n, who, len(ranC), len(ranL), ranC, ranL)
			}
			if p := page(t, l); p.Lens == nil || p.Lens.Shown != 3 {
				t.Fatalf("%d posts: lens %+v", n, p.Lens)
			}
			counts[who] = append(counts[who], len(ranL))
		}
	}
	for who, c := range counts {
		if len(c) != 2 || c[0] != c[1] {
			t.Errorf("%q: the since page ran %v statements on the two threads; the length changed the count", who, c)
		}
	}
}
