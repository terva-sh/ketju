package views

import (
	"database/sql"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/terva-sh/ketju/internal/render"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/db/dbtest"
)

// searchWorld is two forums, 101 the viewer can read and 102 they cannot,
// with threads in each.
func searchWorld(t *testing.T, trace func(string)) *db.DB {
	t.Helper()
	d := dbtest.Open(t, db.Options{Path: t.TempDir() + "/ketju.db", Trace: trace})
	exec(t, d,
		`INSERT INTO forums (id, slug, name, created_at, updated_at) VALUES (101, 'open', 'Open', 0, 0)`,
		`INSERT INTO forums (id, slug, name, created_at, updated_at) VALUES (102, 'closed', 'Closed', 0, 0)`,
	)
	return d
}

// searchThread inserts a threads row with posts.
func searchThread(t *testing.T, d *db.DB, id, forum, posts int64) {
	t.Helper()
	exec(t, d, fmt.Sprintf(`INSERT INTO threads (id, forum_id, slug, title, author_id, created_at,
		post_count, last_post_at, last_post_author_id, last_post_ordinal, log_length_projected)
		VALUES (%d, %d, 'slug-%d', 'Thread %d', 1, 0, %d, 0, 1, %d, 0)`, id, forum, id, id, posts, posts))
}

func putDocs(t *testing.T, d *db.DB, docs ...db.SearchDoc) {
	t.Helper()
	if err := d.Write(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		for _, doc := range docs {
			if err := db.PutSearchDoc(t.Context(), tx, doc); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func search(t *testing.T, d *db.DB, p SearchParams) []SearchRow {
	t.Helper()
	var rows []SearchRow
	if err := d.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		var err error
		rows, err = Search(t.Context(), tx, p)
		return err
	}); err != nil {
		t.Fatalf("Search(%+v): %v", p, err)
	}
	return rows
}

func keys(rows []SearchRow) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = fmt.Sprintf("%d/%d", r.ThreadID, r.Ordinal)
	}
	return out
}

// TestSearchFiltersForumsInTheStatement is criterion 8: 30 matching posts
// in a forum the viewer cannot read, and one in a forum they can. A page of
// 25 holds the one row and nothing more, and the trace shows the forum list
// bound in the statement that matches, so no page is filled from rows the
// viewer would then lose.
func TestSearchFiltersForumsInTheStatement(t *testing.T) {
	var mu sync.Mutex
	var stmts []string
	d := searchWorld(t, func(s string) {
		mu.Lock()
		stmts = append(stmts, s)
		mu.Unlock()
	})
	searchThread(t, d, 1, 101, 1)
	searchThread(t, d, 2, 102, 30)
	docs := []db.SearchDoc{{Thread: 1, Ordinal: 1, Revision: 1, AuthorID: 5, CreatedAt: 10, SourcePlain: "needle open"}}
	for i := range 30 {
		docs = append(docs, db.SearchDoc{Thread: 2, Ordinal: int64(i + 1), Revision: 1, AuthorID: 5,
			CreatedAt: int64(100 + i), SourcePlain: "needle closed"})
	}
	putDocs(t, d, docs...)
	mu.Lock()
	stmts = nil
	mu.Unlock()

	rows := search(t, d, SearchParams{Match: MatchAll([]string{"needle"}), Forums: []int64{101}, Limit: 25})
	if got := keys(rows); !slices.Equal(got, []string{"1/1"}) {
		t.Fatalf("rows = %v, want only 1/1", got)
	}
	mu.Lock()
	seen := slices.Clone(stmts)
	mu.Unlock()
	var matching []string
	for _, s := range seen {
		if strings.Contains(s, "MATCH") {
			matching = append(matching, s)
		}
	}
	if len(matching) != 1 {
		t.Fatalf("%d statements ran a MATCH, want 1: %q", len(matching), matching)
	}
	if s := matching[0]; !strings.Contains(s, "t.forum_id IN (SELECT value FROM json_each(?))") || !strings.Contains(s, "LIMIT ?") {
		t.Errorf("the matching statement does not bind the forum list and the limit together:\n%s", s)
	}
	if rows := search(t, d, SearchParams{Match: MatchAll([]string{"needle"}), Forums: nil, Limit: 25}); len(rows) != 0 {
		t.Errorf("no readable forum gave %v, want nothing", keys(rows))
	}
}

