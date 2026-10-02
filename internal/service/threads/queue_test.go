package threads

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/audit"
	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/service/respcache"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadcache"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// Tests for the moderator queue (TKT-01M29CRJ4E). A queue world is a flag
// world with more people and places: an admin, a second moderator by role
// (mod2), and fmod, a member granted moderate on the flag world's forum
// through a group. Besides that forum there is a second public forum
// ("other") and a restricted forum ("staff") that only the admin can see,
// each with a thread of one post by the author.

type queueWorld struct {
	*flagWorld
	other, staff     int64
	otherID, staffID threadid.ID
	roles            map[string]string
}

func newQueueWorld(t *testing.T) *queueWorld {
	t.Helper()
	w := &queueWorld{flagWorld: newFlagWorld(t), roles: map[string]string{"mod": domain.RoleModerator}}
	for _, u := range []struct{ handle, role string }{
		{"admin", domain.RoleAdmin}, {"mod2", domain.RoleModerator}, {"fmod", domain.RoleMember},
	} {
		w.write(func(q *dbq.Queries) error {
			id, err := q.CreateUser(t.Context(), dbq.CreateUserParams{Handle: u.handle, DisplayName: u.handle,
				Status: "active", Role: u.role, Kind: "human", CreatedAt: 1, UpdatedAt: 1})
			w.users[u.handle] = id
			return err
		})
		w.roles[u.handle] = u.role
	}
	for _, f := range []struct {
		slug, visibility string
		to               *int64
	}{{"other", domain.VisibilityPublic, &w.other}, {"staff", domain.VisibilityRestricted, &w.staff}} {
		w.write(func(q *dbq.Queries) error {
			var err error
			*f.to, err = q.CreateForum(t.Context(), dbq.CreateForumParams{Slug: f.slug, Name: f.slug,
				Visibility: f.visibility, CreatedAt: 1, UpdatedAt: 1})
			return err
		})
	}
	var group int64
	w.write(func(q *dbq.Queries) error {
		var err error
		group, err = q.CreateGroup(t.Context(), dbq.CreateGroupParams{Name: "Forum mods", Slug: "forum-mods", CreatedAt: 1, UpdatedAt: 1})
		return err
	})
	if err := w.svc.Perm.AddMember(t.Context(), audit.By{}, group, w.users["fmod"]); err != nil {
		t.Fatal(err)
	}
	if err := w.svc.Perm.Grant(t.Context(), audit.By{}, w.forum, group, domain.PermModerate); err != nil {
		t.Fatal(err)
	}
	w.otherID = w.thread(w.other, 2)
	w.staffID = w.thread(w.staff, 3)
	return w
}

func (w *queueWorld) as(handle string) domain.Viewer {
	role := w.roles[handle]
	if role == "" {
		role = domain.RoleMember
	}
	return domain.Viewer{UserID: w.users[handle], Role: role, Active: true, SessionKey: "s-" + handle}
}

// thread makes thread n in forum, with one post by the author.
func (w *queueWorld) thread(forum int64, n uint64) threadid.ID {
	w.t.Helper()
	id := threadid.ID(uint64(1_757_500_000_000)<<16 | n)
	th, err := w.cache.Create(id, uint64(forum), w.clock)
	if err != nil {
		w.t.Fatal(err)
	}
	w.cache.Release(th)
	w.appendTo(id, threadlog.Append{Type: threadlog.TypeThreadCreated, Author: uint64(w.users["author"]),
		Body: &threadlog.ThreadCreatedBody{Title: fmt.Sprintf("Thread %d", n), Slug: fmt.Sprintf("thread-%d", n), ForumID: uint64(forum)}})
	w.appendTo(id, threadlog.Append{Type: threadlog.TypePost, Author: uint64(w.users["author"]),
		Body: &threadlog.PostBody{Source: "a post elsewhere"}})
	return id
}

func (w *queueWorld) appendTo(id threadid.ID, a threadlog.Append) {
	w.t.Helper()
	w.clock += 1000
	a.TimeMS = w.clock
	var res threadlog.Result
	if err := w.cache.With(id, func(th *threadcache.Thread) error {
		var err error
		res, err = th.Actor.Append(a)
		return err
	}); err != nil {
		w.t.Fatal(err)
	}
	if _, err := w.proj.ProjectThread(w.t.Context(), id, res.LogLength); err != nil {
		w.t.Fatal(err)
	}
}

// insertFlag writes a flag straight to the table, bypassing Flag and its
// lock, as a reporter who cannot see the forum could not.
func (w *queueWorld) insertFlag(id threadid.ID, ordinal uint32, reporter string) int64 {
	w.t.Helper()
	w.clock += 1000
	var fid int64
	w.write(func(q *dbq.Queries) error {
		var err error
		fid, err = q.CreateFlag(w.t.Context(), dbq.CreateFlagParams{ThreadID: int64(id), Ordinal: int64(ordinal),
			ReporterID: w.users[reporter], Reason: FlagSpam, CreatedAt: w.clock})
		return err
	})
	return fid
}

// application writes a pending application submitted now.
func (w *queueWorld) application() int64 {
	w.t.Helper()
	w.clock += 1000
	var id int64
	if err := w.db.Write(w.t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		return tx.QueryRowContext(w.t.Context(), `INSERT INTO applications (form_id, form_version, answers_json, created_at)
			VALUES (1, 1, '{}', ?) RETURNING id`, w.clock).Scan(&id)
	}); err != nil {
		w.t.Fatal(err)
	}
	return id
}

func (w *queueWorld) exec(stmt string) {
	w.t.Helper()
	if err := w.db.Write(w.t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		_, err := tx.ExecContext(w.t.Context(), stmt)
		return err
	}); err != nil {
		w.t.Fatal(err)
	}
}

func (w *queueWorld) resolve(who string, id threadid.ID, ordinal uint32, action string) (dto.QueueResolution, error) {
	w.clock += 1000
	return w.svc.ResolvePost(w.t.Context(), w.as(who), id, ordinal, Resolution{Action: action, Note: "looked at it", IP: "192.0.2.1"})
}

func (w *queueWorld) mustResolve(who string, id threadid.ID, ordinal uint32, action, post string) dto.QueueResolution {
	w.t.Helper()
	res, err := w.resolve(who, id, ordinal, action)
	if err != nil {
		w.t.Fatalf("%s's %s of post %d of %s: %v", who, action, ordinal, id, err)
	}
	if res.Action != action || res.Post != post || len(res.Flags) == 0 {
		w.t.Fatalf("%s's %s of post %d of %s answered %+v: want post %q and the flags it closed", who, action, ordinal, id, res, post)
	}
	return res
}

// items is every item of who's queue, page by page, as "flag <thread>/<n>
// (<flags>)" or "application <id>", and fails on an item repeated.
func (w *queueWorld) items(who string) []string {
	w.t.Helper()
	var out []string
	seen := map[string]bool{}
	cursor := ""
	for range 100 {
		page, err := w.svc.Queue(w.t.Context(), w.as(who), cursor)
		if err != nil {
			w.t.Fatalf("%s's queue: %v", who, err)
		}
		for _, it := range page.Items {
			var s string
			switch it.Kind {
			case dto.QueueKindFlag:
				s = fmt.Sprintf("flag %s/%d (%d)", w.name(it.Flag.Thread.ID), it.Flag.Ordinal, len(it.Flag.Flags))
			case dto.QueueKindApplication:
				s = fmt.Sprintf("application %d", it.Application.ID)
			}
			if seen[s] {
				w.t.Fatalf("%s's queue repeats %s", who, s)
			}
			seen[s] = true
			out = append(out, s)
		}
		if page.NextCursor == "" {
			return out
		}
		cursor = page.NextCursor
	}
	w.t.Fatal("the queue never ended")
	return nil
}

// name is a thread's name in item strings.
func (w *queueWorld) name(id string) string {
	switch id {
	case w.id.String():
		return "general"
	case w.otherID.String():
		return "other"
	case w.staffID.String():
		return "staff"
	}
	return id
}

func (w *queueWorld) count(who string) *int {
	w.t.Helper()
	n, err := w.svc.QueueCount(w.t.Context(), w.as(who))
	if err != nil {
		w.t.Fatal(err)
	}
	return n
}

// flagRow is a flag.* audit row.
type flagRow struct {
	action, actor string
	at            int64
	details       map[string]any
}

func (w *queueWorld) flagRows() []flagRow {
	w.t.Helper()
	var out []flagRow
	if err := w.db.Read(w.t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		rows, err := tx.QueryContext(w.t.Context(), `SELECT action, coalesce(actor_id, 0), at, details_json FROM audit_log
			WHERE action LIKE 'flag.%' ORDER BY id`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var r flagRow
			var actor int64
			var details string
			if err := rows.Scan(&r.action, &actor, &r.at, &details); err != nil {
				return err
			}
			for h, id := range w.users {
				if id == actor {
					r.actor = h
				}
			}
			if err := json.Unmarshal([]byte(details), &r.details); err != nil {
				return err
			}
			out = append(out, r)
		}
		return rows.Err()
	}); err != nil {
		w.t.Fatal(err)
	}
	return out
}

