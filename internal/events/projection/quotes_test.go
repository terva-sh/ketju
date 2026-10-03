package projection

import (
	"database/sql"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync/atomic"
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

// asAfterMigration0030 leaves the database as migrations 0025 and 0030 leave
// one projected before them: post_quotes empty, since no build wrote it, and
// a backfill asked for on every projection_state row, whose length 0030
// keeps (db TestBackfillMigrationKeepsTheProjectedLength holds the
// migrations to that).
func (f *fixture) asAfterMigration0030() {
	f.t.Helper()
	f.exec(`DELETE FROM post_quotes`)
	f.exec(`UPDATE projection_state SET backfill = 1`)
}

// backfillOwed is the threads whose projection_state still asks for a
// backfill.
func (f *fixture) backfillOwed() []threadid.ID {
	f.t.Helper()
	var out []threadid.ID
	err := f.db.Read(f.t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		rows, err := tx.QueryContext(f.t.Context(), `SELECT thread_id FROM projection_state WHERE backfill = 1 ORDER BY thread_id`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				return err
			}
			out = append(out, threadid.ID(id))
		}
		return rows.Err()
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return out
}

// quotingThreads is n threads whose posts quote each other, with an edit and
// a moderator's delete, each projected to its end.
func (f *fixture) quotingThreads(n int) []threadid.ID {
	f.t.Helper()
	f.users(5, 6, 7)
	var ids []threadid.ID
	for i := range n {
		id, _ := f.thread(fmt.Sprintf("Quotes %d", i), "meta")
		f.quoter(id, 5, "first")
		f.quoter(id, 6, quote(1))
		f.quoter(id, 7, quote(1, 2))
		f.append(id, threadlog.Append{Type: threadlog.TypeEdit, Author: 6, Ordinal: 2,
			Body: &threadlog.EditBody{Source: quote(3)}})
		// A moderator's delete, which writes an audit row a replay must not
		// write again.
		end := f.append(id, threadlog.Append{Type: threadlog.TypeDelete, Author: 5, Ordinal: 3,
			Body: &threadlog.DeleteBody{ByModerator: true}})
		f.project(id, end)
		ids = append(ids, id)
	}
	return ids
}

// TestCatchUpBackfillsTheBacklinksAfterTheMigration (Terva reviews 1998 and
// 2074): a database whose threads were projected before post_quotes existed
// gets every backlink at the next start, because catch-up replays each log
// whose backfill is asked for over the rows already there, though none has a
// gap. It ends on exactly the rows a projection that had the table all along
// made, audit rows included, with no backfill left owed.
func TestCatchUpBackfillsTheBacklinksAfterTheMigration(t *testing.T) {
	f := newFixture(t)
	ids := f.quotingThreads(3)
	want := f.dump()
	audits := len(f.auditRows())
	if f.countQuotes() != 9 || audits == 0 {
		t.Fatalf("the setup has %d backlinks and %d audit rows, want 9 and some", f.countQuotes(), audits)
	}

	f.asAfterMigration0030()
	var reopens atomic.Int64
	rep, err := f.w.CatchUp(t.Context(), reopenWith(f.cache, &reopens))
	if err != nil {
		t.Fatal(err)
	}
	if rep.Backfilled != len(ids) || rep.Closed != 0 || len(rep.Failed) != 0 || len(rep.Corrupt) != 0 {
		t.Fatalf("catch-up report %+v, want every thread backfilled and no gap closed", rep)
	}
	if got := f.dump(); got != want {
		t.Fatalf("after the migration and a catch-up the rows differ:\n%s\n---\n%s", want, got)
	}
	if got := len(f.auditRows()); got != audits {
		t.Fatalf("the replay left %d audit rows, want the %d there were", got, audits)
	}
	if owed := f.backfillOwed(); len(owed) != 0 {
		t.Fatalf("backfills still owed after the catch-up: %v", owed)
	}

	// Once is enough: the next catch-up finds nothing to do.
	rep, err = f.w.CatchUp(t.Context(), reopenWith(f.cache, &reopens))
	if err != nil {
		t.Fatal(err)
	}
	if rep.Backfilled != 0 || rep.Closed != 0 || rep.Bytes != 0 {
		t.Fatalf("a second catch-up report %+v, want nothing replayed", rep)
	}
}

// TestBackfillKeepsTheCorruptionCheck is TKT-01M41MTXWH (Terva review 2074):
// a log cut before the upgrade, at a frame boundary above its header or to
// exactly its header, is still a log shorter than its projection while a
// backfill is owed. Catch-up quarantines both without opening either,
// leaves their rows and their backfill as they were, and backfills the
// rest. A gap and a backfill on one thread are closed by one replay.
func TestBackfillKeepsTheCorruptionCheck(t *testing.T) {
	f := newFixture(t)
	ids := f.quotingThreads(4)
	toFrame, toHeader, grown, intact := ids[0], ids[1], ids[2], ids[3]
	// The last whole frame of toFrame ends here; the cut is at the one before.
	cut := logSize(t, f.root, toFrame)
	f.asAfterMigration0030()
	before := f.dump()
	end := f.quoter(grown, 6, quote(2)) // a gap as well as a backfill
	c := f.restart()
	ends := frameEnds(t, f.root, toFrame)
	if ends[len(ends)-1] != cut || len(ends) < 2 {
		t.Fatalf("frames of %s end at %v", toFrame, ends)
	}
	for id, n := range map[threadid.ID]int64{toFrame: ends[len(ends)-2], toHeader: threadlog.FileHeaderSize} {
		if err := os.Truncate(id.LogPath(f.root), n); err != nil {
			t.Fatal(err)
		}
	}

	var reopens atomic.Int64
	rep, err := f.w.CatchUp(t.Context(), reopenWith(c, &reopens))
	if err != nil {
		t.Fatal(err)
	}
	corrupt := []threadid.ID{toFrame, toHeader}
	slices.Sort(corrupt)
	if !slices.Equal(rep.Corrupt, corrupt) || rep.Backfilled != 2 || rep.Closed != 1 || len(rep.Failed) != 0 {
		t.Fatalf("catch-up report %+v, want %v corrupt, two backfilled and one gap closed", rep, corrupt)
	}
	if n := reopens.Load(); n != 2 {
		t.Errorf("opened %d threads, want the two it replayed", n)
	}
	for _, id := range corrupt {
		if !f.w.Quarantined(id) {
			t.Errorf("thread %s, cut to %d, is not quarantined", id, logSize(t, f.root, id))
		}
	}
	if owed := f.backfillOwed(); !slices.Equal(owed, corrupt) {
		t.Errorf("backfills owed after the catch-up: %v, want the quarantined %v", owed, corrupt)
	}
	got := f.projected()
	if got[grown] != end || got[intact] != logSize(t, f.root, intact) {
		t.Errorf("projected lengths %v, want %s at %d and %s at its end", got, grown, end, intact)
	}
	// The quarantined threads' rows are as they were; theirs are the lines
	// that name them.
	for line := range strings.Lines(before) {
		for _, id := range corrupt {
			if strings.Contains(line, fmt.Sprintf("thread_id=%d ", int64(id))) && !strings.Contains(f.dump(), line) {
				t.Errorf("catch-up changed a quarantined thread's row: %s", line)
			}
		}
	}
	if q := f.quotes(intact); !slices.Equal(q, []string{"2>3", "3>1", "3>2"}) {
		t.Errorf("the intact thread's backlinks are %v", q)
	}
	if q := f.quotes(grown); !slices.Equal(q, []string{"2>3", "3>1", "3>2", "4>2"}) {
		t.Errorf("the grown thread's backlinks are %v", q)
	}
}

// TestAnyProjectionPaysAnOwedBackfill: a backfill catch-up could not finish
// (its thread held, say) is paid by the next projection of the thread from
// any path, here an event's, which replays from the header rather than from
// the recorded length, even when the event reports no more than that.
func TestAnyProjectionPaysAnOwedBackfill(t *testing.T) {
	f := newFixture(t)
	ids := f.quotingThreads(1)
	want := f.dump()
	f.asAfterMigration0030()
	end := f.projected()[ids[0]]
	f.project(ids[0], end)
	if got := f.dump(); got != want {
		t.Fatalf("the event's projection did not backfill:\n%s\n---\n%s", want, got)
	}
	if owed := f.backfillOwed(); len(owed) != 0 {
		t.Fatalf("backfills still owed: %v", owed)
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

// frameEnds is the offset at which each whole frame of thread id's log under
// root ends, in order.
func frameEnds(t *testing.T, root string, id threadid.ID) []int64 {
	t.Helper()
	f, err := os.Open(id.LogPath(root))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var ends []int64
	err = threadlog.ScanFramesFrom(f, threadlog.FileHeaderSize, logSize(t, root, id), func(rec threadlog.ScanRecord) bool {
		if rec.Err != nil {
			t.Fatalf("thread %s: frame at %d: %v", id, rec.Offset, rec.Err)
		}
		ends = append(ends, rec.Offset+int64(rec.Size))
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	return ends
}
