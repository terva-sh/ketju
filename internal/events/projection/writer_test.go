package projection

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/db/dbtest"
	"github.com/terva-sh/ketju/internal/store/threadcache"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// fixture is a migrated database, a thread cache writing real logs, and a
// Writer over both.
type fixture struct {
	t     testing.TB
	db    *db.DB
	root  string
	cache *threadcache.Cache
	w     *Writer
	next  atomic.Uint64 // shared by goroutines appending at once
}

func newFixture(t testing.TB) *fixture {
	t.Helper()
	d := dbtest.Open(t, db.Options{Path: filepath.Join(t.TempDir(), "db", "ketju.db")})
	root := filepath.Join(t.TempDir(), "threads")
	c, err := threadcache.New(root, threadcache.Options{Sync: threadlog.SyncNever})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	w, err := New(Options{DB: d, Root: root, Sweep: func(context.Context) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	w.now = func() time.Time { return time.UnixMilli(1_790_000_000_000) }
	return &fixture{t: t, db: d, root: root, cache: c, w: w}
}

// thread creates a thread with a ThreadCreated record and returns its id and
// the log length after it.
func (f *fixture) thread(title string, tags ...string) (threadid.ID, int64) {
	f.t.Helper()
	id := threadid.ID(uint64(1_700_000_000_000)<<16 | f.next.Add(1))
	th, err := f.cache.Create(id, 7, 1_700_000_000_000)
	if err != nil {
		f.t.Fatal(err)
	}
	f.cache.Release(th)
	return id, f.append(id, threadlog.Append{
		Type: threadlog.TypeThreadCreated, Author: 3, TimeMS: 1_700_000_000_000,
		Body: &threadlog.ThreadCreatedBody{Title: title, Slug: strings.ToLower(title), ForumID: 7, Tags: tags},
	})
}

// append writes one record and returns the log length after it.
func (f *fixture) append(id threadid.ID, a threadlog.Append) int64 {
	f.t.Helper()
	if a.TimeMS == 0 {
		a.TimeMS = 1_700_000_100_000 + int64(f.next.Add(1))*1000
	}
	var res threadlog.Result
	err := f.cache.With(id, func(th *threadcache.Thread) error {
		var err error
		res, err = th.Actor.Append(a)
		return err
	})
	if err != nil {
		f.t.Fatalf("appending %s: %v", a.Type, err)
	}
	return res.LogLength
}

func (f *fixture) post(id threadid.ID, author uint64, replyTo uint32, mentions ...uint64) int64 {
	f.t.Helper()
	return f.append(id, threadlog.Append{Type: threadlog.TypePost, Author: author,
		Body: &threadlog.PostBody{Source: "text", ReplyTo: replyTo, Mentions: mentions}})
}

func (f *fixture) project(id threadid.ID, upTo int64) {
	f.t.Helper()
	got, err := f.w.ProjectThread(f.t.Context(), id, upTo)
	if err != nil {
		f.t.Fatal(err)
	}
	if got != upTo {
		f.t.Fatalf("projected to %d, want %d", got, upTo)
	}
}

func (f *fixture) exec(query string, args ...any) {
	f.t.Helper()
	err := f.db.Write(f.t.Context(), func(q *dbq.Queries, tx *sql.Tx) error {
		_, err := tx.ExecContext(f.t.Context(), query, args...)
		return err
	})
	if err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) clear() {
	f.t.Helper()
	err := f.db.Write(f.t.Context(), func(q *dbq.Queries, tx *sql.Tx) error {
		return db.ClearProjections(f.t.Context(), q)
	})
	if err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) dump() string {
	f.t.Helper()
	return dbtest.DumpProjections(f.t, f.db)
}

// TestReplayIsIdempotentPerRecordType is AC1. For each record type a log ends
// in that record; it is projected, then projected again from the start of the
// log over the rows already there, then again into emptied tables. All three
// dumps must match, and each case also checks the row its record produced, so
// an apply that did nothing cannot pass by being trivially repeatable.
func TestReplayIsIdempotentPerRecordType(t *testing.T) {
	empty := []uint64{}
	cases := []struct {
		typ  threadlog.RecordType
		tail func(f *fixture, id threadid.ID) int64
		want []string
	}{
		{threadlog.TypeThreadCreated, func(f *fixture, id threadid.ID) int64 { return 0 },
			[]string{`threads: id=`, `title="Hello"`, `forum_id=7`, `author_id=3`, `tag="meta"`, `post_count=0`}},
		{threadlog.TypePost, func(f *fixture, id threadid.ID) int64 {
			f.post(id, 5, 0)
			return f.post(id, 6, 1, 5, 9)
		}, []string{"post_count=2", "last_post_author_id=6", "last_post_ordinal=2",
			"ordinal=2 author_id=6", "reply_to=1", "mentioned_user_id=9"}},
		{threadlog.TypeEdit, func(f *fixture, id threadid.ID) int64 {
			f.post(id, 5, 0, 4)
			f.post(id, 6, 0, 8)
			f.append(id, threadlog.Append{Type: threadlog.TypeEdit, Author: 5, Ordinal: 1,
				Body: &threadlog.EditBody{Source: "fixed", ReplyTo: 0}})
			return f.append(id, threadlog.Append{Type: threadlog.TypeEdit, Author: 6, Ordinal: 2,
				Body: &threadlog.EditBody{Source: "no mentions now", ReplyTo: 1, Mentions: &empty}})
		}, []string{"ordinal=1 author_id=5 created_at=", "current_revision=2 edited_at=17",
			"ordinal=2 author_id=6 created_at=", "reply_to=1 deleted=0",
			"mentioned_user_id=4", "mentions: 1 rows", "ordinal=2 author_id=6"}},
		{threadlog.TypeDelete, func(f *fixture, id threadid.ID) int64 {
			f.post(id, 5, 0)
			return f.append(id, threadlog.Append{Type: threadlog.TypeDelete, Author: 1, Ordinal: 1,
				Body: &threadlog.DeleteBody{Reason: "spam"}})
		}, []string{"deleted=1"}},
		{threadlog.TypeRestore, func(f *fixture, id threadid.ID) int64 {
			f.post(id, 5, 0)
			f.append(id, threadlog.Append{Type: threadlog.TypeDelete, Author: 1, Ordinal: 1, Body: &threadlog.DeleteBody{}})
			return f.append(id, threadlog.Append{Type: threadlog.TypeRestore, Author: 1, Ordinal: 1, Body: &threadlog.RestoreBody{}})
		}, []string{"ordinal=1 author_id=5", "deleted=0"}},
		{threadlog.TypeThreadRenamed, func(f *fixture, id threadid.ID) int64 {
			return f.append(id, threadlog.Append{Type: threadlog.TypeThreadRenamed, Author: 1,
				Body: &threadlog.ThreadRenamedBody{Title: "Renamed", Slug: "renamed"}})
		}, []string{`slug="renamed" title="Renamed"`}},
		{threadlog.TypeThreadMoved, func(f *fixture, id threadid.ID) int64 {
			return f.append(id, threadlog.Append{Type: threadlog.TypeThreadMoved, Author: 1,
				Body: &threadlog.ThreadMovedBody{ForumID: 12}})
		}, []string{"forum_id=12"}},
		{threadlog.TypeThreadLocked, func(f *fixture, id threadid.ID) int64 {
			return f.append(id, threadlog.Append{Type: threadlog.TypeThreadLocked, Author: 1, Body: &threadlog.ThreadLockedBody{}})
		}, []string{"locked=1"}},
		{threadlog.TypeThreadUnlocked, func(f *fixture, id threadid.ID) int64 {
			f.append(id, threadlog.Append{Type: threadlog.TypeThreadLocked, Author: 1, Body: &threadlog.ThreadLockedBody{}})
			return f.append(id, threadlog.Append{Type: threadlog.TypeThreadUnlocked, Author: 1, Body: &threadlog.ThreadUnlockedBody{}})
		}, []string{"locked=0"}},
		{threadlog.TypeThreadPinned, func(f *fixture, id threadid.ID) int64 {
			return f.append(id, threadlog.Append{Type: threadlog.TypeThreadPinned, Author: 1,
				Body: &threadlog.ThreadPinnedBody{Scope: "global", Until: 1_800_000_000_000}})
		}, []string{`pinned_scope="global" pinned_until=1800000000000`}},
		{threadlog.TypeThreadUnpinned, func(f *fixture, id threadid.ID) int64 {
			f.append(id, threadlog.Append{Type: threadlog.TypeThreadPinned, Author: 1,
				Body: &threadlog.ThreadPinnedBody{Scope: "forum"}})
			// A global unpin does not clear a forum pin.
			return f.append(id, threadlog.Append{Type: threadlog.TypeThreadUnpinned, Author: 1,
				Body: &threadlog.ThreadUnpinnedBody{Scope: "global"}})
		}, []string{`pinned_scope="forum" pinned_until=NULL`}},
		{threadlog.TypeThreadTagged, func(f *fixture, id threadid.ID) int64 {
			return f.append(id, threadlog.Append{Type: threadlog.TypeThreadTagged, Author: 1,
				Body: &threadlog.ThreadTaggedBody{Tags: []string{"tools", "zeta"}}})
		}, []string{`tag="tools"`, `tag="zeta"`, "thread_tags: 2 rows"}},
		{threadlog.TypeThreadArchived, func(f *fixture, id threadid.ID) int64 {
			return f.append(id, threadlog.Append{Type: threadlog.TypeThreadArchived, Author: 1, Body: &threadlog.ThreadArchivedBody{}})
		}, []string{"archived=1"}},
		{threadlog.TypeRedacted, func(f *fixture, id threadid.ID) int64 {
			f.post(id, 5, 0, 9)
			f.post(id, 6, 1, 8)
			return f.redact(id, 1)
		}, []string{"ordinal=1 author_id=5", "deleted=1", "mentioned_user_id=8", "mentions: 1 rows"}},
		// The source's side of a move: the posts are moved, their rows say
		// where to and at what revision, and the last post falls back to the
		// newest that stayed.
		{threadlog.TypePostsSplitOut, func(f *fixture, id threadid.ID) int64 {
			f.post(id, 5, 0)
			f.post(id, 6, 1)
			f.post(id, 7, 0)
			f.append(id, threadlog.Append{Type: threadlog.TypeEdit, Author: 6, Ordinal: 2,
				Body: &threadlog.EditBody{Source: "fixed"}})
			return f.append(id, threadlog.Append{Type: threadlog.TypePostsSplitOut, Author: 1,
				Body: &threadlog.PostsSplitOutBody{Ordinals: []uint32{2, 3}, TargetThreadID: 0x1234}})
		}, []string{"post_count=3", "last_post_author_id=5", "last_post_ordinal=1",
			"ordinal=2 author_id=6", "moved=1", "post_moves: 2 rows",
			"source_ordinal=2 source_revision=2 target_thread_id=4660 target_ordinal=NULL split_offset=",
			"source_ordinal=3 source_revision=1 target_thread_id=4660"}},
		// The target's side: a merge into a thread with a post, whose copies
		// raise post_count but not the last post, and whose rows say where
		// each copy landed.
		{threadlog.TypePostsMergedIn, func(f *fixture, id threadid.ID) int64 {
			f.post(id, 5, 0)
			f.append(id, threadlog.Append{Type: threadlog.TypePostsMergedIn, Author: 1,
				Body: &threadlog.PostsMergedInBody{SourceThreadID: 0x1234, Ordinals: []uint32{4, 5}}})
			f.append(id, threadlog.Append{Type: threadlog.TypePost, Author: 8, TimeMS: 1_700_000_000_500,
				Body: &threadlog.PostBody{Source: "copy", Origin: &threadlog.PostOrigin{ThreadID: 0x1234, Ordinal: 4}}})
			f.append(id, threadlog.Append{Type: threadlog.TypeEdit, Author: 8, Ordinal: 2, TimeMS: 1_700_000_000_600,
				Body: &threadlog.EditBody{Source: "copy, edited"}})
			return f.append(id, threadlog.Append{Type: threadlog.TypePost, Author: 9, TimeMS: 1_800_000_000_000,
				Body: &threadlog.PostBody{Source: "copy", Origin: &threadlog.PostOrigin{ThreadID: 0x1234, Ordinal: 5}}})
		}, []string{"post_count=3", "last_post_author_id=5", "last_post_ordinal=1", "post_moves: 2 rows",
			"source_ordinal=4 source_revision=NULL target_thread_id=", "target_ordinal=2 split_offset=NULL merged_offset=",
			"target_ordinal=3", "opens_thread=0", "ordinal=2 author_id=8 created_at=1700000000500 current_revision=2"}},
	}
	covered := map[threadlog.RecordType]bool{}
	for _, c := range cases {
		covered[c.typ] = true
		t.Run(c.typ.String(), func(t *testing.T) {
			f := newFixture(t)
			id, created := f.thread("Hello", "meta")
			end := max(created, c.tail(f, id))
			f.project(id, end)
			first := f.dump()
			for _, want := range c.want {
				if !strings.Contains(first, want) {
					t.Errorf("the projection lacks %q:\n%s", want, first)
				}
			}
			if !strings.Contains(first, fmt.Sprintf("log_length_projected=%d", end)) ||
				!strings.Contains(first, fmt.Sprintf("log_length=%d", end)) {
				t.Errorf("the projection does not record reaching %d:\n%s", end, first)
			}

			// Again from the start of the log, over rows scribbled with state
			// no record put there: every apply must set, not merely add to,
			// what it owns, or the stale values would survive the replay.
			f.exec(`UPDATE threads SET locked = 1 - locked, archived = 1 - archived, pinned_scope = 'global',
				pinned_until = 1, title = 'stale', slug = 'stale', forum_id = 99, post_count = 99,
				last_post_ordinal = 99, last_post_at = 1, last_post_author_id = 99`)
			f.exec(`UPDATE post_meta SET deleted = 1 - deleted, moved = 1, edited_at = 5, current_revision = 9,
				reply_to = 77, author_id = 99, created_at = 1`)
			f.exec(`INSERT INTO mentions SELECT thread_id, ordinal, 777 FROM post_meta`)
			f.exec(`INSERT INTO thread_tags (thread_id, tag) SELECT id, 'stale' FROM threads`)
			// Only the columns this thread's side of a move owns: the other
			// side's are the other thread's to restore.
			f.exec(`UPDATE post_moves SET split_offset = 1, source_revision = 9, target_thread_id = 99
				WHERE source_thread_id = ?`, int64(id))
			f.exec(`UPDATE post_moves SET merged_offset = 1, opens_thread = 1 - opens_thread, target_ordinal = 7
				WHERE target_thread_id = ?`, int64(id))
			f.exec(`UPDATE projection_state SET log_length = ?`, threadlog.FileHeaderSize)
			f.project(id, end)
			if again := f.dump(); again != first {
				t.Fatalf("replaying over stale rows did not restore them:\n%s\n---\n%s", first, again)
			}
			// And into empty tables, as reindex does.
			f.clear()
			f.project(id, end)
			if rebuilt := f.dump(); rebuilt != first {
				t.Fatalf("rebuilding from empty differs:\n%s\n---\n%s", first, rebuilt)
			}
			f.agreesWithIndex(id)
		})
	}
	for typ, d := range decisions {
		if d.apply != nil && !covered[typ] {
			t.Errorf("%s has a projection and no case here", typ)
		}
	}
}

// TestGraceEditProjection (TKT-01M29CKRHS): a grace Edit keeps a post's
// edited_at null while it has only grace edits, and a visible Edit sets it.
// A grace Edit after a visible one, which the service never writes and a
// hand-made log may hold, moves edited_at to its own time, as the index
// moves to that revision and keeps its edited flag. After each record the
// projection agrees with the index, and replaying the log over stale rows or
// into empty tables, as reindex does, gives the same rows.
func TestGraceEditProjection(t *testing.T) {
	f := newFixture(t)
	id, _ := f.thread("Hello")
	f.post(id, 5, 0)
	f.post(id, 6, 0)
	edit := func(ordinal uint32, at int64, grace bool) int64 {
		return f.append(id, threadlog.Append{Type: threadlog.TypeEdit, Author: 5, Ordinal: ordinal, TimeMS: at,
			Body: &threadlog.EditBody{Source: "fixed", Grace: grace}})
	}
	editedAt := func(ordinal int) sql.NullInt64 {
		var v sql.NullInt64
		if err := f.db.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
			return tx.QueryRowContext(t.Context(), `SELECT edited_at FROM post_meta WHERE thread_id = ? AND ordinal = ?`,
				int64(id), ordinal).Scan(&v)
		}); err != nil {
			t.Fatal(err)
		}
		return v
	}
	null := sql.NullInt64{}
	at := func(ms int64) sql.NullInt64 { return sql.NullInt64{Int64: ms, Valid: true} }

	var end int64
	for _, step := range []struct {
		name     string
		ordinal  uint32
		ms       int64
		grace    bool
		want1    sql.NullInt64
		wantBoth sql.NullInt64
	}{
		{"a grace edit", 1, 1_700_000_200_001, true, null, null},
		{"a second grace edit", 1, 1_700_000_200_002, true, null, null},
		{"a grace edit of the other post", 2, 1_700_000_200_003, true, null, null},
		{"a visible edit", 1, 1_700_000_200_004, false, at(1_700_000_200_004), null},
		{"a hand-made grace edit after it", 1, 1_700_000_200_005, true, at(1_700_000_200_005), null},
	} {
		end = edit(step.ordinal, step.ms, step.grace)
		f.project(id, end)
		if got := editedAt(1); got != step.want1 {
			t.Errorf("after %s: post 1 edited_at %+v, want %+v", step.name, got, step.want1)
		}
		if got := editedAt(2); got != step.wantBoth {
			t.Errorf("after %s: post 2 edited_at %+v, want %+v", step.name, got, step.wantBoth)
		}
		f.agreesWithIndex(id)
	}

	first := f.dump()
	f.exec(`UPDATE post_meta SET edited_at = 5`)
	f.exec(`UPDATE projection_state SET log_length = ?`, threadlog.FileHeaderSize)
	f.project(id, end)
	if again := f.dump(); again != first {
		t.Fatalf("replaying over stale rows differs:\n%s\n---\n%s", first, again)
	}
	f.clear()
	f.project(id, end)
	if rebuilt := f.dump(); rebuilt != first {
		t.Fatalf("rebuilding from empty differs:\n%s\n---\n%s", first, rebuilt)
	}
}