// flagState is each of a post's flags as id -> "status pending audit_id".
func (w *queueWorld) flagState(id threadid.ID, ordinal uint32) map[int64]string {
	w.t.Helper()
	out := map[int64]string{}
	if err := w.db.Read(w.t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		rows, err := tx.QueryContext(w.t.Context(), `SELECT id, status, coalesce(audit_pending, '-'), audit_id IS NOT NULL
			FROM flags WHERE thread_id = ? AND ordinal = ?`, int64(id), int64(ordinal))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var fid int64
			var status, pending string
			var linked bool
			if err := rows.Scan(&fid, &status, &pending, &linked); err != nil {
				return err
			}
			out[fid] = fmt.Sprintf("%s pending=%s linked=%t", status, pending, linked)
		}
		return rows.Err()
	}); err != nil {
		w.t.Fatal(err)
	}
	return out
}

// deletes counts the Delete records appended at or after offset from.
func (w *queueWorld) deletes(from int64) int {
	n := 0
	for _, f := range w.frames(from) {
		if f.h.Type == threadlog.TypeDelete {
			n++
		}
	}
	return n
}

// TestQueueOneListNewestFirst is AC1's order: flags and applications
// interleave by time, newest first, and paging repeats nothing and misses
// nothing.
func TestQueueOneListNewestFirst(t *testing.T) {
	w := newQueueWorld(t)
	w.svc.QueuePage = 2
	app1 := w.application()
	w.mustFlag("rep1", 1)
	second := w.post("a second post")
	w.mustFlag("rep2", second)
	app2 := w.application()
	w.insertFlag(w.otherID, 1, "rep3")
	app3 := w.application()
	want := []string{
		fmt.Sprintf("application %d", app3),
		"flag other/1 (1)",
		fmt.Sprintf("application %d", app2),
		fmt.Sprintf("flag general/%d (1)", second),
		"flag general/1 (1)",
		fmt.Sprintf("application %d", app1),
	}
	if got := w.items("mod"); !reflect.DeepEqual(got, want) {
		t.Errorf("the queue:\n  %s\nwant\n  %s", strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}
	// A later flag on an item moves it to the top: its time is its newest
	// open flag.
	w.mustFlag("rep4", 1)
	if got := w.items("mod"); got[0] != "flag general/1 (2)" {
		t.Errorf("after a new flag on post 1 the queue opens with %q", got[0])
	}
	if _, err := w.svc.Queue(t.Context(), w.as("mod"), "not a cursor"); codeOf(err) != problem.CodeBadRequest {
		t.Errorf("a malformed cursor: %v, want bad-request", err)
	}
}

// TestQueueGroupsFlagsByPost is AC1's grouping: three reports of one post
// are one item holding all three, newest first, with their reporters and
// reasons, and the post as a moderator reads it: hidden pending review, with
// its words.
func TestQueueGroupsFlagsByPost(t *testing.T) {
	w := newQueueWorld(t)
	w.mustFlag("rep1", 1)
	w.mustFlag("rep2", 1)
	w.clock += 1000
	if _, err := w.svc.Flag(t.Context(), w.as("rep3"), w.id, FlagRequest{Ordinal: 1, Reason: FlagOther, Note: "our address"}); err != nil {
		t.Fatal(err)
	}
	page, err := w.svc.Queue(t.Context(), w.as("mod"), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].Flag == nil {
		t.Fatalf("the queue: %+v, want one flag item", page.Items)
	}
	it := page.Items[0]
	f := it.Flag
	var reps []string
	for _, r := range f.Flags {
		reps = append(reps, r.Reporter.Handle+" "+r.Reason+" "+r.Note)
	}
	if want := []string{"rep3 other our address", "rep2 spam ", "rep1 spam "}; !reflect.DeepEqual(reps, want) {
		t.Errorf("the item's flags %q, want %q", reps, want)
	}
	if int64(it.CreatedAt) != int64(f.Flags[0].CreatedAt) {
		t.Errorf("the item's time %d is not its newest flag's %d", it.CreatedAt, f.Flags[0].CreatedAt)
	}
	if f.Thread.ID != w.id.String() || f.Thread.Slug != "flagged" || f.Thread.Title != "Flagged" || f.Ordinal != 1 {
		t.Errorf("the item names %+v post %d", f.Thread, f.Ordinal)
	}
	if f.Post == nil || f.Post.Removal == nil || *f.Post.Removal != dto.RemovalReview || f.Post.HTML == "" {
		t.Errorf("the item's post %+v: want it hidden pending review, with its words", f.Post)
	}
}

// TestQueueScopesFlagsToModeratedForums is AC1's scope: a member granted
// moderate on one forum through a group sees that forum's flags and no
// applications; a moderator by role sees every forum they can read, and the
// applications, but no flags in a restricted forum they cannot read; an
// admin sees everything; a plain member has no queue.
func TestQueueScopesFlagsToModeratedForums(t *testing.T) {
	w := newQueueWorld(t)
	app := w.application()
	w.mustFlag("rep1", 1)
	w.insertFlag(w.otherID, 1, "rep2")
	w.insertFlag(w.staffID, 1, "rep3")
	for _, c := range []struct {
		who  string
		want []string
	}{
		{"fmod", []string{"flag general/1 (1)"}},
		{"mod", []string{"flag other/1 (1)", "flag general/1 (1)", fmt.Sprintf("application %d", app)}},
		{"admin", []string{"flag staff/1 (1)", "flag other/1 (1)", "flag general/1 (1)", fmt.Sprintf("application %d", app)}},
	} {
		if got := w.items(c.who); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s's queue %q, want %q", c.who, got, c.want)
		}
	}
	for _, who := range []string{"rep1", "author"} {
		if _, err := w.svc.Queue(t.Context(), w.as(who), ""); codeOf(err) != problem.CodeForbidden {
			t.Errorf("%s's queue: %v, want forbidden", who, err)
		}
	}
	if _, err := w.svc.Queue(t.Context(), domain.Anonymous(), ""); codeOf(err) != problem.CodeUnauthenticated {
		t.Errorf("an anonymous queue: %v, want unauthenticated", err)
	}
	// A forum-scoped moderator may not act outside their forum, and nobody
	// may act where they cannot see.
	if _, err := w.resolve("fmod", w.otherID, 1, QueueDismiss); codeOf(err) != problem.CodeForbidden {
		t.Errorf("fmod's dismissal in another forum: %v, want forbidden", err)
	}
	if _, err := w.resolve("mod", w.staffID, 1, QueueDismiss); codeOf(err) != problem.CodeNotFound {
		t.Errorf("mod's dismissal in the hidden forum: %v, want not-found", err)
	}
}

// TestQueueCountMatchesList is AC3's server half: /me's queue is the number
// of items the queue lists, across every page, for each kind of viewer, and
// null for a viewer with no queue.
func TestQueueCountMatchesList(t *testing.T) {
	w := newQueueWorld(t)
	w.svc.QueuePage = 2
	w.application()
	w.application()
	w.mustFlag("rep1", 1)
	w.mustFlag("rep2", 1)
	w.mustFlag("rep1", w.post("two"))
	w.insertFlag(w.otherID, 1, "rep2")
	w.insertFlag(w.staffID, 1, "rep3")
	for _, who := range []string{"fmod", "mod", "admin"} {
		n := w.count(who)
		if items := w.items(who); n == nil || *n != len(items) {
			t.Errorf("%s: /me queue %v, and the queue lists %d", who, n, len(items))
		}
	}
	for _, who := range []string{"rep1", "author"} {
		if n := w.count(who); n != nil {
			t.Errorf("%s has queue %d, want null", who, *n)
		}
	}
	if n, err := w.svc.QueueCount(t.Context(), domain.Anonymous()); n != nil || err != nil {
		t.Errorf("anonymous: %v, %v", n, err)
	}
	// Archived threads are left out of both: their flags are read-only.
	w.exec(fmt.Sprintf(`UPDATE threads SET archived = 1 WHERE id = %d`, int64(w.otherID)))
	if n, items := w.count("mod"), w.items("mod"); *n != len(items) || strings.Contains(strings.Join(items, " "), "other") {
		t.Errorf("with the other thread archived: count %d, items %q", *n, items)
	}
}

// TestForumScopedMemberGetsQueueCount is AC12's service half: a member
// granted moderate on one forum through a group gets that forum's item
// count, and a member with no grant gets null.
func TestForumScopedMemberGetsQueueCount(t *testing.T) {
	w := newQueueWorld(t)
	if n := w.count("fmod"); n == nil || *n != 0 {
		t.Fatalf("fmod with nothing flagged: %v, want 0", n)
	}
	w.mustFlag("rep1", 1)
	w.insertFlag(w.otherID, 1, "rep2")
	w.application()
	if n := w.count("fmod"); n == nil || *n != 1 {
		t.Errorf("fmod: %v, want 1: their forum's one item, and no other forum's or application", n)
	}
	if n := w.count("rep1"); n != nil {
		t.Errorf("rep1: %d, want null", *n)
	}
}

