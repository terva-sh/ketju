package threads

import (
	"database/sql"
	"fmt"
	"reflect"
	"slices"
	"testing"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/render"
	"github.com/terva-sh/ketju/internal/service/respcache"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadcache"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// TestProjectedQuotesMatchTheCreateRender is TKT-01M29BKBM7's criterion 3
// for quotes (TKT-01M3YME9AZ): a post created through the service, quoting
// posts that exist, leaves post_quotes holding exactly the posts the
// create-time render reported in Result.Quotes (renderSource, which Reply
// renders with), each once. That holds for a deleted quoted post, which the
// render reports as a tombstone, and for a quotation nested in another.
func TestProjectedQuotesMatchTheCreateRender(t *testing.T) {
	w := newFlagWorld(t)
	w.post("the second post")
	w.post("the third post")
	w.append(threadlog.Append{Type: threadlog.TypeDelete, Author: uint64(w.users["author"]), Ordinal: 3,
		Body: &threadlog.DeleteBody{}})
	q := func(o int) string { return fmt.Sprintf("> [quote=#%d]\n> quoted\n\n", o) }
	for _, c := range []struct {
		source string
		quotes int // how many distinct posts the render must report
	}{
		{q(1) + "one quotation", 1},
		{q(2) + q(1) + q(2) + "a repeated one, out of order", 2},
		{q(3) + "a deleted post", 1},
		{"> [quote=#2]\n> outer\n>\n> > [quote=#1]\n> > inner\n\nnested", 2},
		{"no quotation at all", 0},
	} {
		source := c.source
		var r render.Result
		if err := w.cache.With(w.id, func(th *threadcache.Thread) error {
			var err error
			r, err = w.svc.renderSource(t.Context(), th, source, nil)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		var want []string
		for _, quoted := range r.Quotes {
			want = append(want, fmt.Sprint(quoted.Ordinal))
		}
		slices.Sort(want)
		want = slices.Compact(want)
		if len(want) != c.quotes {
			t.Fatalf("%q: the create render reported %v, want %d posts", source, want, c.quotes)
		}

		w.clock += 60_000
		res, err := w.svc.Reply(t.Context(), w.viewer("rep1"), w.id, ReplyRequest{Source: source})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.proj.ProjectThread(t.Context(), w.id, res.LogLength); err != nil {
			t.Fatal(err)
		}
		var got []string
		if err := w.db.Read(t.Context(), func(dq *dbq.Queries, _ *sql.Tx) error {
			rows, err := dq.PostQuotes(t.Context(), dbq.PostQuotesParams{ThreadID: int64(w.id), Ordinals: []int64{int64(res.Ordinal)}})
			for _, row := range rows {
				got = append(got, fmt.Sprint(row.QuotedOrdinal))
			}
			return err
		}); err != nil {
			t.Fatal(err)
		}
		slices.Sort(got)
		if !slices.Equal(got, want) {
			t.Errorf("%q: post_quotes holds %v for #%d; the create render reported %v", source, got, res.Ordinal, want)
		}
	}
}

// TestQuotedByFollowsTheProjection (TKT-01M3YME9AZ): quoted_by is read from
// the post_quotes projection, so a page drawn while the projection lags
// shows no backlink for a quoting post it has not reached, and the response
// cache serves that page again only while the projection stays where it
// was. Once the projection catches up, the next request is drawn afresh,
// shows the backlink, and carries a new ETag.
func TestQuotedByFollowsTheProjection(t *testing.T) {
	w := newRepliesWorld(t)
	c := w.cache()
	s := w.service(c, nil)
	s.Responses = respcache.New(1 << 20)
	id := threadid.ID(uint64(1_757_500_000_000)<<16 | 1)
	th, err := c.Create(id, uint64(w.forum), 1)
	if err != nil {
		t.Fatal(err)
	}
	var length int64
	add := func(a threadlog.Append) {
		t.Helper()
		a.Author = uint64(w.author)
		a.TimeMS = 1_757_500_000_000 + length
		res, err := th.Actor.Append(a)
		if err != nil {
			t.Fatalf("appending %s: %v", a.Type, err)
		}
		length = res.LogLength
	}
	project := func() {
		t.Helper()
		if _, err := w.proj.ProjectThread(t.Context(), id, length); err != nil {
			t.Fatal(err)
		}
	}
	add(threadlog.Append{Type: threadlog.TypeThreadCreated,
		Body: &threadlog.ThreadCreatedBody{Title: "Quotes", Slug: "quotes", ForumID: uint64(w.forum)}})
	add(threadlog.Append{Type: threadlog.TypePost, Body: &threadlog.PostBody{Source: "the first post"}})
	project()
	add(threadlog.Append{Type: threadlog.TypePost, Body: &threadlog.PostBody{Source: "> [quote=#1]\n> the first\n\nquoting it"}})
	c.Release(th)

	view := func(ifNoneMatch string) Result {
		t.Helper()
		res, err := s.View(t.Context(), domain.Anonymous(), id, Query{Page: 1}, ifNoneMatch)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	quotedBy := func(res Result) []int {
		t.Helper()
		if len(res.Body.Posts) != 2 {
			t.Fatalf("the page holds %d posts, want 2", len(res.Body.Posts))
		}
		return res.Body.Posts[0].QuotedBy
	}

	lagging := view("")
	if got := quotedBy(lagging); !slices.Equal(got, []int{}) {
		t.Fatalf("drawn before the projection reached #2, #1 is quoted by %v", got)
	}
	hits := s.Responses.Stats().Hits
	if again := view(""); !slices.Equal(quotedBy(again), []int{}) || again.ETag != lagging.ETag ||
		s.Responses.Stats().Hits != hits+1 {
		t.Fatalf("a second request at the same projection was not the cached page: %v, %q, stats %+v",
			quotedBy(again), again.ETag, s.Responses.Stats())
	}

	project()
	caught := view(lagging.ETag)
	if caught.NotModified {
		t.Fatal("the page drawn before the projection caught up answered 304 after it")
	}
	if got := quotedBy(caught); !slices.Equal(got, []int{2}) {
		t.Fatalf("after the projection caught up, #1 is quoted by %v, want [2]", got)
	}
	if caught.ETag == lagging.ETag {
		t.Fatalf("the ETag %q did not move with the backlink", caught.ETag)
	}
}

// TestAMovedQuotingPostIsNotABacklink (TKT-01M3YME9AZ): the filter drops a
// quoting post whose index entry is moved, though post_meta, which lags the
// index, still lists it. So does every other removal, and a quoting post
// past the post count the page was drawn at. No record moves a post yet
// (TKT-01M29CXJZZ), so the filter is tested here, directly, and the
// moving-posts lane's end-to-end tests cover the rest.
func TestAMovedQuotingPostIsNotABacklink(t *testing.T) {
	flags := []threadlog.EntryFlags{
		0,                          // #1, quoted
		0,                          // #2, visible
		threadlog.EntryMoved,       // #3
		threadlog.EntryDeleted,     // #4
		threadlog.EntryHiddenByMod, // #5
		threadlog.EntryHiddenByMod | threadlog.EntryAutoHidden, // #6
		threadlog.EntryDeleted | threadlog.EntryRedacted,       // #7
		threadlog.EntryEdited,                                  // #8, visible
		0,                                                      // #9, past the page's count
	}
	entries := make([]threadlog.IndexEntry, len(flags))
	for i, f := range flags {
		entries[i] = threadlog.IndexEntry{Offset: uint64(64 * (i + 1)), Revision: 1, Flags: f}
	}
	ix := threadlog.NewIndex(threadlog.IndexHeader{ThreadID: 1, PostCount: uint32(len(entries))}, entries)
	var rows []dbq.QuotedByRow
	for _, o := range []int64{9, 8, 7, 6, 5, 4, 3, 2} {
		rows = append(rows, dbq.QuotedByRow{QuotedOrdinal: 1, Ordinal: o})
	}
	// A quotation of #2 by the moved #3 alone leaves #2 nothing; one of a
	// post the index lacks names nobody the page could show.
	rows = append(rows, dbq.QuotedByRow{QuotedOrdinal: 2, Ordinal: 3}, dbq.QuotedByRow{QuotedOrdinal: 1, Ordinal: 40})

	got := visibleQuoting(ix, 8, rows)
	want := map[uint32][]int{1: {2, 8}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("visibleQuoting kept %v, want %v", got, want)
	}

	// The moved flag alone is what drops #3: the same entry visible is kept.
	entries[2].Flags = 0
	ix = threadlog.NewIndex(threadlog.IndexHeader{ThreadID: 1, PostCount: uint32(len(entries))}, entries)
	if got := visibleQuoting(ix, 8, rows); !reflect.DeepEqual(got, map[uint32][]int{1: {2, 3, 8}, 2: {3}}) {
		t.Fatalf("with #3 visible, visibleQuoting kept %v", got)
	}
}
