package api

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/events"
	"github.com/terva-sh/ketju/internal/events/projection"
	"github.com/terva-sh/ketju/internal/jobs"
	"github.com/terva-sh/ketju/internal/perm"
	"github.com/terva-sh/ketju/internal/service/account"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadcache"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// Tests for a suspension's hidden posts and its expiry (TKT-01M29CP2QK). In
// the seeded thread bob moderates and #4 is carol's; each test gives carol
// more posts, and bob suspends her.

// suspendedWorld is the seeded thread with carol's #5 and #6 added, and
// #6 hidden by bob for its own sake before any suspension.
func suspendedWorld(t *testing.T) (*world, threadid.ID) {
	t.Helper()
	w := newWorld(t)
	id, _ := w.seed()
	w.post(id, "carol", "A second post from carol.", 0)
	w.post(id, "carol", "A third post from carol.", 0)
	changed(t, send(w.chain(), "bob", http.MethodDelete, postPathN(id, 6), `{"reason":"rude"}`))
	return w, id
}

// admin sends method path body as bob, the moderator, through the handler,
// and fails unless it answers 204.
func (w *world) admin(v domain.Viewer, method, path, body string) {
	w.t.Helper()
	if rec := w.sendAs(v, method, path, body); rec.Code != http.StatusNoContent {
		w.t.Fatalf("%s %s: %d %s", method, path, rec.Code, rec.Body)
	}
}

// hidden reports which of ordinals are hidden by a moderator.
func (w *world) hidden(id threadid.ID, ordinals ...uint32) []uint32 {
	w.t.Helper()
	var out []uint32
	for _, o := range ordinals {
		if w.entry(id, o).Flags.Has(threadlog.EntryHiddenByMod) {
			out = append(out, o)
		}
	}
	return out
}

// postCounts is carol's profile post count as alice reads it, and the
// forum's post total, forum_thread_counts.live_posts, which the forum index
// shows (docs/07).
func (w *world) postCounts() (profile, forum int) {
	w.t.Helper()
	rec := w.get(w.viewer("alice"), "/api/v1/users/carol")
	if rec.Code != http.StatusOK {
		w.t.Fatalf("carol's profile: %d %s", rec.Code, rec.Body)
	}
	var p dto.Profile
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		w.t.Fatal(err)
	}
	if err := w.db.Read(w.t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		return tx.QueryRowContext(w.t.Context(), `SELECT live_posts FROM forum_thread_counts WHERE forum_id = ?`, w.forum).Scan(&forum)
	}); err != nil {
		w.t.Fatal(err)
	}
	return p.PostCount, forum
}

// suspensionID is the id of carol's newest user.suspended row.
func (w *world) suspensionID() int64 {
	w.t.Helper()
	var id int64
	if err := w.db.Read(w.t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		return tx.QueryRowContext(w.t.Context(), `SELECT max(id) FROM audit_log WHERE action = 'user.suspended'
			AND target_type = 'user' AND target_id = ?`, fmt.Sprint(w.users["carol"])).Scan(&id)
	}); err != nil {
		w.t.Fatal(err)
	}
	return id
}

// deletedOnPage is which of the page's posts alice reads as removed.
func (w *world) deletedOnPage(id threadid.ID) []int {
	w.t.Helper()
	var out []int
	for _, p := range page(w.t, w.get(w.viewer("alice"), "/api/v1/threads/"+id.String())).Posts {
		if p.Deleted {
			out = append(out, int(p.Ordinal))
		}
	}
	return out
}

const carolPosts = "/api/v1/admin/users/carol/"