// TestForumScopedModeratorUsesQueue is AC6's service half: a member granted
// moderate on one forum through a group lists that forum's items and can
// resolve them.
func TestForumScopedModeratorUsesQueue(t *testing.T) {
	w := newQueueWorld(t)
	w.mustFlag("rep1", 1)
	if got := w.items("fmod"); !reflect.DeepEqual(got, []string{"flag general/1 (1)"}) {
		t.Fatalf("fmod's queue %q", got)
	}
	w.mustResolve("fmod", w.id, 1, QueueResolve, dto.QueuePostUnchanged)
	if got := w.items("fmod"); len(got) != 0 {
		t.Errorf("after fmod's resolve, the queue still lists %q", got)
	}
	if rows := w.flagRows(); len(rows) != 1 || rows[0].actor != "fmod" || rows[0].action != ActionFlagResolved {
		t.Errorf("audit %+v: want one flag.resolved by fmod", rows)
	}
}

// TestResolvedItemLeavesQueueAndStaysQueryable is AC2: after each action the
// item is gone, GetFlag still returns the flag, closed, with its resolver,
// time and note, and one audit row names the actor, the flags and what
// happened, and is linked from the flags.
func TestResolvedItemLeavesQueueAndStaysQueryable(t *testing.T) {
	w := newQueueWorld(t)
	for _, c := range []struct {
		action, status, audit, post string
	}{
		{QueueResolve, "resolved", ActionFlagResolved, dto.QueuePostUnchanged},
		{QueueDismiss, "dismissed", ActionFlagDismissed, dto.QueuePostUnchanged},
		{QueueRemove, "resolved", ActionFlagResolved, dto.QueuePostHidden},
	} {
		t.Run(c.action, func(t *testing.T) {
			ordinal := w.post("post for " + c.action)
			w.mustFlag("rep1", ordinal)
			res := w.mustResolve("mod", w.id, ordinal, c.action, c.post)
			for _, it := range w.items("mod") {
				if it == fmt.Sprintf("flag general/%d (1)", ordinal) {
					t.Errorf("the item is still listed after %s", c.action)
				}
			}
			var f dbq.Flag
			if err := w.db.Read(t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
				var err error
				f, err = q.GetFlag(t.Context(), res.Flags[0])
				return err
			}); err != nil {
				t.Fatal(err)
			}
			if f.Status != c.status || f.ResolvedBy.Int64 != w.users["mod"] || f.ResolvedAt.Int64 != w.clock ||
				f.ResolutionNote != "looked at it" || !f.AuditID.Valid || f.AuditPending.Valid {
				t.Errorf("GetFlag after %s: %+v", c.action, f)
			}
			rows := w.flagRows()
			last := rows[len(rows)-1]
			if last.action != c.audit || last.actor != "mod" || last.details["action"] != c.action ||
				last.details["post"] != c.post || last.details["note"] != "looked at it" {
				t.Errorf("the audit row after %s: %+v", c.action, last)
			}
		})
	}
}

// TestConcurrentResolveOneOutcome is AC7, under -race: two moderators act on
// one post at once, with every pair of actions, on a shown post and on one
// hidden pending review. One succeeds; the other waits on the post's lock,
// finds no open flag, and gets conflict before touching the log. There is
// one flag audit row and at most one Delete.
func TestConcurrentResolveOneOutcome(t *testing.T) {
	actions := []string{QueueResolve, QueueDismiss, QueueRemove}
	for _, hidden := range []bool{false, true} {
		for _, a := range actions {
			for _, b := range actions {
				t.Run(fmt.Sprintf("hidden=%t/%s-%s", hidden, a, b), func(t *testing.T) {
					w := newQueueWorld(t)
					if hidden {
						w.hide(1)
					} else {
						w.mustFlag("rep1", 1)
					}
					before := w.logLength()
					var once sync.Once
					holding, release := make(chan struct{}), make(chan struct{})
					w.svc.afterQueueRead = func(threadid.ID, uint32) {
						once.Do(func() { close(holding); <-release })
					}
					errs := make([]error, 2)
					var wg sync.WaitGroup
					wg.Add(2)
					go func() {
						defer wg.Done()
						_, errs[0] = w.svc.ResolvePost(t.Context(), w.as("mod"), w.id, 1, Resolution{Action: a})
					}()
					<-holding
					go func() {
						defer wg.Done()
						_, errs[1] = w.svc.ResolvePost(t.Context(), w.as("mod2"), w.id, 1, Resolution{Action: b})
					}()
					time.Sleep(20 * time.Millisecond) // let the second reach the lock
					close(release)
					wg.Wait()
					if errs[0] != nil || codeOf(errs[1]) != problem.CodeConflict {
						t.Errorf("answers %v and %v: want success, then conflict", errs[0], errs[1])
					}
					if n := len(w.flagRows()); n != 1 {
						t.Errorf("%d flag audit rows, want 1", n)
					}
					if n := w.deletes(before); n > 1 {
						t.Errorf("%d Deletes appended, want at most 1", n)
					}
				})
			}
		}
	}
}

// TestQueueSharesTheFlagLock is AC7's lock: the queue holds the same per-post
// lock as the auto-hide.
//   - An EnforceAutoHide arriving while a queue dismissal holds the lock at
//     its Restore waits, then finds no open flag and hides nothing.
//   - A queue dismissal arriving while the sweep holds the lock between its
//     count and its append waits; the sweep hides the post, and the
//     dismissal then restores it from that state.
func TestQueueSharesTheFlagLock(t *testing.T) {
	t.Run("the auto-hide waits for the queue", func(t *testing.T) {
		w := newQueueWorld(t)
		w.hide(1)
		holding, release := make(chan struct{}), make(chan struct{})
		w.writer.set(func(a threadlog.Append) {
			if a.Type == threadlog.TypeRestore {
				close(holding)
				<-release
			}
		}, nil)
		done := make(chan error, 1)
		go func() {
			_, err := w.svc.ResolvePost(t.Context(), w.as("mod"), w.id, 1, Resolution{Action: QueueDismiss})
			done <- err
		}()
		<-holding
		hid := make(chan bool, 1)
		go func() {
			ok, _ := w.svc.EnforceAutoHide(t.Context(), w.id, 1)
			hid <- ok
		}()
		select {
		case <-hid:
			t.Fatal("EnforceAutoHide ran while the queue held the post's lock")
		case <-time.After(30 * time.Millisecond):
		}
		close(release)
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		if <-hid {
			t.Error("EnforceAutoHide hid the post the queue restored")
		}
		if e := w.entry(1); !e.Flags.Visible() {
			t.Errorf("the post ends %#x, want shown", e.Flags)
		}
	})
	t.Run("the queue waits for the sweep", func(t *testing.T) {
		w := newQueueWorld(t)
		w.mustFlag("rep1", 1)
		w.mustFlag("rep2", 1)
		w.setThreshold(2)
		holding, release := make(chan struct{}), make(chan struct{})
		var once sync.Once
		w.svc.afterFlagCount = func(threadid.ID, uint32) { once.Do(func() { close(holding); <-release }) }
		swept := make(chan int, 1)
		go func() {
			n, _ := w.svc.SweepAutoHide(t.Context())
			swept <- n
		}()
		<-holding
		done := make(chan dto.QueueResolution, 1)
		go func() {
			res, err := w.svc.ResolvePost(t.Context(), w.as("mod"), w.id, 1, Resolution{Action: QueueDismiss})
			if err != nil {
				t.Error(err)
			}
			done <- res
		}()
		select {
		case <-done:
			t.Fatal("the queue acted while the sweep held the post's lock")
		case <-time.After(30 * time.Millisecond):
		}
		close(release)
		if n := <-swept; n != 1 {
			t.Errorf("the sweep hid %d, want 1", n)
		}
		if res := <-done; res.Post != dto.QueuePostRestored {
			t.Errorf("the dismissal after the sweep answered %+v: want it to restore what the sweep hid", res)
		}
		if e := w.entry(1); !e.Flags.Visible() || w.openFlags(1) != 0 {
			t.Errorf("the post ends %#x with %d open flags, want shown with none", e.Flags, w.openFlags(1))
		}
	})
}

