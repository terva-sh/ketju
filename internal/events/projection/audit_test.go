package projection

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"github.com/terva-sh/ketju/internal/events"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadcache"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// The members the audit tests act as. The fixture's threads are started by
// member 3; 5 moderates; 9 is named by the log but has no users row, as in a
// database restored from a backup older than the logs.
const (
	author    = 3
	moderator = 5
	missing   = 9
)

func (f *fixture) users(ids ...int64) {
	f.t.Helper()
	for _, id := range ids {
		f.exec(`INSERT INTO users (id, handle, display_name, status, role, kind, prefs_json, created_at, updated_at)
			VALUES (?, ?, ?, 'active', 'member', 'human', '{}', 1, 1)`, id, fmt.Sprintf("user%d", id), fmt.Sprintf("User %d", id))
	}
}

// auditRow is one audit_log row as the tests compare it.
type auditRow struct {
	At      int64
	Actor   sql.NullInt64
	Action  string
	Type    string
	ID      string
	Details map[string]any
	IP      string
}

func (f *fixture) auditRows() []auditRow {
	f.t.Helper()
	var out []auditRow
	err := f.db.Read(f.t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		rows, err := tx.QueryContext(f.t.Context(), `SELECT at, actor_id, action, target_type, target_id, details_json, ip
			FROM audit_log ORDER BY id`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var r auditRow
			var dj string
			if err := rows.Scan(&r.At, &r.Actor, &r.Action, &r.Type, &r.ID, &dj, &r.IP); err != nil {
				return err
			}
			if err := json.Unmarshal([]byte(dj), &r.Details); err != nil {
				return err
			}
			out = append(out, r)
		}
		return rows.Err()
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return out
}

// at appends a at a fixed time and returns the offset it was written at and
// the log length after it.
func (f *fixture) at(id threadid.ID, ms int64, a threadlog.Append) (offset, end int64) {
	f.t.Helper()
	offset = f.logSize(id)
	a.TimeMS = ms
	return offset, f.append(id, a)
}

func (f *fixture) logSize(id threadid.ID) int64 {
	f.t.Helper()
	var n int64
	if err := f.cache.With(id, func(th *threadcache.Thread) error {
		th.Actor.Stable(func(l int64) { n = l })
		return nil
	}); err != nil {
		f.t.Fatal(err)
	}
	return n
}

func actor(id int64) sql.NullInt64 { return sql.NullInt64{Int64: id, Valid: id != 0} }

// TestAuditRowsComeFromTheLog is AC7. The projection writer writes one row
// per moderation record, in the transaction projecting it, and none for an
// author's own action; a reindex over the same database adds no second row;
// and an audit insert that fails rolls the record's projection back, holds
// the thread, and the retry writes the row.
func TestAuditRowsComeFromTheLog(t *testing.T) {
	f := newFixture(t)
	f.users(author, moderator)
	id, _ := f.thread("Audited")
	f.post(id, author, 0)
	th := id.String()

	type want struct {
		offset int64
		row    auditRow
	}
	var wants []want
	expect := func(offset int64, r auditRow) {
		r.Details["thread"] = th
		r.Details["log_offset"] = float64(offset)
		wants = append(wants, want{offset, r})
	}

	o, _ := f.at(id, 1_700_000_200_000, threadlog.Append{Type: threadlog.TypeThreadLocked, Author: moderator,
		Body: &threadlog.ThreadLockedBody{Reason: "cooling off"}})
	expect(o, auditRow{At: 1_700_000_200_000, Actor: actor(moderator), Action: ActionThreadLocked, Type: "thread", ID: th,
		Details: map[string]any{"reason": "cooling off"}})

	o, _ = f.at(id, 1_700_000_201_000, threadlog.Append{Type: threadlog.TypeThreadPinned, Author: moderator,
		Body: &threadlog.ThreadPinnedBody{Scope: "global", Until: 1_800_000_000_000}})
	expect(o, auditRow{At: 1_700_000_201_000, Actor: actor(moderator), Action: ActionThreadPinned, Type: "thread", ID: th,
		Details: map[string]any{"scope": "global", "until": float64(1_800_000_000_000)}})

	o, _ = f.at(id, 1_700_000_202_000, threadlog.Append{Type: threadlog.TypeThreadRenamed, Author: moderator,
		Body: &threadlog.ThreadRenamedBody{Title: "Renamed", Slug: "renamed"}})
	expect(o, auditRow{At: 1_700_000_202_000, Actor: actor(moderator), Action: ActionThreadRenamed, Type: "thread", ID: th,
		Details: map[string]any{"title": "Renamed", "slug": "renamed"}})

	// The author's own rename, edit, delete and restore write nothing.
	f.at(id, 1_700_000_203_000, threadlog.Append{Type: threadlog.TypeThreadRenamed, Author: author,
		Body: &threadlog.ThreadRenamedBody{Title: "Mine", Slug: "mine"}})
	f.at(id, 1_700_000_204_000, threadlog.Append{Type: threadlog.TypeEdit, Author: author, Ordinal: 1,
		Body: &threadlog.EditBody{Source: "mine"}})
	f.at(id, 1_700_000_205_000, threadlog.Append{Type: threadlog.TypeDelete, Author: author, Ordinal: 1,
		Body: &threadlog.DeleteBody{}})
	f.at(id, 1_700_000_206_000, threadlog.Append{Type: threadlog.TypeRestore, Author: author, Ordinal: 1,
		Body: &threadlog.RestoreBody{}})

	o, _ = f.at(id, 1_700_000_207_000, threadlog.Append{Type: threadlog.TypeDelete, Author: moderator, Ordinal: 1,
		Body: &threadlog.DeleteBody{Reason: "spam"}})
	expect(o, auditRow{At: 1_700_000_207_000, Actor: actor(moderator), Action: ActionPostDeleted, Type: "post", ID: th + "/1",
		Details: map[string]any{"reason": "spam"}})

	// A moderator's restore, edit and hide of the author's post, as the
	// service writes them (TKT-01M29CRJ5J): a Delete with by_moderator is
	// post.hidden.
	o, _ = f.at(id, 1_700_000_207_100, threadlog.Append{Type: threadlog.TypeRestore, Author: moderator, Ordinal: 1,
		Body: &threadlog.RestoreBody{Reason: "not spam"}})
	expect(o, auditRow{At: 1_700_000_207_100, Actor: actor(moderator), Action: ActionPostRestored, Type: "post", ID: th + "/1",
		Details: map[string]any{"reason": "not spam"}})
	o, _ = f.at(id, 1_700_000_207_200, threadlog.Append{Type: threadlog.TypeEdit, Author: moderator, Ordinal: 1,
		Body: &threadlog.EditBody{Source: "[removed a phone number]", Reason: "privacy"}})
	expect(o, auditRow{At: 1_700_000_207_200, Actor: actor(moderator), Action: ActionPostEdited, Type: "post", ID: th + "/1",
		Details: map[string]any{"reason": "privacy", "revision": float64(3)}})
	o, _ = f.at(id, 1_700_000_207_300, threadlog.Append{Type: threadlog.TypeDelete, Author: moderator, Ordinal: 1,
		Body: &threadlog.DeleteBody{Reason: "off topic", ByModerator: true}})
	expect(o, auditRow{At: 1_700_000_207_300, Actor: actor(moderator), Action: ActionPostHidden, Type: "post", ID: th + "/1",
		Details: map[string]any{"reason": "off topic"}})

	// An actor with no users row is NULL, its id kept in the details.
	o, _ = f.at(id, 1_700_000_208_000, threadlog.Append{Type: threadlog.TypeThreadUnlocked, Author: missing,
		Body: &threadlog.ThreadUnlockedBody{}})
	expect(o, auditRow{At: 1_700_000_208_000, Action: ActionThreadUnlocked, Type: "thread", ID: th,
		Details: map[string]any{"reason": "", "log_actor": float64(missing)}})

	// Actor 0 is no account: NULL, and the thread log named as what did it.
	o, end := f.at(id, 1_700_000_209_000, threadlog.Append{Type: threadlog.TypeThreadUnpinned, Author: 0,
		Body: &threadlog.ThreadUnpinnedBody{Scope: "global"}})
	expect(o, auditRow{At: 1_700_000_209_000, Action: ActionThreadUnpinned, Type: "thread", ID: th,
		Details: map[string]any{"scope": "global", "until": nil, "via": viaLog}})

	f.project(id, end)
	var rows []auditRow
	for _, w := range wants {
		rows = append(rows, w.row)
	}
	if got := f.auditRows(); !reflect.DeepEqual(got, rows) {
		t.Fatalf("audit rows after projection:\n got %+v\nwant %+v", got, rows)
	}

	// Reindex replays every record into emptied projection tables, and
	// audit_log is not one of them.
	for range 2 {
		if _, err := f.w.Reindex(t.Context(), nil); err != nil {
			t.Fatal(err)
		}
		if got := f.auditRows(); !reflect.DeepEqual(got, rows) {
			t.Fatalf("audit rows after reindex:\n got %+v\nwant %+v", got, rows)
		}
	}

	// A failing audit insert rolls the whole range back.
	id2, end2 := f.thread("Retried")
	f.project(id2, end2)
	f.post(id2, author, 0)
	_, end2 = f.at(id2, 1_700_000_300_000, threadlog.Append{Type: threadlog.TypeThreadLocked, Author: moderator,
		Body: &threadlog.ThreadLockedBody{Reason: "retry"}})
	f.exec(`CREATE TRIGGER inject_audit_failure BEFORE INSERT ON audit_log BEGIN SELECT RAISE(ABORT, 'injected'); END`)
	if err := f.w.Deliver(t.Context(), events.Batch{Events: []events.Event{{Thread: id2, LogLength: end2}}}); err != nil {
		t.Fatal(err)
	}
	if l := f.w.Lag(); l.Threads != 1 {
		t.Fatalf("lag %+v after the audit insert failed: want the thread held", l)
	}
	st, locked := f.projectedLock(id2)
	if st >= end2 || locked {
		t.Fatalf("after the failed audit insert projection_state is %d (log %d) and locked is %v: want the range rolled back",
			st, end2, locked)
	}
	if got := len(f.auditRows()); got != len(rows) {
		t.Fatalf("%d audit rows after the failure, want %d", got, len(rows))
	}
	f.exec(`DROP TRIGGER inject_audit_failure`)
	if err := f.w.Retry(t.Context()); err != nil {
		t.Fatal(err)
	}
	if st, locked := f.projectedLock(id2); st != end2 || !locked {
		t.Fatalf("after the retry projection_state is %d (log %d) and locked is %v", st, end2, locked)
	}
	got := f.auditRows()
	if len(got) != len(rows)+1 || got[len(got)-1].Action != ActionThreadLocked || got[len(got)-1].ID != id2.String() {
		t.Fatalf("after the retry the audit rows are %+v", got)
	}
}

// TestModeratorHideIsDeletedInPostMeta: a moderator's Delete marks the post
// deleted in post_meta, as an author's does, so the forum and profile counts
// leave it out, and a reindex rebuilds it so; a Restore clears it
// (TKT-01M29CRJ5J).
func TestModeratorHideIsDeletedInPostMeta(t *testing.T) {
	f := newFixture(t)
	f.users(author, moderator)
	id, _ := f.thread("Hidden")
	f.post(id, author, 0)
	f.post(id, author, 0)
	end := f.append(id, threadlog.Append{Type: threadlog.TypeDelete, Author: moderator, Ordinal: 1,
		Body: &threadlog.DeleteBody{ByModerator: true}})
	f.project(id, end)
	deleted := func(when string, want ...int64) {
		t.Helper()
		var got []int64
		if err := f.db.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
			rows, err := tx.QueryContext(t.Context(), `SELECT deleted FROM post_meta WHERE thread_id = ? ORDER BY ordinal`, int64(id))
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var d int64
				if err := rows.Scan(&d); err != nil {
					return err
				}
				got = append(got, d)
			}
			return rows.Err()
		}); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s: post_meta.deleted %v, want %v", when, got, want)
		}
	}
	deleted("projected", 1, 0)
	if _, err := f.w.Reindex(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	deleted("reindexed", 1, 0)
	end = f.append(id, threadlog.Append{Type: threadlog.TypeRestore, Author: moderator, Ordinal: 1,
		Body: &threadlog.RestoreBody{}})
	f.project(id, end)
	deleted("restored", 0, 0)
	var actions []string
	for _, r := range f.auditRows() {
		actions = append(actions, r.Action)
	}
	if want := []string{ActionPostHidden, ActionPostRestored}; !reflect.DeepEqual(actions, want) {
		t.Fatalf("audit actions %v, want %v", actions, want)
	}
}