// TestHidePostsIsReversible is AC4: hide_posts appends a hidden-by-mod
// Delete carrying the suspension's audit id to each of carol's visible
// posts, which members then read as removed, and which leave her profile
// count and the forum's total. Restoring appends a Restore to exactly those:
// #6, which bob hid for its own sake first, stays hidden, and so does #5,
// which bob restored during the suspension and then hid again for a reason
// of his own.
func TestHidePostsIsReversible(t *testing.T) {
	w, id := suspendedWorld(t)
	bob := w.viewer("bob")
	profile, forum := w.postCounts()
	if got := w.deletedOnPage(id); !slices.Equal(got, []int{3, 6}) {
		t.Fatalf("before: removed %v", got)
	}

	w.admin(bob, http.MethodPost, carolPosts+"suspend", `{"reason":"spam","hide_posts":true}`)
	sid := w.suspensionID()
	if got := w.hidden(id, 4, 5, 6); !slices.Equal(got, []uint32{4, 5, 6}) {
		t.Fatalf("after the hide, hidden-by-mod: %v", got)
	}
	for _, o := range []uint32{4, 5} {
		fr := w.frames(id, o)
		last := fr[len(fr)-1]
		d, ok := last.body.(*threadlog.DeleteBody)
		if !ok || !d.ByModerator || d.Suspension != uint64(sid) || last.h.Actor != uint64(w.users["bob"]) {
			t.Errorf("#%d's last record is %s by %d with %+v: want bob's hide carrying suspension %d",
				o, last.h.Type, last.h.Actor, last.body, sid)
		}
	}
	if fr := w.frames(id, 6); len(fr) != 2 {
		t.Errorf("#6 was already hidden and gained a record: %d frames", len(fr))
	}
	if got := w.deletedOnPage(id); !slices.Equal(got, []int{3, 4, 5, 6}) {
		t.Errorf("after the hide alice reads as removed %v", got)
	}
	if p, f := w.postCounts(); p != profile-2 || f != forum-2 {
		t.Errorf("after the hide: profile %d forum %d, want %d and %d", p, f, profile-2, forum-2)
	}

	// Bob restores #5 by hand, then hides it again for his own reason.
	changed(t, send(w.chain(), "bob", http.MethodPost, postPathN(id, 5)+"/restore", `{}`))
	changed(t, send(w.chain(), "bob", http.MethodDelete, postPathN(id, 5), `{"reason":"also rude"}`))

	w.admin(bob, http.MethodPost, carolPosts+"unsuspend", `{"restore_posts":true}`)
	if got := w.hidden(id, 4, 5, 6); !slices.Equal(got, []uint32{5, 6}) {
		t.Fatalf("after the restore, hidden-by-mod: %v, want 5 and 6", got)
	}
	if got := w.deletedOnPage(id); !slices.Equal(got, []int{3, 5, 6}) {
		t.Errorf("after the restore alice reads as removed %v", got)
	}
	if p, f := w.postCounts(); p != profile-1 || f != forum-1 {
		t.Errorf("after the restore: profile %d forum %d, want %d and %d", p, f, profile-1, forum-1)
	}
	fr := w.frames(id, 4)
	if last := fr[len(fr)-1]; last.h.Type != threadlog.TypeRestore || last.h.Actor != uint64(w.users["bob"]) {
		t.Errorf("#4's last record is %s by %d, want bob's Restore", last.h.Type, last.h.Actor)
	}

	// A second restore finds nothing of the suspension's left.
	before := w.logState(id)
	w.admin(bob, http.MethodPost, carolPosts+"posts/restore", "")
	if after := w.logState(id); after.size != before.size {
		t.Errorf("a second restore wrote %d bytes", after.size-before.size)
	}
}

// TestHidePostsIsIdempotent: hiding again appends nothing and writes no
// row, and the hide route refuses an account that is not suspended.
func TestHidePostsIsIdempotent(t *testing.T) {
	w, id := suspendedWorld(t)
	bob := w.viewer("bob")
	w.admin(bob, http.MethodPost, carolPosts+"suspend", `{"reason":"spam","hide_posts":true}`)
	before, rows := w.logState(id), len(w.auditLog())
	w.admin(bob, http.MethodPost, carolPosts+"posts/hide", "")
	if after := w.logState(id); after != before {
		t.Errorf("hiding again changed the log: %+v, was %+v", after, before)
	}
	if got := w.auditSince(rows); len(got) != 0 {
		t.Errorf("hiding again wrote %v", got)
	}

	w.admin(bob, http.MethodPost, carolPosts+"unsuspend", "")
	if got := w.hidden(id, 4, 5); !slices.Equal(got, []uint32{4, 5}) {
		t.Errorf("an unsuspension without restore_posts left hidden %v, want 4 and 5", got)
	}
	if rec := w.sendAs(bob, http.MethodPost, carolPosts+"posts/hide", ""); rec.Code != http.StatusConflict {
		t.Errorf("hiding an active account's posts: %d %s, want 409", rec.Code, rec.Body)
	}
}