// TestEveryRecordTypeHasADecision is AC3: every type this build knows has a
// projection, and no other type has a decision. A known type must project:
// it has a body, so it says something about the thread. No number is held
// in reserve any more: split and merge, the last, landed in TKT-01M29CXJZZ.
func TestEveryRecordTypeHasADecision(t *testing.T) {
	for n := 1; n <= 255; n++ {
		typ := threadlog.RecordType(n)
		d, ok := decisions[typ]
		switch {
		case typ.Known() && (!ok || d.apply == nil):
			t.Errorf("%s is in the catalogue and has no projection", typ)
		case !typ.Known() && ok:
			t.Errorf("%s is not in the catalogue but has a decision", typ)
		}
	}
}

// TestProjectionKeepsTheLogReadOnly: the writer opens logs read-only, so no
// bug in it can write a log.
func TestProjectionKeepsTheLogReadOnly(t *testing.T) {
	f := newFixture(t)
	id, _ := f.thread("Hello")
	lf, err := f.w.open(id.LogPath(f.root))
	if err != nil {
		t.Fatal(err)
	}
	defer lf.Close()
	file, ok := lf.(*os.File)
	if !ok {
		t.Fatalf("the default opener returned %T", lf)
	}
	if _, err := file.WriteAt([]byte("x"), 0); err == nil {
		t.Fatal("a log opened by the writer accepted a write")
	}
}

