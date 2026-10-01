package search

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/events"
	"github.com/terva-sh/ketju/internal/render"
	"github.com/terva-sh/ketju/internal/service/posts"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadcache"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// TestBurstIsBatched is criterion 2's first half: 200 posts queued before
// delivery are indexed in at most two write transactions, counted where the
// indexer opens each one.
func TestBurstIsBatched(t *testing.T) {
	f := newFixture(t)
	id, created := f.thread("Burst")
	evs := []events.Event{created}
	for i := range 200 {
		evs = append(evs, f.post(id, 5, fmt.Sprintf("burst post number%d", i)))
	}
	f.begins.Store(0)
	f.deliver(evs...)
	if n := f.begins.Load(); n > 2 || n == 0 {
		t.Errorf("200 posts took %d write transactions, want 1 or 2", n)
	}
	if n := f.hits("burst"); n != 201 {
		t.Errorf("%d rows match, want 200 posts and the title", n)
	}
	if got, want := f.state(id), evs[len(evs)-1].LogLength; got != want {
		t.Errorf("search_state = %d, want %d", got, want)
	}
	f.check()
}

// TestBatchCommitsAtTheLimit: a batch over the post limit commits at a
// thread boundary and goes on, so the write connection is not held for the
// whole batch.
func TestBatchCommitsAtTheLimit(t *testing.T) {
	f := newFixture(t)
	f.x.maxPosts = 3
	var evs []events.Event
	for range 4 {
		id, created := f.thread("Limit")
		evs = append(evs, created, f.post(id, 5, "one"), f.post(id, 5, "two"))
	}
	f.begins.Store(0)
	f.deliver(evs...)
	if n := f.begins.Load(); n != 4 {
		t.Errorf("four threads of three rows each, at a limit of three, took %d transactions; want 4", n)
	}
	if n := f.hits("limit"); n != 4 {
		t.Errorf("%d titles indexed, want 4", n)
	}
}

// TestEditReplacesTheRow is criterion 2's second half: an edit replaces the
// post's row, at the new revision, and only the new text matches.
func TestEditReplacesTheRow(t *testing.T) {
	f := newFixture(t)
	id, created := f.thread("Edits")
	f.deliver(created, f.post(id, 5, "original wording"))
	f.deliver(f.edit(id, 1, "revised wording"))
	r, ok := f.row(id, 1)
	if !ok || r.Revision != 2 || r.SourcePlain != "revised wording" {
		t.Fatalf("row = %+v, %v; want revision 2 with the new text", r, ok)
	}
	if f.hits("original") != 0 || f.hits("revised") != 1 || f.hits("wording") != 1 {
		t.Errorf("hits: original %d, revised %d, wording %d; want 0, 1, 1", f.hits("original"), f.hits("revised"), f.hits("wording"))
	}
	var n int
	f.write(func(_ *dbq.Queries, tx *sql.Tx) error {
		return tx.QueryRowContext(t.Context(), `SELECT count(*) FROM search_docs WHERE thread_id = ?`, int64(id)).Scan(&n)
	})
	if n != 2 {
		t.Errorf("%d rows for the thread, want the title and one post", n)
	}
	f.check()
}

// TestOldRevisionTextIsNotIndexed is criterion 3's third test: a word only
// in revision 1 matches nothing after the edit, incrementally and after
// indexing from the first frame.
func TestOldRevisionTextIsNotIndexed(t *testing.T) {
	f := newFixture(t)
	id, created := f.thread("Revisions")
	f.deliver(created, f.post(id, 5, "first draft mentions zanzibar"))
	f.deliver(f.edit(id, 1, "second draft"))
	if n := f.hits("zanzibar"); n != 0 {
		t.Errorf("incrementally: %d hits for a word only in revision 1", n)
	}
	g := f.fromScratch()
	if strings.Contains(g, "zanzibar") {
		t.Errorf("from the first frame, the index holds revision 1:\n%s", g)
	}
}

// TestIndexIsPlainSource is criterion 3's second test: the row holds
// render.Plain of the source, so markdown syntax and link destinations
// match nothing.
func TestIndexIsPlainSource(t *testing.T) {
	f := newFixture(t)
	id, created := f.thread("Plain")
	src := "**bold** and _soft_ with [a link](https://destination.example/page) and `code`"
	f.deliver(created, f.post(id, 5, src))
	r, ok := f.row(id, 1)
	if !ok || r.SourcePlain != render.Plain(src) {
		t.Fatalf("row = %q, want %q", r.SourcePlain, render.Plain(src))
	}
	for _, w := range []string{"destination", "https", "page"} {
		if n := f.hits(w); n != 0 {
			t.Errorf("%q matches %d rows; a link destination reached the index", w, n)
		}
	}
	for _, w := range []string{"bold", "soft", "link", "code"} {
		if n := f.hits(w); n != 1 {
			t.Errorf("%q matches %d rows, want 1", w, n)
		}
	}
}