// TestReporterLearnsNothing is AC4: a resolution sends the reporter no
// notification, gives them no queue, and a new report answers the same
// receipt as before.
func TestReporterLearnsNothing(t *testing.T) {
	w := newQueueWorld(t)
	before, err := w.flag("rep1", 1)
	if err != nil {
		t.Fatal(err)
	}
	notes := func() int {
		var n int
		if err := w.db.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
			return tx.QueryRowContext(t.Context(), `SELECT count(*) FROM notifications WHERE user_id = ?`, w.users["rep1"]).Scan(&n)
		}); err != nil {
			t.Fatal(err)
		}
		return n
	}
	n0 := notes()
	w.mustResolve("mod", w.id, 1, QueueDismiss, dto.QueuePostUnchanged)
	if notes() != n0 || w.count("rep1") != nil {
		t.Errorf("after the dismissal rep1 has %d notifications (was %d) and queue %v", notes(), n0, w.count("rep1"))
	}
	after, err := w.flag("rep1", 1)
	if err != nil || after != before {
		t.Errorf("rep1's report after the dismissal answered %+v, %v; before it answered %+v", after, err, before)
	}
}

// TestQueueRemoveMovesTheCacheKey: a thread page a member had cached before
// a remove shows the tombstone after it, because the append moved the log
// length the cache is keyed by (docs/07 "The response cache").
func TestQueueRemoveMovesTheCacheKey(t *testing.T) {
	w := newQueueWorld(t)
	w.svc.Responses = respcache.New(1 << 20)
	w.mustFlag("rep1", 1)
	page := func() dto.Post {
		res, err := w.svc.View(t.Context(), w.as("rep4"), w.id, Query{}, "")
		if err != nil {
			t.Fatal(err)
		}
		return res.Body.Posts[0]
	}
	if p := page(); p.Deleted {
		t.Fatal("the post is down before the remove")
	}
	w.mustResolve("mod", w.id, 1, QueueRemove, dto.QueuePostHidden)
	if p := page(); !p.Deleted || p.HTML != "" {
		t.Errorf("after the remove a member reads %+v: want a tombstone", p)
	}
}

// TestDismissRestoresOnlyAnAutoHide is AC5: dismissing every flag on a post
// hidden pending review appends a Restore and shows it; on a post a
// moderator hid by hand it appends nothing and the post stays hidden.
func TestDismissRestoresOnlyAnAutoHide(t *testing.T) {
	w := newQueueWorld(t)
	w.hide(1)
	before := w.logLength()
	w.mustResolve("mod", w.id, 1, QueueDismiss, dto.QueuePostRestored)
	recs := w.frames(before)
	if len(recs) != 1 || recs[0].h.Type != threadlog.TypeRestore || recs[0].h.Actor != uint64(w.users["mod"]) {
		t.Errorf("the dismissal appended %v: want one Restore by mod", recs)
	}
	if e := w.entry(1); !e.Flags.Visible() {
		t.Errorf("after the dismissal the post is %#x, want shown", e.Flags)
	}

	hand := w.post("hidden by hand")
	w.mustFlag("rep1", hand)
	w.append(threadlog.Append{Type: threadlog.TypeDelete, Author: uint64(w.users["mod"]), Ordinal: hand,
		Body: &threadlog.DeleteBody{ByModerator: true}})
	before = w.logLength()
	w.mustResolve("mod", w.id, hand, QueueDismiss, dto.QueuePostAlreadyHidden)
	if w.logLength() != before || w.entry(hand).Flags.Visible() {
		t.Errorf("dismissing the flags of a hand-hidden post changed the log or showed the post")
	}
	if got := w.flagStatuses(hand); got["rep1"] != "dismissed by mod" {
		t.Errorf("its flag %v, want dismissed", got)
	}
}

// TestKeepAutoHiddenPostRestoresIt is AC11: "Keep post" on a post hidden
// pending review restores it through the dismissal's path, the flags end
// dismissed and the row says resolve; on a post a moderator hid by hand it
// is conflict and changes nothing.
func TestKeepAutoHiddenPostRestoresIt(t *testing.T) {
	w := newQueueWorld(t)
	w.hide(1)
	before := w.logLength()
	w.mustResolve("mod", w.id, 1, QueueResolve, dto.QueuePostRestored)
	if recs := w.frames(before); len(recs) != 1 || recs[0].h.Type != threadlog.TypeRestore || !w.entry(1).Flags.Visible() {
		t.Errorf("keeping an auto-hidden post appended %v and left it %#x", recs, w.entry(1).Flags)
	}
	if len(w.items("mod")) != 0 {
		t.Error("the item is still listed")
	}
	rows := w.flagRows()
	if len(rows) != 1 || rows[0].action != ActionFlagDismissed || rows[0].details["action"] != QueueResolve ||
		rows[0].details["post"] != dto.QueuePostRestored {
		t.Errorf("audit %+v: want one flag.dismissed saying resolve and restored", rows)
	}

	hand := w.post("hidden by hand")
	w.mustFlag("rep1", hand)
	w.append(threadlog.Append{Type: threadlog.TypeDelete, Author: uint64(w.users["mod"]), Ordinal: hand,
		Body: &threadlog.DeleteBody{ByModerator: true}})
	before = w.logLength()
	if _, err := w.resolve("mod", w.id, hand, QueueResolve); codeOf(err) != problem.CodeConflict ||
		!strings.Contains(err.Error(), "restore it from the thread page first") {
		t.Errorf("keeping a hand-hidden post: %v, want conflict", err)
	}
	if w.logLength() != before || w.openFlags(hand) != 1 || len(w.flagRows()) != 1 {
		t.Error("the refused keep changed something")
	}
}

// TestFailedActionStaysInQueue is AC9's failures:
//   - with the remove's Delete made to fail, the flags stay open, no flag row
//     is written, and the item is listed with the post shown;
//   - with the resolve after the Delete made to fail, the item is listed with
//     the post hidden, and a second remove resolves it as already hidden
//     without a second Delete.
func TestFailedActionStaysInQueue(t *testing.T) {
	w := newQueueWorld(t)
	w.mustFlag("rep1", 1)
	w.writer.set(nil, func(a threadlog.Append) error {
		if a.Type == threadlog.TypeDelete {
			return errInjected
		}
		return nil
	})
	if _, err := w.resolve("mod", w.id, 1, QueueRemove); !errors.Is(err, errInjected) {
		t.Fatalf("the remove with its Delete failing: %v", err)
	}
	w.writer.set(nil, nil)
	page, err := w.svc.Queue(t.Context(), w.as("mod"), "")
	if err != nil {
		t.Fatal(err)
	}
	if w.openFlags(1) != 1 || len(w.flagRows()) != 0 || len(page.Items) != 1 || page.Items[0].Flag.Post.Deleted {
		t.Errorf("after the failed Delete: %d open flags, rows %v, items %+v", w.openFlags(1), w.flagRows(), page.Items)
	}

	w.exec(`CREATE TRIGGER fail_resolve BEFORE UPDATE OF status ON flags WHEN NEW.status = 'resolved'
		BEGIN SELECT RAISE(ABORT, 'injected'); END`)
	before := w.logLength()
	if _, err := w.resolve("mod", w.id, 1, QueueRemove); err == nil {
		t.Fatal("the remove with its resolve failing answered success")
	}
	w.exec(`DROP TRIGGER fail_resolve`)
	page, err = w.svc.Queue(t.Context(), w.as("mod"), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || !page.Items[0].Flag.Post.Deleted || *page.Items[0].Flag.Post.Removal != dto.RemovalModerator {
		t.Errorf("after the failed resolve the item is %+v: want it listed, the post hidden by a moderator", page.Items)
	}
	w.mustResolve("mod", w.id, 1, QueueRemove, dto.QueuePostAlreadyHidden)
	if n := w.deletes(before); n != 1 {
		t.Errorf("%d Deletes across both removes, want 1", n)
	}
}

// TestStateChangeIsConflictNotSuccess: a change to the post that lands
// between the queue's read and its write is conflict, with the flags left
// open and no flag row, for every action on a shown post (Terva reviews
// 1522 and 1524 on PR #184). The change is appended straight to the log,
// bypassing the post's flag lock: a suspension's hide of an author's posts,
// the system's auto-hide, and an unsuspension's restore of a post the
// suspension hid. A remove meets a hide as its append's precondition;
// dismiss and resolve, which append nothing, meet it when they read the
// state again before their write, and the restore likewise.
//
// The suspension's hide and restore are the real HidePostsOf and
// RestorePostsOf (TKT-01M3R10VP3), called from afterQueueRead while the
// queue holds the post's stripe. Each returning at all proves it takes no
// stripe: sync.Mutex is not reentrant, so one that did would wait for the
// queue that is waiting for it. Both also reach the author's post in
// "other", so their audit rows come in pairs.
func TestStateChangeIsConflictNotSuccess(t *testing.T) {
	const noStripe = 10 * time.Second // a guard against a deadlock, not a wait
	within := func(t *testing.T, what string, fn func() (int, error)) {
		t.Helper()
		done := make(chan error, 1)
		go func() {
			_, err := fn()
			done <- err
		}()
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("%s: %v", what, err)
			}
		case <-time.After(noStripe):
			t.Fatalf("%s waited on the stripe the queue holds", what)
		}
	}
	changes := []struct {
		name    string
		setup   func(t *testing.T, w *queueWorld)
		change  func(t *testing.T, w *queueWorld, id threadid.ID, ordinal uint32)
		deletes int
		audit   []string
	}{
		{"a suspension's hide", nil, func(t *testing.T, w *queueWorld, _ threadid.ID, _ uint32) {
			within(t, "the suspension's hide", w.suspendHide)
		}, 1, []string{"post.hidden by mod2", "post.hidden by mod2"}},
		{"the auto-hide", nil, func(_ *testing.T, w *queueWorld, id threadid.ID, ordinal uint32) {
			w.appendTo(id, threadlog.Append{Type: threadlog.TypeDelete, Ordinal: ordinal,
				Body: &threadlog.DeleteBody{Reason: AutoHideReason, Auto: true}})
		}, 1, []string{"post.auto_hidden by system"}},
		// The flag was reported on the shown post, then the author was
		// suspended with their posts hidden: the queue lists the post as
		// hidden by a moderator, and the restore lands while it decides.
		// The post ends shown with its flag open, never shown with a
		// flag.dismissed row saying it is hidden.
		{"an unsuspension's restore", func(t *testing.T, w *queueWorld) {
			if n, err := w.suspendHide(); err != nil || n != 2 {
				t.Fatalf("the suspension's hide answered %d, %v", n, err)
			}
		}, func(t *testing.T, w *queueWorld, _ threadid.ID, _ uint32) {
			within(t, "the unsuspension's restore", w.suspendRestore)
		}, 0, []string{"post.hidden by mod2", "post.hidden by mod2", "post.restored by mod2", "post.restored by mod2"}},
	}
	for _, c := range changes {
		for _, action := range []string{QueueRemove, QueueDismiss, QueueResolve} {
			t.Run(c.name+"/"+action, func(t *testing.T) {
				w := newQueueWorld(t)
				w.mustFlag("rep1", 1)
				if c.setup != nil {
					c.setup(t, w)
				}
				before := w.logLength()
				w.svc.afterQueueRead = func(id threadid.ID, ordinal uint32) { c.change(t, w, id, ordinal) }
				if _, err := w.resolve("mod", w.id, 1, action); codeOf(err) != problem.CodeConflict {
					t.Fatalf("the %s after %s: %v, want conflict", action, c.name, err)
				}
				w.svc.afterQueueRead = nil
				if w.openFlags(1) != 1 || len(w.flagRows()) != 0 {
					t.Errorf("after the conflict: %d open flags, rows %v", w.openFlags(1), w.flagRows())
				}
				if n := w.deletes(before); n != c.deletes {
					t.Errorf("%d Deletes, want %d, the change's", n, c.deletes)
				}
				if got := w.audit(); !reflect.DeepEqual(got, c.audit) {
					t.Errorf("audit %v, want %v", got, c.audit)
				}
			})
		}
	}
}