// projectedLock is thread id's projection_state length and its locked column.
func (f *fixture) projectedLock(id threadid.ID) (int64, bool) {
	f.t.Helper()
	var (
		st     int64
		locked bool
	)
	err := f.db.Read(f.t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
		s, err := q.GetProjectionState(f.t.Context(), int64(id))
		if err == nil {
			st = s.LogLength
		} else if err != sql.ErrNoRows {
			return err
		}
		t, err := q.GetThread(f.t.Context(), int64(id))
		if err == nil {
			locked = t.Locked != 0
		} else if err != sql.ErrNoRows {
			return err
		}
		return nil
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return st, locked
}

// TestLockAndPinSurviveReindex is AC1: after a reindex, locked, pinned_scope
// and pinned_until are what the last lock and pin records say. An unpin of a
// scope the thread is not pinned in changes nothing.
func TestLockAndPinSurviveReindex(t *testing.T) {
	f := newFixture(t)
	f.users(author, moderator)
	id, _ := f.thread("Held")
	f.post(id, author, 0)
	for _, a := range []threadlog.Append{
		{Type: threadlog.TypeThreadLocked, Body: &threadlog.ThreadLockedBody{Reason: "one"}},
		{Type: threadlog.TypeThreadUnlocked, Body: &threadlog.ThreadUnlockedBody{}},
		{Type: threadlog.TypeThreadLocked, Body: &threadlog.ThreadLockedBody{Reason: "two"}},
		{Type: threadlog.TypeThreadPinned, Body: &threadlog.ThreadPinnedBody{Scope: "global", Until: 1_800_000_000_000}},
		{Type: threadlog.TypeThreadUnpinned, Body: &threadlog.ThreadUnpinnedBody{Scope: "forum"}},
	} {
		a.Author = moderator
		f.append(id, a)
	}
	check := func(when string) {
		t.Helper()
		var row dbq.Thread
		if err := f.db.Read(t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
			var err error
			row, err = q.GetThread(t.Context(), int64(id))
			return err
		}); err != nil {
			t.Fatalf("%s: %v", when, err)
		}
		if row.Locked != 1 || row.PinnedScope != (sql.NullString{String: "global", Valid: true}) ||
			row.PinnedUntil != (sql.NullInt64{Int64: 1_800_000_000_000, Valid: true}) {
			t.Fatalf("%s: locked %d, pinned %v until %v; want locked, global until 1800000000000",
				when, row.Locked, row.PinnedScope, row.PinnedUntil)
		}
	}
	f.project(id, f.logSize(id))
	check("projected")
	rows := len(f.auditRows())
	if rows != 5 {
		t.Fatalf("%d audit rows for five moderation records", rows)
	}
	f.exec(`UPDATE threads SET locked = 0, pinned_scope = NULL, pinned_until = NULL`)
	if _, err := f.w.Reindex(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	check("reindexed")
	if got := len(f.auditRows()); got != rows {
		t.Fatalf("reindex left %d audit rows, want %d", got, rows)
	}
}