// TestSuspensionHideMovesTheCacheKey: a page cached for a member before the
// hide is not served after it.
func TestSuspensionHideMovesTheCacheKey(t *testing.T) {
	w, id := suspendedWorld(t)
	path := "/api/v1/threads/" + id.String()
	first := w.get(w.viewer("alice"), path)
	if again := w.get(w.viewer("alice"), path); again.Header().Get("ETag") != first.Header().Get("ETag") {
		t.Fatal("the page is not stable before the hide")
	}
	w.admin(w.viewer("bob"), http.MethodPost, carolPosts+"suspend", `{"reason":"spam","hide_posts":true}`)
	after := w.get(w.viewer("alice"), path)
	if after.Header().Get("ETag") == first.Header().Get("ETag") {
		t.Error("the hide left the ETag where it was")
	}
	if got := w.deletedOnPage(id); !slices.Contains(got, 4) {
		t.Errorf("after the hide the page removes %v, want #4 among them", got)
	}
}

// TestSuspensionAudited is AC2: suspend, hide, unsuspend and restore each
// write their rows, naming bob and carol, and each hidden or restored post
// has the projection's row, the hides naming the suspension.
func TestSuspensionAudited(t *testing.T) {
	w, id := suspendedWorld(t)
	bob := w.viewer("bob")
	rows := len(w.auditLog())
	w.admin(bob, http.MethodPost, carolPosts+"suspend", `{"reason":"spam","hide_posts":true}`)
	w.admin(bob, http.MethodPost, carolPosts+"unsuspend", `{"restore_posts":true}`)
	sid := w.suspensionID()

	carol, by := fmt.Sprint(w.users["carol"]), fmt.Sprint(w.users["bob"])
	var got []string
	for _, a := range w.auditLog()[rows:] {
		actor := "null"
		if a.actor.Valid {
			actor = fmt.Sprint(a.actor.Int64)
		}
		line := fmt.Sprintf("%s by %s on %s/%s", a.action, actor, a.typ, a.id)
		switch a.action {
		case projection.ActionPostHidden:
			line += fmt.Sprintf(" suspension %v", a.details["suspension"])
		case account.ActionPostsHidden:
			line += fmt.Sprintf(" suspension %v posts %v", a.details["suspension"], a.details["posts"])
		case account.ActionPostsRestored:
			line += fmt.Sprintf(" posts %v", a.details["posts"])
		}
		got = append(got, line)
	}
	post := func(o int) string { return fmt.Sprintf("post/%s/%d", id, o) }
	want := []string{
		"user.suspended by " + by + " on user/" + carol,
		"post.hidden by " + by + " on " + post(4) + fmt.Sprintf(" suspension %d", sid),
		"post.hidden by " + by + " on " + post(5) + fmt.Sprintf(" suspension %d", sid),
		"user.posts_hidden by " + by + " on user/" + carol + fmt.Sprintf(" suspension %d posts 2", sid),
		"user.unsuspended by " + by + " on user/" + carol,
		"post.restored by " + by + " on " + post(4),
		"post.restored by " + by + " on " + post(5),
		"user.posts_restored by " + by + " on user/" + carol + " posts 2",
	}
	if !slices.Equal(got, want) {
		t.Errorf("audit rows\n  %v\nwant\n  %v", got, want)
	}
}