// TestMissingRowIsAnError: an Edit whose post the projection does not have
// is an error that keeps the thread behind, not an update to nothing.
func TestMissingRowIsAnError(t *testing.T) {
	f := newFixture(t)
	id, _ := f.thread("Hello")
	end := f.post(id, 5, 0)
	f.project(id, end)
	f.exec(`DELETE FROM post_meta`)
	end = f.append(id, threadlog.Append{Type: threadlog.TypeEdit, Author: 5, Ordinal: 1, Body: &threadlog.EditBody{Source: "x"}})
	got, err := f.w.ProjectThread(t.Context(), id, end)
	if err == nil || !strings.Contains(err.Error(), "names a post the projection does not have") {
		t.Fatalf("got %v, want a missing-post error", err)
	}
	if got >= end {
		t.Fatalf("a failed projection reported reaching %d", got)
	}
}

// TestStaleEventIsANoOp: an event naming a length the projection has already
// passed, as one can after a sweep, changes nothing and reports where the
// projection is.
func TestStaleEventIsANoOp(t *testing.T) {
	f := newFixture(t)
	id, created := f.thread("Hello")
	end := f.post(id, 5, 0)
	f.project(id, end)
	before := f.dump()
	got, err := f.w.ProjectThread(t.Context(), id, created)
	if err != nil || got != end {
		t.Fatalf("a stale event gave %d, %v; want %d, nil", got, err, end)
	}
	if f.dump() != before {
		t.Fatal("a stale event changed the rows")
	}
}