// TestTitleRow: the title is a row of its own at ordinal 0, carrying the
// thread's creator and creation time; a rename replaces it, and a title
// redaction leaves only the replacement.
func TestTitleRow(t *testing.T) {
	f := newFixture(t)
	id, created := f.thread("Original heading")
	f.deliver(created, f.post(id, 5, "body"))
	r, ok := f.row(id, 0)
	if !ok || r.Title != "Original heading" || r.AuthorID != 3 || r.CreatedAt != 1_700_000_000_000 || r.SourcePlain != "" {
		t.Fatalf("title row = %+v, %v", r, ok)
	}
	f.deliver(f.rename(id, "Second heading"))
	if r, _ := f.row(id, 0); r.Title != "Second heading" || r.AuthorID != 3 || r.CreatedAt != 1_700_000_000_000 {
		t.Errorf("after the rename the title row is %+v", r)
	}
	if f.hits("original") != 0 || f.hits("second") != 1 {
		t.Errorf("after the rename: original %d, second %d", f.hits("original"), f.hits("second"))
	}
	f.deliver(f.redactTitle(id, "Title removed"))
	if r, _ := f.row(id, 0); r.Title != "Title removed" {
		t.Errorf("after the title redaction the title row is %+v", r)
	}
	if f.hits("second") != 0 || f.hits("original") != 0 || f.hits("removed") != 1 {
		t.Errorf("after the title redaction: second %d, original %d, removed %d", f.hits("second"), f.hits("original"), f.hits("removed"))
	}
	if g := f.fromScratch(); strings.Contains(g, "heading") || !strings.Contains(g, "Title removed") {
		t.Errorf("from the first frame the title rows are:\n%s", g)
	}
	f.check()
}

// TestRemovedPostsLeaveTheIndex is criterion 9: every way a post stops being
// visible removes its row, through incremental delivery in a batch of its own
// and through indexing from the first frame alike, and a Restore brings it
// back at its current revision.
func TestRemovedPostsLeaveTheIndex(t *testing.T) {
	for _, c := range []struct {
		name   string
		remove func(f *fixture, id threadid.ID) events.Event
	}{
		{"the author's delete", func(f *fixture, id threadid.ID) events.Event {
			return f.del(id, 1, threadlog.DeleteBody{})
		}},
		{"a moderator's hide", func(f *fixture, id threadid.ID) events.Event {
			return f.del(id, 1, threadlog.DeleteBody{ByModerator: true, Reason: "spam"})
		}},
		// A moderator holding the grant through a group writes the same
		// record as one holding it by role (docs/08 "Decided 2026-09-30").
		{"a group moderator's hide", func(f *fixture, id threadid.ID) events.Event {
			return f.append(id, threadlog.Append{Type: threadlog.TypeDelete, Author: 77, Ordinal: 1,
				Body: &threadlog.DeleteBody{ByModerator: true}})
		}},
		{"the auto-hide", func(f *fixture, id threadid.ID) events.Event {
			return f.append(id, threadlog.Append{Type: threadlog.TypeDelete, Author: 0, Ordinal: 1,
				Body: &threadlog.DeleteBody{Auto: true}})
		}},
		{"a suspension's hide", func(f *fixture, id threadid.ID) events.Event {
			return f.del(id, 1, threadlog.DeleteBody{ByModerator: true, Suspension: 42})
		}},
		{"a hard redaction", func(f *fixture, id threadid.ID) events.Event {
			return f.redact(id, 1)
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t)
			id, created := f.thread("Removals")
			f.deliver(created, f.post(id, 5, "secret words"), f.post(id, 6, "neighbour stays"))
			f.deliver(f.edit(id, 1, "secret words revised"))
			if f.hits("secret") != 1 {
				t.Fatal("the post was never indexed")
			}
			f.deliver(c.remove(f, id))
			if _, ok := f.row(id, 1); ok || f.hits("secret") != 0 {
				t.Errorf("incrementally, the removed post keeps its row (%d hits)", f.hits("secret"))
			}
			if f.hits("neighbour") != 1 {
				t.Error("the neighbouring post lost its row")
			}
			if g := f.fromScratch(); strings.Contains(g, "secret") || !strings.Contains(g, "neighbour") {
				t.Errorf("from the first frame:\n%s", g)
			}
			f.check()
			if c.name == "a hard redaction" {
				return // a redaction is not restored
			}
			f.deliver(f.restore(id, 1))
			r, ok := f.row(id, 1)
			if !ok || r.Revision != 2 || r.SourcePlain != "secret words revised" {
				t.Errorf("after the Restore the row is %+v, %v; want revision 2", r, ok)
			}
			f.check()
		})
	}
}