// TestSuspensionExpires is AC3 and AC7: the job lifts a suspension within a
// run of its end and no sooner, as an unsuspension by no account via
// expiry, after which carol is an active member again; the posts the
// suspension hid stay hidden. A suspension with no end is never lifted.
func TestSuspensionExpires(t *testing.T) {
	w, id := suspendedWorld(t)
	end := time.Now().Add(time.Hour).Truncate(time.Millisecond)
	w.admin(w.viewer("bob"), http.MethodPost, carolPosts+"suspend",
		fmt.Sprintf(`{"reason":"spam","until":%d,"hide_posts":true}`, end.UnixMilli()))
	w.admin(w.viewer("bob"), http.MethodPost, "/api/v1/admin/users/alice/suspend", `{"reason":"no end"}`)

	now := end.Add(-time.Second)
	job := jobs.SuspensionExpiry(&account.Service{DB: w.db, Perm: perm.New(w.db), Now: func() time.Time { return now }})
	if job.Every > time.Minute {
		t.Errorf("the job runs every %s, want a minute at most", job.Every)
	}
	if res, err := job.Run(t.Context()); err != nil || res.Removed != 0 {
		t.Fatalf("a second before the end: lifted %d (%v)", res.Removed, err)
	}
	if v := w.session("carol"); v.Active {
		t.Fatal("carol is active before her suspension ends")
	}

	rows := len(w.auditLog())
	now = end
	if res, err := job.Run(t.Context()); err != nil || res.Removed != 1 {
		t.Fatalf("at the end: lifted %d (%v), want 1", res.Removed, err)
	}
	if v := w.session("carol"); !v.Active {
		t.Error("carol is not active after her suspension ended")
	}
	if v := w.session("alice"); v.Active {
		t.Error("a suspension with no end was lifted")
	}
	got := w.auditLog()[rows:]
	if len(got) != 1 || got[0].action != "user.unsuspended" || got[0].actor.Valid || got[0].details["via"] != "expiry" ||
		got[0].id != fmt.Sprint(w.users["carol"]) {
		t.Errorf("the expiry wrote %+v: want one user.unsuspended of carol, no actor, via expiry", got)
	}
	if got := w.hidden(id, 4, 5); !slices.Equal(got, []uint32{4, 5}) {
		t.Errorf("after the expiry hidden-by-mod %v: the suspension's posts must stay hidden", got)
	}

	// A moderator restores them afterwards, from the restore route.
	w.admin(w.viewer("bob"), http.MethodPost, carolPosts+"posts/restore", "")
	if got := w.hidden(id, 4, 5, 6); !slices.Equal(got, []uint32{6}) {
		t.Errorf("after the moderator's restore hidden-by-mod %v, want only #6", got)
	}
	if res, err := job.Run(t.Context()); err != nil || res.Removed != 0 {
		t.Errorf("a later run lifted %d (%v)", res.Removed, err)
	}
}

// A moderator's hide passes over a forum they cannot see, and says nothing
// about it; an admin's reaches it. Restoring follows the same rule.
func TestHideSkipsForumsTheModeratorCannotModerate(t *testing.T) {
	w, id := suspendedWorld(t)
	var staff int64
	w.write(func(q *dbq.Queries) error {
		var err error
		staff, err = q.CreateForum(t.Context(), dbq.CreateForumParams{Slug: "staff", Name: "Staff",
			Visibility: domain.VisibilityRestricted, CreatedAt: 1, UpdatedAt: 1})
		return err
	})
	hiddenThread := w.threadIn(9, staff, "Staff only", "carol")
	root := w.anonymizer()

	w.admin(w.viewer("bob"), http.MethodPost, carolPosts+"suspend", `{"reason":"spam","hide_posts":true}`)
	if got := w.hidden(hiddenThread, 1); len(got) != 0 {
		t.Fatal("bob's hide reached a forum he cannot see")
	}
	for _, a := range w.auditLog() {
		if a.action == account.ActionPostsHidden && a.details["posts"] != float64(2) {
			t.Errorf("bob's row counts %v posts, want 2", a.details["posts"])
		}
	}
	w.admin(root, http.MethodPost, carolPosts+"posts/hide", "")
	if got := w.hidden(hiddenThread, 1); len(got) != 1 {
		t.Error("an admin's hide did not reach the restricted forum")
	}
	w.admin(w.viewer("bob"), http.MethodPost, carolPosts+"posts/restore", "")
	if got := w.hidden(hiddenThread, 1); len(got) != 1 {
		t.Error("bob's restore reached a forum he cannot see")
	}
	if got := w.hidden(id, 4, 5); len(got) != 0 {
		t.Errorf("bob's restore left %v hidden", got)
	}
	w.admin(root, http.MethodPost, carolPosts+"posts/restore", "")
	if got := w.hidden(hiddenThread, 1); len(got) != 0 {
		t.Error("an admin's restore did not reach the restricted forum")
	}
}