// TestSearchFiltersCombine: forum, author, after and before, alone and
// together. after is the first instant included and before the first not
// (docs/07 "Search"). Rows come newest first, and a thread with no post is
// left out.
func TestSearchFiltersCombine(t *testing.T) {
	d := searchWorld(t, nil)
	exec(t, d, `INSERT INTO forums (id, slug, name, created_at, updated_at) VALUES (103, 'other', 'Other', 0, 0)`)
	searchThread(t, d, 1, 101, 3)
	searchThread(t, d, 2, 103, 1)
	searchThread(t, d, 3, 101, 0) // no post yet
	putDocs(t, d,
		db.SearchDoc{Thread: 1, Ordinal: 0, AuthorID: 5, CreatedAt: 100, Title: "word in a title"},
		db.SearchDoc{Thread: 1, Ordinal: 1, Revision: 1, AuthorID: 5, CreatedAt: 100, SourcePlain: "word one"},
		db.SearchDoc{Thread: 1, Ordinal: 2, Revision: 1, AuthorID: 6, CreatedAt: 200, SourcePlain: "word two"},
		db.SearchDoc{Thread: 1, Ordinal: 3, Revision: 2, AuthorID: 5, CreatedAt: 300, SourcePlain: "word three"},
		db.SearchDoc{Thread: 2, Ordinal: 1, Revision: 1, AuthorID: 5, CreatedAt: 250, SourcePlain: "word other forum"},
		db.SearchDoc{Thread: 3, Ordinal: 0, AuthorID: 5, CreatedAt: 400, Title: "word in an empty thread"},
	)
	i := func(v int64) *int64 { return &v }
	all := []int64{101, 103}
	for _, c := range []struct {
		name string
		p    SearchParams
		want []string
	}{
		{"none", SearchParams{}, []string{"1/3", "2/1", "1/2", "1/1", "1/0"}},
		{"forum", SearchParams{Forum: i(101)}, []string{"1/3", "1/2", "1/1", "1/0"}},
		{"author", SearchParams{Author: i(6)}, []string{"1/2"}},
		{"after includes its edge", SearchParams{After: i(200)}, []string{"1/3", "2/1", "1/2"}},
		{"before excludes its edge", SearchParams{Before: i(200)}, []string{"1/1", "1/0"}},
		{"after and before", SearchParams{After: i(200), Before: i(300)}, []string{"2/1", "1/2"}},
		{"all four", SearchParams{Forum: i(101), Author: i(5), After: i(100), Before: i(301)}, []string{"1/3", "1/1", "1/0"}},
		{"nobody", SearchParams{Author: i(99)}, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			p := c.p
			p.Match, p.Forums, p.Limit = MatchAll([]string{"word"}), all, 25
			if got := keys(search(t, d, p)); !slices.Equal(got, c.want) {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
	// Pages: the second page of two starts where the first ended.
	p := SearchParams{Match: MatchAll([]string{"word"}), Forums: all, Limit: 2, Offset: 2}
	if got := keys(search(t, d, p)); !slices.Equal(got, []string{"1/2", "1/1"}) {
		t.Errorf("page 2 = %v", got)
	}
}

// TestSearchSnippetMarksMatches: the snippet is plain text with each match
// between the sentinels, the thread and forum come with it, and a title
// row's snippet is its title.
func TestSearchSnippetMarksMatches(t *testing.T) {
	d := searchWorld(t, nil)
	searchThread(t, d, 1, 101, 1)
	putDocs(t, d,
		db.SearchDoc{Thread: 1, Ordinal: 0, AuthorID: 5, CreatedAt: 1, Title: "Kettles and teapots"},
		db.SearchDoc{Thread: 1, Ordinal: 1, Revision: 1, AuthorID: 5, CreatedAt: 2,
			SourcePlain: "Bring the kettle to the boil, then warm the pot with a splash of hot water."},
	)
	open, closing := string(render.SnippetOpen), string(render.SnippetClose)
	rows := search(t, d, SearchParams{Match: MatchAll([]string{"kettle"}), Forums: []int64{101}, Limit: 25})
	if len(rows) != 1 {
		t.Fatalf("rows = %v, want the post", keys(rows))
	}
	r := rows[0]
	if want := "Bring the " + open + "kettle" + closing + " to the boil"; !strings.HasPrefix(r.Snippet, want) {
		t.Errorf("snippet = %q, want it to start %q", r.Snippet, want)
	}
	if r.ThreadSlug != "slug-1" || r.ThreadTitle != "Thread 1" || r.ForumSlug != "open" || r.ForumName != "Open" ||
		r.Revision != 1 || r.AuthorID != 5 || r.CreatedAt != 2 {
		t.Errorf("row = %+v", r)
	}
	rows = search(t, d, SearchParams{Match: MatchAll([]string{"teapots"}), Forums: []int64{101}, Limit: 25})
	if len(rows) != 1 || rows[0].Ordinal != 0 || rows[0].Snippet != "Kettles and "+open+"teapots"+closing ||
		rows[0].DocTitle != "Kettles and teapots" {
		t.Errorf("title rows = %+v", rows)
	}
}

// TestMatchAllQuotesEveryTerm: FTS5 syntax in a term is matched as words
// and never raises a syntax error, and a term of several words is a phrase.
func TestMatchAllQuotesEveryTerm(t *testing.T) {
	d := searchWorld(t, nil)
	searchThread(t, d, 1, 101, 2)
	putDocs(t, d,
		db.SearchDoc{Thread: 1, Ordinal: 1, Revision: 1, CreatedAt: 1,
			SourcePlain: `she said "near" a title:x or y with NEAR stars * and carets ^x`},
		db.SearchDoc{Thread: 1, Ordinal: 2, Revision: 1, CreatedAt: 2, SourcePlain: "a b"},
	)
	for _, c := range []struct {
		terms []string
		want  int
	}{
		{[]string{`"`}, 0},
		{[]string{`"near"`}, 1},
		{[]string{`*`}, 0},
		{[]string{`NEAR(a b)`}, 0}, // the phrase "near a b", which neither post holds
		{[]string{`NEAR(a title)`}, 1},
		{[]string{`NEAR(said title)`}, 0},
		{[]string{`title:x`}, 1},
		{[]string{`OR`}, 1},
		{[]string{`a`, `OR`, `b`}, 0},
		{[]string{`^x`}, 1},
		{[]string{`said near`}, 1}, // a phrase
		{[]string{`near said`}, 0}, // the phrase in the other order
		{[]string{`said`, `title`}, 1},
		{[]string{`stars*`}, 1},
		{[]string{`star*`}, 0}, // no prefix query
	} {
		rows := search(t, d, SearchParams{Match: MatchAll(c.terms), Forums: []int64{101}, Limit: 25})
		if len(rows) != c.want {
			t.Errorf("%q (%s): %d rows, want %d", c.terms, MatchAll(c.terms), len(rows), c.want)
		}
	}
}