// TestMovedIsNotVisible: this build writes no split (the record is reserved
// until TKT-01M29CXJZZ), so no post can be marked moved yet. When one is,
// its entry is not Visible, and the indexer drops a touched post whose entry
// is not Visible, as the removals above show.
func TestMovedIsNotVisible(t *testing.T) {
	if threadlog.EntryMoved.Visible() {
		t.Fatal("a moved post's entry is Visible; the indexer would keep its row")
	}
}

// TestEveryRecordTypeHasAnIndexDecision: every catalogued or reserved
// record type has a decision, and nothing else does. A type is classed as
// touching its ordinal when applying a record of it to an index can change
// a post's entry: the index's own fold is the judge, so a record added later
// that hides posts cannot be classed as changing nothing.
func TestEveryRecordTypeHasAnIndexDecision(t *testing.T) {
	for i := 1; i < 256; i++ {
		typ := threadlog.RecordType(i)
		d, ok := decisions[typ]
		switch {
		case (typ.Known() || typ.Reserved()) && !ok:
			t.Errorf("%s has no index decision", typ)
			continue
		case !typ.Known() && !typ.Reserved():
			if ok {
				t.Errorf("%s is neither catalogued nor reserved, but has a decision", typ)
			}
			continue
		case d.effect == none && d.reason == "":
			t.Errorf("%s changes nothing in the index, and its decision gives no reason", typ)
		}
		if typ.Known() && changesAnEntry(t, typ) && d.effect != touchesOrdinal {
			t.Errorf("%s can change a post's index entry, but its decision does not touch the ordinal", typ)
		}
	}
}

// changesAnEntry reports whether a record of type typ, with any of a few
// bodies, changes an index entry when applied to a visible post and to a
// deleted one.
func changesAnEntry(t *testing.T, typ threadlog.RecordType) bool {
	t.Helper()
	var bodies [][]byte
	for _, v := range []any{map[string]any{}, map[string]any{"ordinal": 1}, map[string]any{"ordinal": 1, "title": false}} {
		b, err := threadlog.EncodeBody(threadlog.EncodingCBOR, v)
		if err != nil {
			t.Fatal(err)
		}
		bodies = append(bodies, b)
	}
	for _, flags := range []threadlog.EntryFlags{0, threadlog.EntryDeleted, threadlog.EntryHiddenByMod} {
		for _, body := range bodies {
			for _, ordinal := range []uint32{1, 2} {
				before := []threadlog.IndexEntry{{Offset: 100, FrameLength: 40, Revision: 1, Author: 5, Flags: flags}}
				ix := threadlog.NewIndex(threadlog.IndexHeader{}, slices.Clone(before))
				h := threadlog.FrameHeader{Type: typ, Encoding: threadlog.EncodingCBOR, Ordinal: ordinal, Revision: 2, Actor: 5}
				if err := ix.Apply(h, body, 500, 40); err != nil {
					continue
				}
				if !slices.Equal(ix.Entries(1, 10), before) {
					return true
				}
			}
		}
	}
	return false
}

// TestStateNeverPassesTheScan is criterion 10: an index snapshot taken past
// the delivered length, where another post was hidden after the event,
// records the delivered length; the next delivery scans the hide and drops
// the post (Terva review 1695).
func TestStateNeverPassesTheScan(t *testing.T) {
	f := newFixture(t)
	id, created := f.thread("Snapshot")
	f.deliver(created, f.post(id, 5, "early post"), f.post(id, 6, "doomed words"))
	third := f.post(id, 5, "late post")
	edited := f.edit(id, 1, "early post edited")
	hidden := f.del(id, 2, threadlog.DeleteBody{ByModerator: true})
	f.deliver(third) // the thread is open, so the snapshot is at hidden.LogLength
	if got := f.state(id); got != third.LogLength {
		t.Fatalf("search_state = %d after delivering %d; the snapshot was at %d", got, third.LogLength, hidden.LogLength)
	}
	f.deliver(edited, hidden)
	if f.hits("doomed") != 0 {
		t.Error("the hide after the first delivery was skipped; the post keeps its row")
	}
	if r, _ := f.row(id, 1); r.SourcePlain != "early post edited" {
		t.Errorf("post 1 = %q", r.SourcePlain)
	}
	if got := f.state(id); got != hidden.LogLength {
		t.Errorf("search_state = %d, want %d", got, hidden.LogLength)
	}
	f.check()
}