// TestThreadPageDeleteWaitsForTheQueue: the thread page's delete of one post
// holds the post's flag lock, so a delete arriving while a queue action
// holds it waits, and lands after the action, which recorded the post as it
// decided on it (Terva review 1522 on PR #184).
func TestThreadPageDeleteWaitsForTheQueue(t *testing.T) {
	w := newQueueWorld(t)
	w.mustFlag("rep1", 1)
	holding, release := make(chan struct{}), make(chan struct{})
	w.svc.afterQueueRead = func(threadid.ID, uint32) { close(holding); <-release }
	queued := make(chan error, 1)
	go func() {
		res, err := w.svc.ResolvePost(t.Context(), w.as("mod"), w.id, 1, Resolution{Action: QueueDismiss})
		if err == nil && res.Post != dto.QueuePostUnchanged {
			err = fmt.Errorf("the dismissal says the post %q", res.Post)
		}
		queued <- err
	}()
	<-holding
	deleted := make(chan error, 1)
	go func() {
		_, err := w.svc.Delete(t.Context(), w.as("mod2"), w.id, 1, "spam")
		deleted <- err
	}()
	select {
	case err := <-deleted:
		t.Fatalf("the delete did not wait for the queue: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	if err := <-queued; err != nil {
		t.Fatalf("the dismissal: %v", err)
	}
	if err := <-deleted; err != nil {
		t.Fatalf("the delete after it: %v", err)
	}
	w.svc.afterQueueRead = nil
	if f := w.entry(1).Flags; !f.Has(threadlog.EntryHiddenByMod) {
		t.Errorf("the post after the delete: flags %v", f)
	}
	if got, want := w.audit(), []string{"flag.dismissed by mod", "post.hidden by mod2"}; !reflect.DeepEqual(got, want) {
		t.Errorf("audit %v, want %v", got, want)
	}
}

// TestQueueRemoveOfOwnPostIsAModeratorsHide: the queue's remove is a
// moderation decision whoever wrote the post, so a moderator removing their
// own flagged post hides it as a moderator, as the auto-hidden case does
// (Terva review 1522 on PR #184). Only a moderator may restore it.
func TestQueueRemoveOfOwnPostIsAModeratorsHide(t *testing.T) {
	w := newQueueWorld(t)
	w.append(threadlog.Append{Type: threadlog.TypePost, Author: uint64(w.users["mod"]), Body: &threadlog.PostBody{Source: "mine"}})
	own := uint32(w.entries())
	w.mustFlag("rep1", own)
	w.mustResolve("mod", w.id, own, QueueRemove, dto.QueuePostHidden)
	if f := w.entry(own).Flags; !f.Has(threadlog.EntryHiddenByMod) || f.Has(threadlog.EntryDeleted) {
		t.Errorf("the removed post's flags %v, want hidden by a moderator", f)
	}
}

// TestFailedRestoreWritesNoDismissal is AC10: with the Restore append made
// to fail, the flags are reopened with no marker and no flag.dismissed row
// exists; with it succeeding, exactly one row is written, after the
// projection's post.restored row.
func TestFailedRestoreWritesNoDismissal(t *testing.T) {
	w := newQueueWorld(t)
	w.hide(1)
	w.writer.set(nil, func(a threadlog.Append) error {
		if a.Type == threadlog.TypeRestore {
			return errInjected
		}
		return nil
	})
	if _, err := w.resolve("mod", w.id, 1, QueueDismiss); !errors.Is(err, errInjected) {
		t.Fatalf("the dismissal with its Restore failing: %v", err)
	}
	for id, s := range w.flagState(w.id, 1) {
		if s != "open pending=- linked=false" {
			t.Errorf("flag %d after the failed restore: %s", id, s)
		}
	}
	if len(w.flagRows()) != 0 || w.entry(1).Flags.Visible() {
		t.Errorf("the failed restore wrote %v or showed the post", w.flagRows())
	}
	w.writer.set(nil, nil)
	res := w.mustResolve("mod", w.id, 1, QueueDismiss, dto.QueuePostRestored)
	if got, want := w.audit(), []string{"post.auto_hidden by system", "post.restored by mod", "flag.dismissed by mod"}; !reflect.DeepEqual(got, want) {
		t.Errorf("audit %v, want %v", got, want)
	}
	rows := w.flagRows()
	if ids, _ := rows[0].details["flags"].([]any); len(ids) != len(res.Flags) || len(res.Flags) != 3 {
		t.Errorf("the row names flags %v, the answer %v", rows[0].details["flags"], res.Flags)
	}
}

// TestRestoredButUnauditedIsRecovered is AC13: with the flag.dismissed write
// made to fail after a durable Restore, the answer is 200, the flags stay
// dismissed and marked, and post.restored exists. The row is then written
// once, with the original moderator and time and recovered, by a retry or
// by FlagAuditCatchUp. A retried remove writes it and is still conflict.
func TestRestoredButUnauditedIsRecovered(t *testing.T) {
	failRow := func(w *queueWorld) {
		w.exec(`CREATE TRIGGER fail_row BEFORE INSERT ON audit_log WHEN NEW.action = 'flag.dismissed'
			BEGIN SELECT RAISE(ABORT, 'injected'); END`)
	}
	owed := func(w *queueWorld, ordinal uint32, action string) (int64, []int64) {
		w.t.Helper()
		failRow(w)
		res := w.mustResolve("mod", w.id, ordinal, action, dto.QueuePostRestored)
		at := w.clock
		w.exec(`DROP TRIGGER fail_row`)
		for id, s := range w.flagState(w.id, ordinal) {
			if s != "dismissed pending="+action+" linked=false" {
				w.t.Errorf("flag %d after the failed row: %s", id, s)
			}
		}
		if len(w.flagRows()) != 0 || !strings.Contains(strings.Join(w.audit(), " "), "post.restored by mod") {
			w.t.Errorf("after the failed row: flag rows %v, audit %v", w.flagRows(), w.audit())
		}
		return at, res.Flags
	}
	check := func(w *queueWorld, at int64, action string, n int) {
		w.t.Helper()
		rows := w.flagRows()
		if len(rows) != n {
			w.t.Fatalf("%d flag rows, want %d", len(rows), n)
		}
		r := rows[len(rows)-1]
		if r.action != ActionFlagDismissed || r.actor != "mod" || r.at != at || r.details["recovered"] != true ||
			r.details["action"] != action || r.details["post"] != dto.QueuePostRestored {
			w.t.Errorf("the recovered row %+v", r)
		}
	}

	t.Run("another moderator's request", func(t *testing.T) {
		// The row is written, crediting mod, and mod2 is told somebody
		// handled it: the restore recovered was not theirs.
		w := newQueueWorld(t)
		w.hide(1)
		at, _ := owed(w, 1, QueueDismiss)
		if _, err := w.resolve("mod2", w.id, 1, QueueDismiss); codeOf(err) != problem.CodeConflict {
			t.Fatalf("mod2's request: %v, want conflict", err)
		}
		check(w, at, QueueDismiss, 1)
	})
	t.Run("a retry", func(t *testing.T) {
		w := newQueueWorld(t)
		w.hide(1)
		at, ids := owed(w, 1, QueueDismiss)
		res, err := w.resolve("mod", w.id, 1, QueueDismiss)
		if err != nil || res.Post != dto.QueuePostRestored || !reflect.DeepEqual(res.Flags, ids) {
			t.Fatalf("the retry: %+v, %v", res, err)
		}
		check(w, at, QueueDismiss, 1)
		for id, s := range w.flagState(w.id, 1) {
			if s != "dismissed pending=- linked=true" {
				t.Errorf("flag %d after the retry: %s", id, s)
			}
		}
		w.clock += int64(2 * FlagAuditGrace / time.Millisecond)
		if n, err := w.svc.FlagAuditCatchUp(t.Context()); n != 0 || err != nil {
			t.Errorf("the catch-up after the retry wrote %d, %v", n, err)
		}
		if _, err := w.resolve("mod", w.id, 1, QueueDismiss); codeOf(err) != problem.CodeConflict {
			t.Errorf("a third request: %v, want conflict", err)
		}
		check(w, at, QueueDismiss, 1)
	})
	t.Run("the catch-up", func(t *testing.T) {
		w := newQueueWorld(t)
		w.hide(1)
		at, _ := owed(w, 1, QueueResolve)
		if n, err := w.svc.FlagAuditCatchUp(t.Context()); n != 0 || err != nil {
			t.Errorf("the catch-up inside the grace wrote %d, %v", n, err)
		}
		w.clock += int64(2 * FlagAuditGrace / time.Millisecond)
		if n, err := w.svc.FlagAuditCatchUp(t.Context()); n != 3 || err != nil {
			t.Errorf("the catch-up wrote %d, %v; want 3 flags", n, err)
		}
		check(w, at, QueueResolve, 1)
		if n, _ := w.svc.FlagAuditCatchUp(t.Context()); n != 0 {
			t.Errorf("a second catch-up wrote %d", n)
		}
		if _, err := w.resolve("mod", w.id, 1, QueueResolve); codeOf(err) != problem.CodeConflict {
			t.Errorf("a retry after the catch-up: %v, want conflict", err)
		}
		check(w, at, QueueResolve, 1)
	})
	t.Run("a retried remove", func(t *testing.T) {
		w := newQueueWorld(t)
		w.hide(1)
		at, _ := owed(w, 1, QueueDismiss)
		before := w.logLength()
		if _, err := w.resolve("mod", w.id, 1, QueueRemove); codeOf(err) != problem.CodeConflict {
			t.Errorf("a remove in that state: %v, want conflict", err)
		}
		check(w, at, QueueDismiss, 1)
		if w.logLength() != before {
			t.Error("the refused remove appended")
		}
	})
}

// TestOnlyPendingFlagsAreRecovered is the requirement carried from Terva
// review 1371: a null audit_id alone never means a row is owed. Flags closed
// by ConfirmHide, by the thread page's restore, and by hand before migration
// 0015 all have one, and neither the catch-up nor a queue request writes a
// row for them or changes them. A marked flag whose restore never landed is
// reopened, with no row.
func TestOnlyPendingFlagsAreRecovered(t *testing.T) {
	w := newQueueWorld(t)
	w.hide(1)
	if _, err := w.svc.ConfirmHide(t.Context(), w.as("mod"), w.id, 1, "spam"); err != nil {
		t.Fatal(err)
	}
	restored := w.post("restored from the thread page")
	w.hide(restored)
	if _, err := w.restore("mod", restored); err != nil {
		t.Fatal(err)
	}
	old := w.post("closed before the migration")
	w.mustFlag("rep1", old)
	w.exec(fmt.Sprintf(`UPDATE flags SET status = 'dismissed', resolved_by = %d, resolved_at = 1
		WHERE thread_id = %d AND ordinal = %d`, w.users["mod"], int64(w.id), old))
	states := map[uint32]map[int64]string{}
	for _, o := range []uint32{1, restored, old} {
		states[o] = w.flagState(w.id, o)
		for id, s := range states[o] {
			if strings.Contains(s, "linked=true") || !strings.Contains(s, "pending=-") {
				t.Fatalf("flag %d of post %d: %s", id, o, s)
			}
		}
	}

	w.clock += int64(2 * FlagAuditGrace / time.Millisecond)
	if n, err := w.svc.FlagAuditCatchUp(t.Context()); n != 0 || err != nil {
		t.Errorf("the catch-up wrote %d, %v", n, err)
	}
	for _, o := range []uint32{1, restored, old} {
		if _, err := w.resolve("mod", w.id, o, QueueDismiss); codeOf(err) != problem.CodeConflict {
			t.Errorf("a dismissal of post %d: %v, want conflict", o, err)
		}
		if got := w.flagState(w.id, o); !reflect.DeepEqual(got, states[o]) {
			t.Errorf("post %d's flags went from %v to %v", o, states[o], got)
		}
	}
	if rows := w.flagRows(); len(rows) != 0 {
		t.Errorf("rows written for flags nobody owed: %+v", rows)
	}

	// A marked flag on a post still hidden: its restore failed, and so did
	// the reopen after it. The catch-up reopens it, and writes nothing.
	failed := w.post("a restore that never landed")
	w.hide(failed)
	w.exec(fmt.Sprintf(`UPDATE flags SET status = 'dismissed', resolved_by = %d, resolved_at = %d, audit_pending = 'dismiss'
		WHERE thread_id = %d AND ordinal = %d`, w.users["mod"], w.clock, int64(w.id), failed))
	w.clock += int64(2 * FlagAuditGrace / time.Millisecond)
	if n, err := w.svc.FlagAuditCatchUp(t.Context()); n != 0 || err != nil {
		t.Errorf("the catch-up wrote %d, %v", n, err)
	}
	for id, s := range w.flagState(w.id, failed) {
		if s != "open pending=- linked=false" {
			t.Errorf("flag %d of the failed restore: %s, want reopened", id, s)
		}
	}
	if rows := w.flagRows(); len(rows) != 0 {
		t.Errorf("a row for a restore that never landed: %+v", rows)
	}
}

// TestRecoveryIgnoresSomebodyElsesRestore: a marked flag whose own Restore
// never landed is reopened even when the post is shown, because another
// moderator restored it from the thread page since (Terva review 1522 on PR
// #184). No row credits the first moderator with a restore they did not
// make, and their retry is not told it restored anything.
func TestRecoveryIgnoresSomebodyElsesRestore(t *testing.T) {
	for _, via := range []string{"the catch-up", "a retry"} {
		t.Run(via, func(t *testing.T) {
			w := newQueueWorld(t)
			w.hide(1)
			// What a failed Restore leaves when the reopen after it failed
			// too: the flags dismissed by mod and marked, the post hidden.
			w.exec(fmt.Sprintf(`UPDATE flags SET status = 'dismissed', resolved_by = %d, resolved_at = %d, audit_pending = 'dismiss'
				WHERE thread_id = %d AND ordinal = 1`, w.users["mod"], w.clock, int64(w.id)))
			if _, err := w.svc.Restore(t.Context(), w.as("mod2"), w.id, 1, "not spam"); err != nil {
				t.Fatal(err)
			}
			if !w.entry(1).Flags.Visible() {
				t.Fatal("mod2's restore did not show the post")
			}
			w.clock += int64(2 * FlagAuditGrace / time.Millisecond)
			if via == "the catch-up" {
				if n, err := w.svc.FlagAuditCatchUp(t.Context()); n != 0 || err != nil {
					t.Errorf("the catch-up wrote %d, %v", n, err)
				}
				for id, s := range w.flagState(w.id, 1) {
					if s != "open pending=- linked=false" {
						t.Errorf("flag %d: %s, want reopened", id, s)
					}
				}
				if rows := w.flagRows(); len(rows) != 0 {
					t.Errorf("rows for a restore mod never made: %+v", rows)
				}
				return
			}
			// The retry finds the flags reopened, and dismisses them on the
			// post as it is now: shown, and unchanged by this request.
			res := w.mustResolve("mod", w.id, 1, QueueDismiss, dto.QueuePostUnchanged)
			if len(res.Flags) != 3 {
				t.Errorf("the retry closed %v", res.Flags)
			}
			for _, r := range w.flagRows() {
				if r.details["recovered"] == true || r.details["post"] == dto.QueuePostRestored {
					t.Errorf("a row credits mod with the restore: %+v", r)
				}
			}
		})
	}
}

// TestLateFlagAfterDismiss is AC8: a flag inserted while the dismissal holds
// the lock, after its flags were dismissed, is not dismissed with them. The
// post is restored, the new flag is open and counts one, and the item stays
// listed with the post shown. Flags that reach the threshold again hide it.
//
// The late flag is inserted directly: Flag holds the lock from its check to
// its insert (PR #176), so through Flag it would wait for the dismissal.
func TestLateFlagAfterDismiss(t *testing.T) {
	w := newQueueWorld(t)
	w.hide(1)
	w.writer.set(func(a threadlog.Append) {
		if a.Type == threadlog.TypeRestore {
			w.insertFlag(w.id, 1, "rep4")
		}
	}, nil)
	w.mustResolve("mod", w.id, 1, QueueDismiss, dto.QueuePostRestored)
	w.writer.set(nil, nil)
	if !w.entry(1).Flags.Visible() || w.openFlags(1) != 1 {
		t.Fatalf("after the dismissal: post %#x, %d open flags; want shown with rep4's one", w.entry(1).Flags, w.openFlags(1))
	}
	page, err := w.svc.Queue(t.Context(), w.as("mod"), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || len(page.Items[0].Flag.Flags) != 1 || page.Items[0].Flag.Post.Deleted {
		t.Errorf("the queue after the late flag: %+v", page.Items)
	}
	rows := w.flagRows()
	if ids, _ := rows[0].details["flags"].([]any); len(ids) != 3 {
		t.Errorf("the dismissal row names %v: want the three flags it dismissed, not the late one", rows[0].details["flags"])
	}
	w.mustFlag("rep1", 1)
	if !w.entry(1).Flags.Visible() {
		t.Fatal("two open flags hid the post")
	}
	w.mustFlag("rep2", 1)
	if !w.entry(1).Flags.Has(threadlog.EntryAutoHidden) {
		t.Errorf("three open flags left the post %#x, want hidden pending review", w.entry(1).Flags)
	}
}

// TestQueueActionRefusals: an unknown action and a long note are
// validation-failed; a missing post is not-found; an archived thread is
// conflict; a member is forbidden. Nothing is closed by any of them.
func TestQueueActionRefusals(t *testing.T) {
	w := newQueueWorld(t)
	w.mustFlag("rep1", 1)
	long := strings.Repeat("x", MaxReason+1)
	for _, c := range []struct {
		name string
		v    domain.Viewer
		id   threadid.ID
		o    uint32
		r    Resolution
		want problem.Code
	}{
		{"anonymous", domain.Anonymous(), w.id, 1, Resolution{Action: QueueDismiss}, problem.CodeUnauthenticated},
		{"a member", w.as("rep2"), w.id, 1, Resolution{Action: QueueDismiss}, problem.CodeForbidden},
		{"unknown action", w.as("mod"), w.id, 1, Resolution{Action: "ignore"}, problem.CodeValidationFailed},
		{"long note", w.as("mod"), w.id, 1, Resolution{Action: QueueDismiss, Note: long}, problem.CodeValidationFailed},
		{"no such post", w.as("mod"), w.id, 9, Resolution{Action: QueueDismiss}, problem.CodeNotFound},
		{"no flags", w.as("mod"), w.otherID, 1, Resolution{Action: QueueDismiss}, problem.CodeConflict},
	} {
		if _, err := w.svc.ResolvePost(t.Context(), c.v, c.id, c.o, c.r); codeOf(err) != c.want {
			t.Errorf("%s: %v, want %s", c.name, err, c.want)
		}
	}
	w.exec(fmt.Sprintf(`UPDATE threads SET archived = 1 WHERE id = %d`, int64(w.id)))
	// The cache reads archived when it opens a thread; this one is open, so
	// its flag is set the way an archive would leave it.
	w.cache.Close()
	cache, err := threadcache.New(w.root, threadcache.Options{Sync: threadlog.SyncNever,
		Archived: func(id threadid.ID) threadcache.ArchiveState { return threadcache.ArchiveState{Archived: id == w.id} }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cache.Close() })
	w.cache, w.svc.Threads = cache, cache
	if _, err := w.resolve("mod", w.id, 1, QueueDismiss); codeOf(err) != problem.CodeConflict {
		t.Errorf("an archived thread: %v, want conflict", err)
	}
	if w.openFlags(1) != 1 {
		t.Error("a refusal closed the flag")
	}
}

// A suspension's hide and restore of an author's posts take no flag lock
// stripe (TKT-01M3R10VP3). The tests below prove that the queue's actions
// still end in a serial order against them: HidePostsOf never appends on a
// post hidden pending review, and each of its Deletes refuses a post that
// is already down, so a Delete can land on post 1 only after the queue's
// Restore.

// suspension is the audit id the tests' suspension hides carry.
const suspension = 1

// suspendHide is mod2's hide of the author's posts for suspension 1. It
// only reads the world's clock, so it may run beside a queue action.
func (w *queueWorld) suspendHide() (int, error) {
	return w.svc.HidePostsOf(w.t.Context(), w.as("mod2"), w.users["author"], suspension)
}

// suspendRestore is mod2's restore of the author's posts that suspension 1
// hid, as an unsuspension runs it.
func (w *queueWorld) suspendRestore() (int, error) {
	return w.svc.RestorePostsOf(w.t.Context(), w.as("mod2"), w.users["author"], []int64{suspension})
}

// removals is post ordinal's Delete and Restore records in log order, as
// "Delete auto", "Delete suspension=N", "Delete" or "Restore".
func (w *queueWorld) removals(ordinal uint32) []string {
	w.t.Helper()
	var out []string
	for _, f := range w.frames(0) {
		if f.h.Ordinal != ordinal {
			continue
		}
		switch f.h.Type {
		case threadlog.TypeDelete:
			b, _ := f.body.(*threadlog.DeleteBody)
			switch {
			case b != nil && b.Auto:
				out = append(out, "Delete auto")
			case b != nil && b.Suspension != 0:
				out = append(out, fmt.Sprintf("Delete suspension=%d", b.Suspension))
			default:
				out = append(out, "Delete")
			}
		case threadlog.TypeRestore:
			out = append(out, "Restore")
		}
	}
	return out
}

// dismissedRows counts the flag.dismissed rows, and fails on any other
// flag row.
func (w *queueWorld) dismissedRows() int {
	w.t.Helper()
	n := 0
	for _, r := range w.flagRows() {
		if r.action != ActionFlagDismissed {
			w.t.Errorf("flag row %s by %s: only flag.dismissed is expected", r.action, r.actor)
			continue
		}
		n++
	}
	return n
}

// TestSuspensionHidePassesOverAnAutoHiddenPost: the real HidePostsOf leaves
// a post hidden pending review to the queue. Post 1 keeps its auto-hide and
// its three open flags and gets no second Delete, while the author's shown
// post 2, and their post in "other", get the suspension's Delete.
func TestSuspensionHidePassesOverAnAutoHiddenPost(t *testing.T) {
	w := newQueueWorld(t)
	w.hide(1)
	shown := w.post("still shown")
	n, err := w.suspendHide()
	if err != nil || n != 2 {
		t.Fatalf("the hide answered %d, %v: want 2, post %d here and the post in other", n, err, shown)
	}
	if e := w.entry(1); !e.Flags.Has(threadlog.EntryHiddenByMod|threadlog.EntryAutoHidden) || w.openFlags(1) != 3 {
		t.Errorf("post 1 after the hide: %#x with %d open flags, want hidden pending review with 3", e.Flags, w.openFlags(1))
	}
	if got, want := w.removals(1), []string{"Delete auto"}; !reflect.DeepEqual(got, want) {
		t.Errorf("post 1's removals %v, want %v", got, want)
	}
	if got, want := w.removals(shown), []string{fmt.Sprintf("Delete suspension=%d", suspension)}; !reflect.DeepEqual(got, want) {
		t.Errorf("post %d's removals %v, want %v", shown, got, want)
	}
	if e := w.entry(shown); !e.Flags.Has(threadlog.EntryHiddenByMod) || e.Flags.Has(threadlog.EntryAutoHidden) {
		t.Errorf("post %d after the hide: %#x, want hidden by a moderator", shown, e.Flags)
	}
}

// checkSerial fails unless post 1 and its flag rows are one of the serial
// outcomes of a queue Keep against a suspension's hide, and answers which:
//   - "shown": the hide passed the post before the Restore. 0 flags are
//     open and one flag.dismissed row records the Keep.
//   - "suspension": the Restore landed first and the hide then hid the
//     post. It is hidden by a moderator and not auto-hidden, the Delete in
//     force carries the suspension, 0 flags are open, and there is one
//     flag.dismissed row.
//   - "pending": the Keep failed and changed nothing. Every flag is open
//     and there is no flag row. keepErr must then be an error.
//
// In every outcome no Delete of post 1 follows another Delete without a
// Restore between them (review 1540 on PR #203).
func (w *queueWorld) checkSerial(keepErr error) string {
	w.t.Helper()
	log := w.removals(1)
	down := false
	for i, r := range log {
		isDelete := strings.HasPrefix(r, "Delete")
		if isDelete && down {
			w.t.Errorf("post 1's record %d is a Delete of a post already down: %v", i, log)
		}
		down = isDelete
	}
	f, open, rows := w.entry(1).Flags, w.openFlags(1), w.dismissedRows()
	switch {
	case f.Visible() && open == 0 && rows == 1 && keepErr == nil:
		return "shown"
	case f.Has(threadlog.EntryHiddenByMod) && !f.Has(threadlog.EntryAutoHidden) && !f.Has(threadlog.EntryDeleted) &&
		len(log) > 0 && log[len(log)-1] == fmt.Sprintf("Delete suspension=%d", suspension) &&
		open == 0 && rows == 1 && keepErr == nil:
		return "suspension"
	case f.Has(threadlog.EntryHiddenByMod|threadlog.EntryAutoHidden) && open == 3 && rows == 0 && keepErr != nil:
		return "pending"
	}
	w.t.Errorf("post 1 ends %#x with %d open flags and %d flag.dismissed rows, the Keep answered %v, log %v: not a serial outcome",
		f, open, rows, keepErr, log)
	return ""
}

// TestSuspensionHideWhileTheQueueRestores is the orders, each forced.
//   - (a) The Keep holds its Restore until HidePostsOf has returned. The
//     hide does not wait for it, passes over post 1 and writes nothing on
//     it; after the release the post is shown with its flags dismissed.
//   - (b) The Keep completes first, and the hide then hides the restored
//     post for the suspension, its flags still dismissed.
//   - (c) The hide lands between the Keep's Restore and its row, and the
//     outcome is (b)'s.
func TestSuspensionHideWhileTheQueueRestores(t *testing.T) {
	t.Run("the hide first", func(t *testing.T) {
		w := newQueueWorld(t)
		w.hide(1)
		holding, release := make(chan struct{}), make(chan struct{})
		w.writer.set(func(a threadlog.Append) {
			if a.Type == threadlog.TypeRestore {
				close(holding)
				<-release
			}
		}, nil)
		kept := make(chan error, 1)
		go func() {
			_, err := w.svc.ResolvePost(t.Context(), w.as("mod"), w.id, 1, Resolution{Action: QueueResolve})
			kept <- err
		}()
		// A Keep that answers before it reaches its Restore never closes
		// holding, so wait for either, with a guard against neither (Terva
		// review 1573 on PR #217).
		select {
		case <-holding:
		case err := <-kept:
			t.Fatalf("the Keep answered %v before it reached its Restore", err)
		case <-time.After(10 * time.Second):
			t.Fatal("the Keep never reached its Restore")
		}
		before := w.logLength()
		type answer struct {
			n   int
			err error
		}
		hid := make(chan answer, 1)
		go func() {
			n, err := w.suspendHide()
			hid <- answer{n, err}
		}()
		var a answer
		select {
		case a = <-hid:
		case <-time.After(10 * time.Second):
			close(release)
			t.Fatal("the hide waited for the queue's Restore")
		}
		if a.err != nil || a.n != 1 {
			t.Errorf("the hide answered %d, %v: want 1, the post in other and not post 1", a.n, a.err)
		}
		if recs := w.frames(before); len(recs) != 0 {
			t.Errorf("the hide wrote %d records on the queue's thread, want none", len(recs))
		}
		close(release)
		if err := <-kept; err != nil {
			t.Fatalf("the Keep: %v", err)
		}
		w.writer.set(nil, nil)
		if got := w.checkSerial(nil); got != "shown" {
			t.Errorf("the outcome is %q, want shown", got)
		}
	})
	t.Run("the Keep first", func(t *testing.T) {
		w := newQueueWorld(t)
		w.hide(1)
		w.mustResolve("mod", w.id, 1, QueueResolve, dto.QueuePostRestored)
		if n, err := w.suspendHide(); err != nil || n != 2 {
			t.Errorf("the hide answered %d, %v: want 2, post 1 and the post in other", n, err)
		}
		if got := w.checkSerial(nil); got != "suspension" {
			t.Errorf("the outcome is %q, want suspension", got)
		}
		if got, want := w.removals(1), []string{"Delete auto", "Restore", "Delete suspension=1"}; !reflect.DeepEqual(got, want) {
			t.Errorf("post 1's removals %v, want %v", got, want)
		}
	})
	// (c) The hide lands after the Keep's Restore and before its
	// flag.dismissed row (Terva review 1575 on PR #217). The Keep's restore
	// path dismisses the flags before its Restore and never reaches
	// closeFlags, so this is (b) with the row written late: the Keep
	// answers restored, and the flags end dismissed with their row. The
	// hide runs from the Keep's projection of its Restore, which comes
	// after the append and before the row.
	t.Run("the hide between the Restore and the row", func(t *testing.T) {
		w := newQueueWorld(t)
		w.hide(1)
		inner := w.svc.Project
		armed, fired := true, false
		var (
			hidden  int
			hideErr error
		)
		w.svc.Project = func(ctx context.Context, id threadid.ID, n int64) error {
			if err := inner(ctx, id, n); err != nil {
				return err
			}
			if armed && id == w.id && w.entry(1).Flags.Visible() {
				armed = false // the hide projects too, through here
				if open, rows := w.openFlags(1), len(w.flagRows()); open != 0 || rows != 0 {
					t.Errorf("at the Restore's projection: %d open flags and %d flag rows, want the flags dismissed and no row yet", open, rows)
				}
				hidden, hideErr = w.suspendHide()
				fired = true
			}
			return nil
		}
		res, err := w.svc.ResolvePost(t.Context(), w.as("mod"), w.id, 1, Resolution{Action: QueueResolve})
		w.svc.Project = inner
		if !fired {
			t.Fatal("the hide never ran between the Restore and the row")
		}
		if err != nil || res.Post != dto.QueuePostRestored || len(res.Flags) != 3 {
			t.Errorf("the Keep answered %+v, %v: want restored, naming the 3 flags it dismissed", res, err)
		}
		if hideErr != nil || hidden != 2 {
			t.Errorf("the hide answered %d, %v: want 2, post 1 and the post in other", hidden, hideErr)
		}
		if got := w.checkSerial(err); got != "suspension" {
			t.Errorf("the outcome is %q, want suspension", got)
		}
		if got, want := w.removals(1), []string{"Delete auto", "Restore", "Delete suspension=1"}; !reflect.DeepEqual(got, want) {
			t.Errorf("post 1's removals %v, want %v", got, want)
		}
	})
}

// raceRuns is how many fresh worlds TestSuspensionHideRacesTheQueueRestore
// runs. It stays at 20 so that the package keeps inside the CI race step's
// timeout under runner load (TKT-01M3R5P3CN).
const raceRuns = 20

// TestSuspensionHideRacesTheQueueRestore, under -race: in each of raceRuns
// fresh worlds a Keep of post 1, hidden pending review, and a suspension's
// hide of its author start together from a closed channel, with no lock
// between them. Whatever the scheduler does, post 1 and its flag rows end
// in a serial outcome (checkSerial), and the hide's count agrees with it.
// The forced orders are TestSuspensionHideWhileTheQueueRestores; this is
// the interleavings between them.
func TestSuspensionHideRacesTheQueueRestore(t *testing.T) {
	seen := map[string]int{}
	for i := range raceRuns {
		w := newQueueWorld(t)
		w.hide(1)
		start := make(chan struct{})
		var (
			wg      sync.WaitGroup
			keepErr error
			hidden  int
			hideErr error
		)
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			_, keepErr = w.svc.ResolvePost(t.Context(), w.as("mod"), w.id, 1, Resolution{Action: QueueResolve})
		}()
		go func() {
			defer wg.Done()
			<-start
			hidden, hideErr = w.suspendHide()
		}()
		close(start)
		wg.Wait()
		if hideErr != nil {
			t.Errorf("run %d: the hide: %v", i, hideErr)
		}
		out := w.checkSerial(keepErr)
		want := 1 // the post in other
		if out == "suspension" {
			want = 2
		}
		if out != "" && hidden != want {
			t.Errorf("run %d: the outcome is %s and the hide answered %d, want %d", i, out, hidden, want)
		}
		if t.Failed() {
			t.Fatalf("run %d failed; outcomes so far %v", i, seen)
		}
		seen[out]++
	}
	t.Logf("outcomes over %d runs: %v", raceRuns, seen)
}