// TestShortLogIsAnError: an event claiming more log than the file holds is
// refused by name, not read past the end.
func TestShortLogIsAnError(t *testing.T) {
	f := newFixture(t)
	id, end := f.thread("Hello")
	if _, err := f.w.ProjectThread(t.Context(), id, end+100); err == nil ||
		!strings.Contains(err.Error(), "shorter than") {
		t.Fatalf("got %v, want the log reported as shorter than the event", err)
	}
}

// agreesWithIndex checks every post_meta row of a thread against the
// thread's in-memory index, which is the other fold of the same log. Where
// both hold a fact they must hold the same one; log_offset in particular is
// the current revision's record, as IndexEntry.Offset is (review 389).
// edited_at is the current revision's time when the entry is edited and null
// when it is not, which is the thread page's edited_at (TKT-01M29CKRHS).
func (f *fixture) agreesWithIndex(id threadid.ID) {
	f.t.Helper()
	type row struct {
		ordinal, author, created, revision, offset int64
		replyTo, editedAt                          sql.NullInt64
		deleted                                    bool
	}
	var rows []row
	var live int64
	err := f.db.Read(f.t.Context(), func(q *dbq.Queries, tx *sql.Tx) error {
		if err := tx.QueryRowContext(f.t.Context(), `SELECT live_post_count FROM threads WHERE id = ?`,
			int64(id)).Scan(&live); err != nil {
			return err
		}
		rs, err := tx.QueryContext(f.t.Context(), `SELECT ordinal, author_id, created_at, current_revision,
			log_offset, reply_to, edited_at, deleted FROM post_meta WHERE thread_id = ? ORDER BY ordinal`, int64(id))
		if err != nil {
			return err
		}
		defer rs.Close()
		for rs.Next() {
			var r row
			if err := rs.Scan(&r.ordinal, &r.author, &r.created, &r.revision, &r.offset, &r.replyTo, &r.editedAt, &r.deleted); err != nil {
				return err
			}
			rows = append(rows, r)
		}
		return rs.Err()
	})
	if err != nil {
		f.t.Fatal(err)
	}
	err = f.cache.With(id, func(th *threadcache.Thread) error {
		if th.Index.Len() != len(rows) {
			f.t.Errorf("the index holds %d posts and post_meta %d", th.Index.Len(), len(rows))
		}
		// The forum index's post total sums this column, and counts the
		// posts a reader sees as themselves (TKT-01M3B6A68B).
		var visible int64
		for _, r := range rows {
			e, err := th.Index.Entry(uint32(r.ordinal))
			if err != nil {
				return err
			}
			want := row{ordinal: r.ordinal, author: int64(e.Author), created: e.CreatedMS,
				revision: int64(e.Revision), offset: int64(e.Offset),
				replyTo: sql.NullInt64{Int64: int64(e.ReplyTo), Valid: e.ReplyTo != 0},
				deleted: e.Flags&(threadlog.EntryDeleted|threadlog.EntryHiddenByMod|threadlog.EntryRedacted) != 0}
			if e.Flags.Has(threadlog.EntryEdited) {
				// The current revision's time, from its frame header: the
				// reader will not read a redacted post's frame at all.
				b := make([]byte, threadlog.FrameFixedSize)
				lf, err := os.Open(id.LogPath(f.root))
				if err != nil {
					return err
				}
				_, err = lf.ReadAt(b, int64(e.Offset))
				lf.Close()
				if err != nil {
					return err
				}
				h, _, err := threadlog.ParseFrameHeader(b)
				if err != nil {
					return err
				}
				want.editedAt = sql.NullInt64{Int64: h.TimestampMS, Valid: true}
			}
			if r != want {
				f.t.Errorf("ordinal %d: post_meta %+v, index %+v", r.ordinal, r, want)
			}
			if e.Flags.Visible() {
				visible++
			}
		}
		if live != visible {
			f.t.Errorf("threads.live_post_count is %d and the index shows %d posts", live, visible)
		}
		return nil
	})
	if err != nil {
		f.t.Fatal(err)
	}
}

// TestNewRequiresOptions: a Writer without a database, a root or a sweep is
// refused at construction rather than failing on its first event.
func TestNewRequiresOptions(t *testing.T) {
	f := newFixture(t)
	sweep := func(context.Context) error { return nil }
	for name, o := range map[string]Options{
		"db":    {Root: f.root, Sweep: sweep},
		"root":  {DB: f.db, Sweep: sweep},
		"sweep": {DB: f.db, Root: f.root},
	} {
		if w, err := New(o); err == nil || w != nil {
			t.Errorf("without %s: got %v, %v; want an error", name, w, err)
		}
	}
}