// TestIncrementalEqualsFromScratch is criterion 4: random sequences of
// posts, edits, deletes, hides, auto-hides, restores, renames and
// redactions, delivered in random batches, give the same rows as indexing
// every thread from its first frame into a fresh database, and FTS5's
// integrity-check passes on both. Split and merge are reserved and cannot be
// written in this build.
func TestIncrementalEqualsFromScratch(t *testing.T) {
	for seed := range uint64(6) {
		t.Run(fmt.Sprint(seed), func(t *testing.T) {
			f := newFixture(t)
			randomHistory(f, seed)
			f.check()
			incremental := f.dump()
			if scratch := f.fromScratch(); scratch != incremental {
				t.Fatalf("incremental and from-scratch differ:\n--- incremental\n%s\n--- from scratch\n%s", incremental, scratch)
			}
		})
	}
}

// randomHistory writes 120 records of every kind the index cares about, the
// hard redaction of posts and titles included, to threads of f, and indexes
// them incrementally in batches as serve would.
func randomHistory(f *fixture, seed uint64) {
	f.t.Helper()
	rng := rand.New(rand.NewPCG(seed, 99))
	words := []string{"apple", "birch", "cedar", "delta", "ember", "fjord", "grove"}
	text := func() string {
		var w []string
		for range 1 + rng.IntN(4) {
			w = append(w, words[rng.IntN(len(words))])
		}
		return strings.Join(w, " ")
	}
	var ids []threadid.ID
	posts := map[threadid.ID]uint32{}
	redacted := map[threadid.ID]map[uint32]bool{}
	var pending []events.Event
	flush := func() {
		if len(pending) > 0 {
			f.deliver(pending...)
			pending = nil
		}
	}
	for range 120 {
		if len(ids) == 0 || rng.IntN(10) == 0 {
			id, ev := f.thread(text())
			ids = append(ids, id)
			redacted[id] = map[uint32]bool{}
			pending = append(pending, ev)
			continue
		}
		id := ids[rng.IntN(len(ids))]
		n := posts[id]
		o := uint32(1 + rng.IntN(int(max(n, 1))))
		ok := n > 0 && !redacted[id][o]
		var ev events.Event
		switch r := rng.IntN(20); {
		case r < 7 || n == 0:
			ev = f.post(id, uint64(5+rng.IntN(3)), text())
			posts[id]++
		case r < 10 && ok:
			ev = f.edit(id, o, text())
		case r < 12 && ok:
			ev = f.del(id, o, threadlog.DeleteBody{})
		case r < 13 && ok:
			ev = f.del(id, o, threadlog.DeleteBody{ByModerator: true})
		case r < 14 && ok:
			ev = f.append(id, threadlog.Append{Type: threadlog.TypeDelete, Ordinal: o, Body: &threadlog.DeleteBody{Auto: true}})
		case r < 16 && ok:
			ev = f.restore(id, o)
		case r < 18:
			ev = f.rename(id, text())
		case r < 19 && ok:
			flush() // a redaction closes every thread, so what is queued goes first
			ev = f.redact(id, o)
			redacted[id][o] = true
		default:
			ev = f.redactTitle(id, text())
		}
		pending = append(pending, ev)
		if rng.IntN(3) == 0 {
			flush()
		}
	}
	flush()
}

// TestDroppedEventsAreCaughtUp is criterion 6's second half: a batch
// reporting dropped events catches up every thread from durable state, the
// threads whose events were lost included.
func TestDroppedEventsAreCaughtUp(t *testing.T) {
	f := newFixture(t)
	id, created := f.thread("Dropped")
	f.deliver(created)
	f.post(id, 5, "lost event") // its event never arrives
	other, oc := f.thread("Other")
	if err := f.x.Deliver(t.Context(), events.Batch{Events: []events.Event{oc}, Dropped: 1}); err != nil {
		t.Fatal(err)
	}
	if f.hits("lost") != 1 {
		t.Error("the thread whose event was dropped was not caught up")
	}
	if f.hits("other") != 1 || f.state(other) != oc.LogLength {
		t.Error("the batch's own event was not indexed")
	}
	if l := f.x.Lag(); l.Threads != 0 {
		t.Errorf("Lag = %+v after catching up", l)
	}
}