// Who may hide and restore is who may suspend: a moderator acts on members
// only, a member on nobody, and nobody on themselves. A refusal writes
// nothing.
func TestHideAndRestoreAuthority(t *testing.T) {
	w, id := suspendedWorld(t)
	root := w.anonymizer()
	w.write(func(q *dbq.Queries) error {
		_, err := q.CreateUser(t.Context(), dbq.CreateUserParams{Handle: "dave", DisplayName: "Dave",
			Status: domain.StatusActive, Role: domain.RoleModerator, Kind: domain.KindHuman, CreatedAt: 1, UpdatedAt: 1})
		return err
	})
	w.admin(root, http.MethodPost, "/api/v1/admin/users/dave/suspend", `{"reason":"test"}`)
	w.admin(w.viewer("bob"), http.MethodPost, carolPosts+"suspend", `{"reason":"spam"}`)
	before := w.logState(id)
	for _, c := range []struct {
		name string
		v    domain.Viewer
		path string
		want int
	}{
		{"a moderator on a moderator", w.viewer("bob"), "/api/v1/admin/users/dave/posts/hide", http.StatusForbidden},
		{"a moderator on an admin", w.viewer("bob"), "/api/v1/admin/users/root/posts/restore", http.StatusForbidden},
		{"a moderator on themselves", w.viewer("bob"), "/api/v1/admin/users/bob/posts/restore", http.StatusForbidden},
		{"a member", w.viewer("alice"), carolPosts + "posts/hide", http.StatusForbidden},
		{"anonymous", domain.Anonymous(), carolPosts + "posts/restore", http.StatusUnauthorized},
		{"an admin on themselves", root, "/api/v1/admin/users/root/posts/restore", http.StatusForbidden},
		{"nobody's handle", root, "/api/v1/admin/users/ghost/posts/restore", http.StatusNotFound},
		{"suspend with an unknown field", root, carolPosts + "suspend", http.StatusBadRequest},
	} {
		body := ""
		if c.name == "suspend with an unknown field" {
			body = `{"reason":"spam","hide_post":true}`
		}
		if rec := w.sendAs(c.v, http.MethodPost, c.path, body); rec.Code != c.want {
			t.Errorf("%s: %d %s, want %d", c.name, rec.Code, rec.Body, c.want)
		}
	}
	if after := w.logState(id); after.size != before.size {
		t.Error("a refused hide or restore wrote to the log")
	}
}

