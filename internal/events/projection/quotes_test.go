package projection

import (
	"database/sql"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// Tests for the post_quotes projection (TKT-01M3YME9AZ).

// quote is a source quoting each ordinal in turn.
func quote(ordinals ...uint32) string {
	var b strings.Builder
	for _, o := range ordinals {
		fmt.Fprintf(&b, "> [quote=#%d]\n> quoted text\n\n", o)
	}
	b.WriteString("my answer")
	return b.String()
}

// quoter appends a post by author with source.
func (f *fixture) quoter(id threadid.ID, author uint64, source string) int64 {
	f.t.Helper()
	return f.append(id, threadlog.Append{Type: threadlog.TypePost, Author: author,
		Body: &threadlog.PostBody{Source: source}})
}

// quotes is thread id's post_quotes rows, as "quoting>quoted" in key order.
func (f *fixture) quotes(id threadid.ID) []string {
	f.t.Helper()
	var out []string
	err := f.db.Read(f.t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		rows, err := tx.QueryContext(f.t.Context(),
			`SELECT ordinal, quoted_ordinal FROM post_quotes WHERE thread_id = ? ORDER BY ordinal, quoted_ordinal`, int64(id))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var o, q int64
			if err := rows.Scan(&o, &q); err != nil {
				return err
			}
			out = append(out, fmt.Sprintf("%d>%d", o, q))
		}
		return rows.Err()
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return out
}

// countQuotes is how many post_quotes rows there are, in every thread.
func (f *fixture) countQuotes() int {
	f.t.Helper()
	var n int
	err := f.db.Read(f.t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		return tx.QueryRowContext(f.t.Context(), `SELECT count(*) FROM post_quotes`).Scan(&n)
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return n
}

// TestQuotesAreRecordedAsBacklinks: each case builds a thread whose posts 1
// and 2 quote nothing, applies its records, and checks the post_quotes rows
// after catch-up. The rows depend on the current source alone: whether the
// quoted post exists, and whether the quoting one is visible, is the
// reader's to decide.
func TestQuotesAreRecordedAsBacklinks(t *testing.T) {
	edit := func(ordinal uint32, source string, grace bool) threadlog.Append {
		return threadlog.Append{Type: threadlog.TypeEdit, Author: 6, Ordinal: ordinal,
			Body: &threadlog.EditBody{Source: source, Grace: grace}}
	}
	post := func(source string) threadlog.Append {
		return threadlog.Append{Type: threadlog.TypePost, Author: 6, Body: &threadlog.PostBody{Source: source}}
	}
	for _, c := range []struct {
		name    string
		records []threadlog.Append
		redact  []uint32
		want    []string
	}{
		{"a post quoting #1 and #2", []threadlog.Append{post(quote(2, 1))}, nil, []string{"3>1", "3>2"}},
		{"a duplicate quotation", []threadlog.Append{post(quote(1, 1, 2, 1))}, nil, []string{"3>1", "3>2"}},
		{"a self-quotation", []threadlog.Append{post(quote(3, 1))}, nil, []string{"3>1"}},
		{"an ordinal that does not exist", []threadlog.Append{post(quote(40))}, nil, []string{"3>40"}},
		{"a nested quotation", []threadlog.Append{post("> [quote=#2]\n> outer\n>\n> > [quote=#1]\n> > inner\n\nmine")},
			nil, []string{"3>1", "3>2"}},
		{"plain text names no quotation", []threadlog.Append{post("[quote=#1] inline, not a blockquote")}, nil, nil},
		{"an Edit replaces the quotations", []threadlog.Append{post(quote(1)), edit(3, quote(2), false)},
			nil, []string{"3>2"}},
		{"a grace Edit replaces them too", []threadlog.Append{post(quote(1, 2)), edit(3, quote(1), true)},
			nil, []string{"3>1"}},
		{"an Edit that removes every quotation", []threadlog.Append{post(quote(1)), edit(3, "no quote now", false)},
			nil, nil},
		{"an Edit adds quotations to a post that had none", []threadlog.Append{edit(2, quote(1), false)},
			nil, []string{"2>1"}},
		{"a Delete and a Restore leave the rows", []threadlog.Append{post(quote(1)),
			{Type: threadlog.TypeDelete, Author: 6, Ordinal: 3, Body: &threadlog.DeleteBody{}},
		}, nil, []string{"3>1"}},
		{"a Restore leaves them", []threadlog.Append{post(quote(1)),
			{Type: threadlog.TypeDelete, Author: 1, Ordinal: 3, Body: &threadlog.DeleteBody{ByModerator: true}},
			{Type: threadlog.TypeRestore, Author: 1, Ordinal: 3, Body: &threadlog.RestoreBody{}},
		}, nil, []string{"3>1"}},
		{"a Redacted post has none", []threadlog.Append{post(quote(1)), post(quote(2))}, []uint32{3}, []string{"4>2"}},
		{"a split or merge copy records none", []threadlog.Append{{Type: threadlog.TypePost, Author: 6,
			Body: &threadlog.PostBody{Source: quote(1), Origin: &threadlog.PostOrigin{ThreadID: 1 << 40, Ordinal: 9}}}},
			nil, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t)
			id, _ := f.thread("Quotes")
			f.quoter(id, 5, "first")
			end := f.quoter(id, 6, "second")
			for _, r := range c.records {
				end = f.append(id, r)
			}
			f.project(id, end)
			if len(c.redact) > 0 {
				end = f.redact(id, c.redact...)
				f.project(id, end)
				caught := f.dump()
				f.clear()
				f.project(id, end)
				if rebuilt := f.dump(); rebuilt != caught {
					t.Fatalf("catching up by the marker differs from a reindex:\n%s\n---\n%s", caught, rebuilt)
				}
			}
			if got := f.quotes(id); !slices.Equal(got, c.want) {
				t.Fatalf("post_quotes rows are %v, want %v", got, c.want)
			}
			// Projected again over itself, the rows are the same.
			before := f.dump()
			f.exec(`DELETE FROM projection_state`)
			f.project(id, end)
			if again := f.dump(); again != before {
				t.Fatalf("projecting again changed the rows:\n%s\n---\n%s", before, again)
			}
		})
	}
}