// TestAFailedThreadKeepsItsRows is criterion 11: in one batch with two
// healthy threads, a thread whose log is damaged after posts already indexed
// keeps its rows and search_state, the healthy threads commit, and Lag
// reports the failed one.
func TestAFailedThreadKeepsItsRows(t *testing.T) {
	f := newFixture(t)
	a, ac := f.thread("Healthy one")
	bad, bc := f.thread("Damaged")
	c, cc := f.thread("Healthy two")
	f.deliver(ac, bc, cc, f.post(bad, 5, "kept words"))
	before := f.state(bad)
	keptRow, _ := f.row(bad, 1)

	damaged := f.post(bad, 5, "never indexed")
	// Flip a byte inside the new frame's body on disk; its checksum fails.
	path := bad.LogPath(f.root)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	b[damaged.LogLength-6] ^= 0xff
	if err := os.WriteFile(path, b, 0o640); err != nil {
		t.Fatal(err)
	}
	pa, pc := f.post(a, 5, "alpha arrives"), f.post(c, 5, "gamma arrives")
	f.begins.Store(0)
	f.deliver(pa, damaged, pc)
	if n := f.begins.Load(); n != 1 {
		t.Errorf("the batch took %d transactions, want 1", n)
	}
	if f.hits("alpha") != 1 || f.hits("gamma") != 1 {
		t.Error("a healthy thread did not commit beside the failed one")
	}
	if got := f.state(bad); got != before {
		t.Errorf("the failed thread's search_state moved from %d to %d", before, got)
	}
	if r, ok := f.row(bad, 1); !ok || r != keptRow {
		t.Errorf("the failed thread's row is %+v, %v; want %+v", r, ok, keptRow)
	}
	if f.hits("never") != 0 {
		t.Error("the damaged post was indexed")
	}
	if l := f.x.Lag(); l.Threads != 1 || l.Bytes <= 0 {
		t.Errorf("Lag = %+v, want the failed thread", l)
	}
	if !strings.Contains(f.log.String(), "indexing a thread for search failed") {
		t.Errorf("the failure was not logged:\n%s", f.log)
	}
	f.check()
}

// orphan puts a row for an ordinal the thread's log does not hold.
func (f *fixture) orphan(id threadid.ID) {
	f.t.Helper()
	f.write(func(_ *dbq.Queries, tx *sql.Tx) error {
		return db.PutSearchDoc(f.t.Context(), tx, db.SearchDoc{Thread: int64(id), Ordinal: 99, Revision: 1, SourcePlain: "orphaned row"})
	})
}

func (f *fixture) setState(id threadid.ID, n int64) {
	f.t.Helper()
	f.write(func(q *dbq.Queries, _ *sql.Tx) error {
		return q.SetSearchState(f.t.Context(), dbq.SetSearchStateParams{ThreadID: int64(id), LogLength: n, UpdatedAt: 1})
	})
}