// The restore decides from the log and appends at the length it read. Here,
// just after it restores #4, another moderator restores #5 and hides it again
// for their own reason. The next Restore, decided before those two records,
// is refused, the thread is walked again, and #5 stays hidden.
func TestRestoreDecidesAgainWhenTheLogMoves(t *testing.T) {
	w, id := suspendedWorld(t)
	bob := w.viewer("bob")
	w.admin(bob, http.MethodPost, carolPosts+"suspend", `{"reason":"spam","hide_posts":true}`)

	fired := false
	w.svc.Writer.Publish = func(ev events.Event) {
		w.events.add(ev)
		if fired || ev.Thread != id {
			return
		}
		fired = true
		if err := w.cache.With(id, func(th *threadcache.Thread) error {
			if _, err := th.Actor.Append(threadlog.Append{Type: threadlog.TypeRestore, Author: uint64(w.users["bob"]),
				Ordinal: 5, Body: &threadlog.RestoreBody{}}); err != nil {
				return err
			}
			_, err := th.Actor.Append(threadlog.Append{Type: threadlog.TypeDelete, Author: uint64(w.users["bob"]),
				Ordinal: 5, Body: &threadlog.DeleteBody{Reason: "own", ByModerator: true}})
			return err
		}); err != nil {
			t.Error(err)
		}
	}
	w.admin(bob, http.MethodPost, carolPosts+"unsuspend", `{"restore_posts":true}`)
	if !fired {
		t.Fatal("the interleaving never ran")
	}
	if got := w.hidden(id, 4, 5); !slices.Equal(got, []uint32{5}) {
		t.Errorf("hidden-by-mod %v, want only #5, which a moderator hid again for their own reason", got)
	}
}

// A thread moved into a forum the moderator cannot see, whose move the
// projection has not caught up with, is judged by the forum the log says
// (Terva review 1495 on PR #169). The hide projects the thread under it and
// admits the moderator from the row that follows, so bob's hide passes it
// over, while carol's post elsewhere is hidden.
func TestHideJudgesTheForumTheLogSays(t *testing.T) {
	w, id := suspendedWorld(t)
	other := w.threadIn(2, w.forum, "Another thread", "carol")
	var staff int64
	w.write(func(q *dbq.Queries) error {
		var err error
		staff, err = q.CreateForum(t.Context(), dbq.CreateForumParams{Slug: "staff", Name: "Staff",
			Visibility: domain.VisibilityRestricted, CreatedAt: 1, UpdatedAt: 1})
		return err
	})
	// Appended and not projected: the row still says the general forum.
	if err := w.cache.With(id, func(th *threadcache.Thread) error {
		_, err := th.Actor.Append(threadlog.Append{Type: threadlog.TypeThreadMoved, Author: uint64(w.users["bob"]),
			Body: &threadlog.ThreadMovedBody{ForumID: uint64(staff)}})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	w.admin(w.viewer("bob"), http.MethodPost, carolPosts+"suspend", `{"reason":"spam","hide_posts":true}`)
	if got := w.hidden(id, 4, 5); len(got) != 0 {
		t.Errorf("bob's hide reached %v in a thread the log had moved out of his sight", got)
	}
	if got := w.hidden(other, 1); len(got) != 1 {
		t.Error("carol's post in a thread bob moderates was not hidden")
	}
}

// A thread that cannot be opened is passed over, every other thread is
// still done, and the answer says to run it again (Terva review 1495 on PR
// #169). The thread row here has no log behind it and sorts first.
func TestHideGoesPastAThreadItCannotOpen(t *testing.T) {
	w, id := suspendedWorld(t)
	w.exec(`INSERT INTO threads (id, forum_id, slug, title, author_id, created_at, post_count, last_post_at,
		last_post_author_id, log_length_projected) VALUES (65537, ?, 'ghost', 'Ghost', ?, 1, 1, 1, ?, 0)`,
		w.forum, w.users["carol"], w.users["carol"])
	bob := w.viewer("bob")
	if rec := w.sendAs(bob, http.MethodPost, carolPosts+"suspend", `{"reason":"spam","hide_posts":true}`); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("suspend with a thread it cannot open: %d %s, want 503", rec.Code, rec.Body)
	}
	if s := w.session("carol"); s.Active {
		t.Error("the suspension did not commit")
	}
	if got := w.hidden(id, 4, 5); !slices.Equal(got, []uint32{4, 5}) {
		t.Errorf("the thread after the one it could not open has hidden %v, want 4 and 5", got)
	}
	before := w.logState(id)
	if rec := w.sendAs(bob, http.MethodPost, carolPosts+"posts/hide", ""); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("hiding again: %d %s, want 503 while the thread is still unreachable", rec.Code, rec.Body)
	}
	if after := w.logState(id); after != before {
		t.Error("hiding again appended to a thread already done")
	}
}