// TestReindexRebuildsTheBacklinks: reindex over emptied tables and over
// scribbled ones comes back to the post_quotes rows the incremental
// projection made, Edits and Deletes included.
func TestReindexRebuildsTheBacklinks(t *testing.T) {
	f := newFixture(t)
	id, end := f.thread("Quotes")
	f.project(id, end)
	f.quoter(id, 5, "first")
	f.project(id, f.quoter(id, 6, quote(1)))
	f.project(id, f.quoter(id, 7, quote(1, 2)))
	f.project(id, f.append(id, threadlog.Append{Type: threadlog.TypeEdit, Author: 6, Ordinal: 2,
		Body: &threadlog.EditBody{Source: quote(3)}}))
	f.project(id, f.append(id, threadlog.Append{Type: threadlog.TypeDelete, Author: 7, Ordinal: 3,
		Body: &threadlog.DeleteBody{}}))
	incremental := f.dump()
	if got, want := f.quotes(id), []string{"2>3", "3>1", "3>2"}; !slices.Equal(got, want) {
		t.Fatalf("the setup has rows %v, want %v", got, want)
	}
	for _, setup := range []struct {
		name string
		run  func()
	}{
		{"stale", func() {
			f.exec(`INSERT INTO post_quotes (thread_id, ordinal, quoted_ordinal) VALUES (?, 1, 2)`, int64(id))
			f.exec(`DELETE FROM post_quotes WHERE ordinal = 3 AND quoted_ordinal = 1`)
		}},
		{"empty", f.clear},
	} {
		t.Run(setup.name, func(t *testing.T) {
			setup.run()
			if _, err := f.w.Reindex(t.Context(), nil); err != nil {
				t.Fatal(err)
			}
			if got := f.dump(); got != incremental {
				t.Fatalf("reindexed rows differ from the incremental ones:\n%s\n---\n%s", incremental, got)
			}
		})
	}
}