// TestStateAheadOfLogReindexesTheThread: a search_state past the log's end
// indexes the thread again from its first frame, after deleting every row
// it had, so a row for an ordinal the log does not hold is gone (Terva
// review 1731).
func TestStateAheadOfLogReindexesTheThread(t *testing.T) {
	f := newFixture(t)
	id, created := f.thread("Ahead")
	end := f.post(id, 5, "real post")
	f.deliver(created, end)
	f.orphan(id)
	f.setState(id, end.LogLength+10_000)
	if err := f.x.CatchUp(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, ok := f.row(id, 99); ok || f.hits("orphaned") != 0 {
		t.Error("the row for an ordinal the log does not hold survived")
	}
	if f.hits("real") != 1 || f.state(id) != end.LogLength {
		t.Errorf("hits %d, search_state %d; want 1 and %d", f.hits("real"), f.state(id), end.LogLength)
	}
	if !strings.Contains(f.log.String(), "past its log's end") {
		t.Errorf("the re-index was not logged:\n%s", f.log)
	}
	f.check()
}

// TestStateNotAtAFrameBoundaryReindexesTheThread is criterion 13's first
// half: a search_state inside a frame indexes the thread again from its
// first frame, and no row is left over from before.
func TestStateNotAtAFrameBoundaryReindexesTheThread(t *testing.T) {
	f := newFixture(t)
	id, created := f.thread("Boundary")
	first := f.post(id, 5, "first post")
	f.deliver(created, first)
	second := f.post(id, 5, "second post")
	f.orphan(id)
	f.setState(id, first.LogLength+7) // inside the second post's frame
	if err := f.x.IndexThread(t.Context(), id, second.LogLength); err != nil {
		t.Fatal(err)
	}
	if _, ok := f.row(id, 99); ok {
		t.Error("the row for an ordinal the log does not hold survived")
	}
	if f.hits("first") != 1 || f.hits("second") != 1 || f.state(id) != second.LogLength {
		t.Errorf("first %d, second %d, search_state %d", f.hits("first"), f.hits("second"), f.state(id))
	}
	if !strings.Contains(f.log.String(), "not a frame boundary") {
		t.Errorf("the re-index was not logged:\n%s", f.log)
	}
	f.check()
}

// TestCatchUpAfterARedactionSwap is criterion 13's second half: a database
// that indexed a log before ketju redact rewrote it catches up against the
// rewritten log. Its recorded length is still a frame boundary, because a
// rewrite keeps every frame's length, so catch-up resumes there, scans the
// Redacted record, and drops the row.
func TestCatchUpAfterARedactionSwap(t *testing.T) {
	f := newFixture(t)
	id, created := f.thread("Swap")
	f.deliver(created, f.post(id, 5, "kept text"), f.post(id, 5, "redacted text"), f.post(id, 6, "tail text"))
	at := f.state(id)
	n := f.redact(id, 2).LogLength // no event: the database copy never heard of it
	if n <= at {
		t.Fatalf("the rewritten log is %d bytes, not longer than the %d indexed", n, at)
	}
	if err := f.x.CatchUp(t.Context()); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(f.log.String(), "not a frame boundary") || strings.Contains(f.log.String(), "past its log's end") {
		t.Errorf("catch-up did not resume at the recorded length:\n%s", f.log)
	}
	if f.hits("redacted") != 0 {
		t.Error("the redacted post keeps its row")
	}
	if f.hits("kept") != 1 || f.hits("tail") != 1 || f.state(id) != n {
		t.Errorf("kept %d, tail %d, search_state %d (want %d)", f.hits("kept"), f.hits("tail"), f.state(id), n)
	}
	f.check()
}

// TestVersionChangeReindexes is criterion 12's first test: another version,
// and a missing search_meta row, each empty the index and index every
// thread again. A restart after the pass indexes nothing, and a restart in
// the middle of it indexes only the threads it had not reached.
func TestVersionChangeReindexes(t *testing.T) {
	for _, how := range []string{"another version", "no version"} {
		t.Run(how, func(t *testing.T) {
			f := newFixture(t)
			var ids []threadid.ID
			for i := range 4 {
				id, created := f.thread(fmt.Sprintf("Thread%d", i))
				f.deliver(created, f.post(id, 5, fmt.Sprintf("body%d", i)))
				ids = append(ids, id)
			}
			want := f.dump()
			f.write(func(q *dbq.Queries, tx *sql.Tx) error {
				if how == "no version" {
					return q.ClearSearchMeta(t.Context())
				}
				return q.SetSearchVersion(t.Context(), Version+1)
			})

			// A pass that stops after its first transaction, one thread to
			// a transaction.
			x := f.indexer()
			if f.hits("body0") != 0 {
				t.Fatal("Prepare left a row of the other version")
			}
			x.maxPosts = 1
			ctx, cancel := context.WithCancel(t.Context())
			x.beforeTx = func(_ context.Context, n int) {
				if n == 1 {
					cancel()
				}
			}
			if err := x.CatchUp(ctx); !errors.Is(err, context.Canceled) {
				t.Fatalf("the interrupted pass returned %v", err)
			}
			if f.state(ids[0]) == 0 || f.state(ids[1]) != 0 {
				t.Fatalf("the interrupted pass indexed %d and %d bytes of the first two threads", f.state(ids[0]), f.state(ids[1]))
			}

			// The restart resumes: Prepare finds the version current, and
			// catch-up indexes the three threads left.
			y := f.indexer()
			y.maxPosts = 1
			var txs int
			y.beforeTx = func(context.Context, int) { txs++ }
			if err := y.CatchUp(t.Context()); err != nil {
				t.Fatal(err)
			}
			if txs != 3 {
				t.Errorf("the resumed pass ran %d transactions, want the 3 threads it had not reached", txs)
			}
			if got := f.dump(); got != want {
				t.Errorf("after the pass:\n%s\nwant:\n%s", got, want)
			}

			// A restart after the pass indexes nothing.
			z := f.indexer()
			z.beforeTx = func(context.Context, int) { t.Error("a restart after a finished pass indexed something") }
			if err := z.CatchUp(t.Context()); err != nil {
				t.Fatal(err)
			}
			f.check()
		})
	}
}

// TestVersionChangeServesNoOldRows is criterion 12's second test: an index
// built by an earlier extraction that kept link destinations, then a version
// change with the pass stalled after one thread. A link destination matches
// nothing, a word in a thread not yet reached matches nothing while
// Indexing is true, and the rebuilt thread is found.
func TestVersionChangeServesNoOldRows(t *testing.T) {
	f := newFixture(t)
	var ids []threadid.ID
	for i, w := range []string{"first", "second", "third"} {
		id, created := f.thread(fmt.Sprintf("Thread %d", i))
		f.deliver(created, f.post(id, 5, w+" thread see [the docs](https://linkdest.example)"))
		ids = append(ids, id)
	}
	// What the old extraction made of every post: the destination kept.
	f.write(func(q *dbq.Queries, tx *sql.Tx) error {
		for i, w := range []string{"first", "second", "third"} {
			if err := db.PutSearchDoc(t.Context(), tx, db.SearchDoc{Thread: int64(ids[i]), Ordinal: 1, Revision: 1,
				AuthorID: 5, SourcePlain: w + " thread see the docs https://linkdest.example"}); err != nil {
				return err
			}
		}
		return q.SetSearchVersion(t.Context(), Version-1)
	})
	if f.hits("linkdest") != 3 {
		t.Fatal("the old index does not hold the destinations")
	}

	x := f.indexer() // Prepare: the version differs, so the index is emptied
	x.maxPosts = 1
	g := newGate(1)
	x.beforeTx = g.hook
	done := make(chan error, 1)
	go func() { done <- x.CatchUp(t.Context()) }()
	g.wait(t)

	if n := f.hits("linkdest"); n != 0 {
		t.Errorf("a link destination matches %d rows of the old version", n)
	}
	if n := f.hits("second") + f.hits("third"); n != 0 {
		t.Errorf("threads the pass has not reached match %d rows", n)
	}
	if !x.Indexing() {
		t.Error("Indexing is false while the pass runs")
	}
	if f.hits("first") != 1 {
		t.Error("the thread the pass rebuilt is not found")
	}
	close(g.open)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if x.Indexing() || f.hits("second") != 1 || f.hits("third") != 1 || f.hits("linkdest") != 0 {
		t.Errorf("after the pass: indexing %v, second %d, third %d, linkdest %d",
			x.Indexing(), f.hits("second"), f.hits("third"), f.hits("linkdest"))
	}
}

// TestLagIsReported is criterion 5's first test: Lag counts the threads and
// bytes a stalled delivery holds, and the log has one line when the index
// falls behind and one when it catches up.
func TestLagIsReported(t *testing.T) {
	f := newFixture(t)
	a, ac := f.thread("Lag one")
	b, bc := f.thread("Lag two")
	f.deliver(ac, bc)
	pa, pb := f.post(a, 5, "more"), f.post(b, 5, "more")
	logged := len(f.log.String())

	g := newGate(0)
	f.x.beforeTx = g.hook
	done := make(chan struct{})
	go func() {
		defer close(done)
		f.deliver(pa, pb)
	}()
	g.wait(t)
	l := f.x.Lag()
	if want := (pa.LogLength - ac.LogLength) + (pb.LogLength - bc.LogLength); l.Threads != 2 || l.Bytes != want {
		t.Errorf("Lag while stalled = %+v, want 2 threads and %d bytes", l, want)
	}
	close(g.open)
	<-done
	if l := f.x.Lag(); l != (Lag{}) {
		t.Errorf("Lag after the stall = %+v", l)
	}
	log := f.log.String()[logged:]
	if n := strings.Count(log, "the search index is behind"); n != 1 {
		t.Errorf("%d 'behind' lines, want 1:\n%s", n, log)
	}
	if n := strings.Count(log, "the search index caught up"); n != 1 {
		t.Errorf("%d 'caught up' lines, want 1:\n%s", n, log)
	}
}

// TestAppendDoesNotWaitForTheIndexer is criterion 5's second test: with the
// indexer subscribed to a real bus and its delivery stalled, appends through
// the one append seam still return, and the indexer catches up once
// released.
func TestAppendDoesNotWaitForTheIndexer(t *testing.T) {
	f := newFixture(t)
	id, created := f.thread("Not waiting")
	f.deliver(created)

	bus := events.New(events.Options{})
	if err := bus.Subscribe(f.x, 4); err != nil {
		t.Fatal(err)
	}
	if err := bus.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	g := newGate(0)
	f.x.beforeTx = g.hook
	w := &posts.Writer{Publish: bus.Publish}

	appended := make(chan struct{})
	go func() {
		defer close(appended)
		for i := range 20 {
			err := f.cache.With(id, func(th *threadcache.Thread) error {
				_, err := w.Append(th, threadlog.Append{Type: threadlog.TypePost, Author: 5,
					Body: &threadlog.PostBody{Source: fmt.Sprintf("reply%d while stalled", i)}})
				return err
			})
			if err != nil {
				t.Error(err)
				return
			}
		}
	}()
	g.wait(t)
	select {
	case <-appended:
	case <-time.After(10 * time.Second):
		t.Fatal("appends waited for the stalled indexer")
	}
	close(g.open)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := bus.Close(ctx); err != nil {
		t.Fatal(err)
	}
	// Some events may have been dropped while stalled; a sweep recovers them.
	if err := f.x.CatchUp(t.Context()); err != nil {
		t.Fatal(err)
	}
	if n := f.hits("stalled"); n != 20 {
		t.Errorf("%d replies indexed after the stall, want 20", n)
	}
}

// TestIndexerNeverWritesALogOrIndex: every log and sidecar index the
// indexer opens is opened read-only, and indexing a thread the cache does
// not hold leaves both files byte for byte as they were.
func TestIndexerNeverWritesALogOrIndex(t *testing.T) {
	f := newFixture(t)
	id, created := f.thread("Read only")
	evs := []events.Event{created}
	for i := range 5 {
		evs = append(evs, f.post(id, 5, fmt.Sprintf("post%d", i)))
	}
	if err := f.cache.Close(); err != nil {
		t.Fatal(err)
	}
	f.x.threads = nil // nothing open: every file is opened here
	sum := func(path string) [32]byte {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return sha256.Sum256(b)
	}
	logSum, idxSum := sum(id.LogPath(f.root)), sum(id.IndexPath(f.root))
	logInfo, _ := os.Stat(id.LogPath(f.root))

	var opened []string
	var mu sync.Mutex
	open := f.x.open
	f.x.open = func(path string) (logFile, error) {
		lf, err := open(path)
		if err == nil {
			if file, ok := lf.(*os.File); !ok {
				t.Errorf("the opener returned %T", lf)
			} else if _, werr := file.WriteAt([]byte("x"), 0); werr == nil {
				t.Errorf("%s was opened writable", path)
			}
		}
		mu.Lock()
		opened = append(opened, path)
		mu.Unlock()
		return lf, err
	}
	f.deliver(evs...)
	if f.hits("post3") != 1 {
		t.Fatal("the thread was not indexed")
	}
	if len(opened) == 0 {
		t.Fatal("the indexer opened no log")
	}
	if sum(id.LogPath(f.root)) != logSum || sum(id.IndexPath(f.root)) != idxSum {
		t.Error("indexing changed the log or its index")
	}
	if after, _ := os.Stat(id.LogPath(f.root)); !after.ModTime().Equal(logInfo.ModTime()) {
		t.Error("indexing touched the log's mtime")
	}
}

// TestIndexerDoesNotFillTheThreadCache: catching up more threads than the
// cache holds opens none of them through the cache, so no thread a reader
// has open is evicted.
func TestIndexerDoesNotFillTheThreadCache(t *testing.T) {
	f := newFixture(t)
	if err := f.cache.Close(); err != nil {
		t.Fatal(err)
	}
	f.openCache(threadcache.Options{Bound: 2, Sync: threadlog.SyncNever})
	for i := range 6 {
		id, _ := f.thread(fmt.Sprintf("Cached%d", i))
		f.post(id, 5, "cached words")
	}
	opens, evictions := f.cache.Stats()
	if err := f.x.CatchUp(t.Context()); err != nil {
		t.Fatal(err)
	}
	if n := f.hits("cached"); n != 6 {
		t.Fatalf("%d posts indexed, want 6", n)
	}
	if o, e := f.cache.Stats(); o != opens || e != evictions {
		t.Errorf("catch-up opened %d and evicted %d threads through the cache", o-opens, e-evictions)
	}
}

// TestQuarantinedThreadIsSkipped: a thread the projection quarantined is
// neither caught up nor indexed from an event, and is not counted behind.
func TestQuarantinedThreadIsSkipped(t *testing.T) {
	f := newFixture(t)
	id, created := f.thread("Quarantined")
	ev := f.post(id, 5, "held back")
	f.x.quarantined = func(q threadid.ID) bool { return q == id }
	f.deliver(created, ev)
	if err := f.x.CatchUp(t.Context()); err != nil {
		t.Fatal(err)
	}
	if f.hits("held") != 0 || f.state(id) != 0 || f.x.Lag().Threads != 0 {
		t.Errorf("a quarantined thread was indexed or counted: hits %d, state %d, lag %+v",
			f.hits("held"), f.state(id), f.x.Lag())
	}
}

// TestRetryRecoversAFailedThread: a thread held after a failure is indexed
// by Retry once its log reads again, and leaves the behind set.
func TestRetryRecoversAFailedThread(t *testing.T) {
	f := newFixture(t)
	id, created := f.thread("Retry")
	ev := f.post(id, 5, "eventually indexed")
	open := f.x.open
	fail := true
	f.x.open = func(path string) (logFile, error) {
		if fail {
			return nil, errors.New("disk on fire")
		}
		return open(path)
	}
	f.deliver(created, ev)
	if l := f.x.Lag(); l.Threads != 1 {
		t.Fatalf("Lag = %+v after a failed open", l)
	}
	fail = false
	if err := f.x.Retry(t.Context()); err != nil {
		t.Fatal(err)
	}
	if f.hits("eventually") != 1 || f.x.Lag().Threads != 0 {
		t.Errorf("after Retry: hits %d, lag %+v", f.hits("eventually"), f.x.Lag())
	}
}
