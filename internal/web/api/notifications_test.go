package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/auth/session"
	"github.com/terva-sh/ketju/internal/auth/session/sessiontest"
	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/events"
	"github.com/terva-sh/ketju/internal/events/notifier"
	"github.com/terva-sh/ketju/internal/service/applications"
	formsvc "github.com/terva-sh/ketju/internal/service/forms"
	"github.com/terva-sh/ketju/internal/service/forums"
	"github.com/terva-sh/ketju/internal/service/users"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadcache"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
	"github.com/terva-sh/ketju/internal/web"
)

// notifyWorld is the thread world with the notification generator writing
// rows from its logs, signed-in sessions for each member, and the API behind
// the session middleware, so /me reads the viewer as a real request does
// (TKT-01M29CRJ2T). dave is a member the base world lacks, and restricted is
// a forum nobody reads without a grant.
type notifyWorld struct {
	*world
	gen        *notifier.Generator
	api        http.Handler
	tokens     map[string]string
	restricted int64
	seq        uint64
}

func newNotifyWorld(t *testing.T) *notifyWorld {
	t.Helper()
	w := &notifyWorld{world: newWorld(t), tokens: map[string]string{}, seq: 100}
	w.write(func(q *dbq.Queries) error {
		id, err := q.CreateUser(t.Context(), dbq.CreateUserParams{Handle: "dave", DisplayName: "Dave", Status: "active",
			Role: domain.RoleMember, Kind: "human", CreatedAt: 1, UpdatedAt: 1})
		w.users["dave"] = id
		return err
	})
	w.write(func(q *dbq.Queries) error {
		var err error
		w.restricted, err = q.CreateForum(t.Context(), dbq.CreateForumParams{Slug: "staff", Name: "Staff",
			Visibility: domain.VisibilityRestricted, CreatedAt: 1, UpdatedAt: 1})
		return err
	})
	gen, err := notifier.New(notifier.Options{DB: w.db, Root: w.root, Perm: w.svc.Perm, Since: 1,
		Now: func() time.Time { return time.UnixMilli(w.clock) },
		Project: func(ctx context.Context, id threadid.ID, upTo int64) error {
			_, err := w.proj.ProjectThread(ctx, id, upTo)
			return err
		},
		Sweep: func(context.Context) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	w.gen = gen
	us := &users.Service{DB: w.db, Perm: w.svc.Perm, VisibleNotifications: w.svc.CountVisibleNotifications}
	pass := func(h http.Handler) http.Handler { return h }
	w.api = web.New(web.Groups{API: New(Deps{Threads: w.svc, Users: us, Forums: &forums.Service{DB: w.db, Perm: w.svc.Perm},
		Forms: &formsvc.Service{DB: w.db}, Applications: &applications.Service{DB: w.db}, Perm: w.svc.Perm})},
		web.Options{Session: session.Middleware(&session.Store{DB: w.db}, nil), RateLimit: pass, CSRF: pass})
	for _, h := range []string{"alice", "bob", "carol", "dave"} {
		w.tokens[h] = sessiontest.SignIn(t, w.db, w.users[h], time.Now())
	}
	return w
}

// sql runs statements and drops every cached permission decision, as a
// grant or membership change through perm would.
func (w *notifyWorld) sql(query string, args ...any) {
	w.t.Helper()
	if err := w.db.Write(w.t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		_, err := tx.ExecContext(w.t.Context(), query, args...)
		return err
	}); err != nil {
		w.t.Fatalf("%s: %v", query, err)
	}
	w.svc.Perm.Invalidate()
}

// threadIn starts a thread in forum, by alice, and generates for it.
func (w *notifyWorld) threadIn(forum int64, title string) threadid.ID {
	w.t.Helper()
	w.seq++
	id := threadid.ID(uint64(1_757_500_000_000)<<16 | w.seq)
	th, err := w.cache.Create(id, uint64(forum), w.clock)
	if err != nil {
		w.t.Fatal(err)
	}
	w.cache.Release(th)
	w.generate(id, w.append(id, threadlog.Append{Type: threadlog.TypeThreadCreated, Author: uint64(w.users["alice"]),
		Body: &threadlog.ThreadCreatedBody{Title: title, Slug: strings.ToLower(strings.ReplaceAll(title, " ", "-")),
			ForumID: uint64(forum)}}))
	return id
}

// say appends author's post naming mentions, answering replyTo, and
// generates for it.
func (w *notifyWorld) say(id threadid.ID, author, source string, replyTo uint32, mentions ...string) {
	w.t.Helper()
	var ids []uint64
	for _, m := range mentions {
		ids = append(ids, uint64(w.users[m]))
	}
	w.generate(id, w.append(id, threadlog.Append{Type: threadlog.TypePost, Author: uint64(w.users[author]),
		Body: &threadlog.PostBody{Source: source, ReplyTo: replyTo, Mentions: ids}}))
}

// record appends a and generates for it.
func (w *notifyWorld) record(id threadid.ID, a threadlog.Append) {
	w.t.Helper()
	w.generate(id, w.append(id, a))
}

func (w *notifyWorld) generate(id threadid.ID, n int64) {
	w.t.Helper()
	if err := w.gen.Deliver(w.t.Context(), events.Batch{Events: []events.Event{{Thread: id, LogLength: n}}}); err != nil {
		w.t.Fatal(err)
	}
}

// call sends a request as handle ("" for nobody, or a raw token).
func (w *notifyWorld) call(handle, method, path, body string) *httptest.ResponseRecorder {
	w.t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, "https://forum.example"+path, nil)
	} else {
		r = httptest.NewRequest(method, "https://forum.example"+path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	}
	token := w.tokens[handle]
	if token == "" {
		token = handle
	}
	if token != "" {
		r.AddCookie(sessiontest.Cookie(token))
	}
	rec := httptest.NewRecorder()
	w.api.ServeHTTP(rec, r)
	return rec
}

// list is every page of handle's GET /notifications, in order.
func (w *notifyWorld) list(handle string) []dto.Notification {
	w.t.Helper()
	var out []dto.Notification
	cursor := ""
	for range 100 {
		rec := w.call(handle, http.MethodGet, "/api/v1/notifications?cursor="+cursor, "")
		if rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != "no-store" {
			w.t.Fatalf("GET /notifications as %s: %d, Cache-Control %q: %s", handle, rec.Code,
				rec.Header().Get("Cache-Control"), rec.Body)
		}
		var page dto.NotificationList
		dec := json.NewDecoder(bytes.NewReader(rec.Body.Bytes()))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&page); err != nil {
			w.t.Fatal(err)
		}
		out = append(out, page.Notifications...)
		if cursor = page.NextCursor; cursor == "" {
			return out
		}
	}
	w.t.Fatal("the list did not end")
	return nil
}

// badge is handle's GET /me notifications count.
func (w *notifyWorld) badge(handle string) int {
	w.t.Helper()
	rec := w.call(handle, http.MethodGet, "/api/v1/me", "")
	if rec.Code != http.StatusOK {
		w.t.Fatalf("GET /me as %s: %d %s", handle, rec.Code, rec.Body)
	}
	me := decodeMe(w.t, rec.Body.Bytes())
	if me.User == nil {
		w.t.Fatalf("GET /me as %s answered no user", handle)
	}
	return me.Notifications
}

// sees fails unless handle's list holds exactly want notifications, all
// unread, and the badge counts the same number.
func (w *notifyWorld) sees(handle string, want int, why string) {
	w.t.Helper()
	got := w.list(handle)
	unread := 0
	for _, n := range got {
		if !n.Read {
			unread++
		}
	}
	if len(got) != want || unread != want {
		w.t.Errorf("%s: %s lists %d (%d unread), want %d", why, handle, len(got), unread, want)
	}
	if b := w.badge(handle); b != want {
		w.t.Errorf("%s: %s's badge counts %d, want %d", why, handle, b, want)
	}
}

// TestNotificationsNeverWidenAccess is AC6 and AC10, docs/14's "never widen
// access": a notification is listed and counted only while its post is
// visible to every reader, the viewer may read its forum, and its thread is
// not muted for the viewer, moderators included. Every case checks the list
// and the /me badge together, and the cases that can come back do.
func TestNotificationsNeverWidenAccess(t *testing.T) {
	const secret = "the words of the post itself"
	// mentioned is a thread in forum with alice's post #1 naming each of
	// handles, and its id.
	mentioned := func(w *notifyWorld, forum int64, handles ...string) threadid.ID {
		id := w.threadIn(forum, "Access")
		w.say(id, "alice", secret, 0, handles...)
		return id
	}
	hide := func(w *notifyWorld, id threadid.ID, b threadlog.DeleteBody, actor string) {
		w.record(id, threadlog.Append{Type: threadlog.TypeDelete, Author: uint64(w.users[actor]), Ordinal: 1, Body: &b})
	}
	restore := func(w *notifyWorld, id threadid.ID) {
		w.record(id, threadlog.Append{Type: threadlog.TypeRestore, Author: uint64(w.users["bob"]), Ordinal: 1,
			Body: &threadlog.RestoreBody{}})
	}
	for _, c := range []struct {
		name string
		run  func(t *testing.T, w *notifyWorld)
	}{
		{"(a) deleted by its author, then restored", func(t *testing.T, w *notifyWorld) {
			id := mentioned(w, w.forum, "carol", "bob")
			w.sees("carol", 1, "posted")
			if body := w.call("carol", http.MethodGet, "/api/v1/notifications", "").Body.String(); strings.Contains(body, secret) {
				t.Errorf("the list carries the post's text: %s", body)
			}
			hide(w, id, threadlog.DeleteBody{}, "alice")
			w.sees("carol", 0, "deleted")
			w.sees("bob", 0, "deleted, to a moderator")
			restore(w, id)
			w.sees("carol", 1, "restored")
			w.sees("bob", 1, "restored, to a moderator")
		}},
		{"(b) hidden by a moderator", func(t *testing.T, w *notifyWorld) {
			id := mentioned(w, w.forum, "carol", "bob")
			hide(w, id, threadlog.DeleteBody{ByModerator: true, Reason: "off topic"}, "bob")
			w.sees("carol", 0, "hidden")
			w.sees("bob", 0, "hidden, to the moderator who hid it")
			restore(w, id)
			w.sees("carol", 1, "restored")
		}},
		{"(c) hidden pending review", func(t *testing.T, w *notifyWorld) {
			id := mentioned(w, w.forum, "carol", "bob")
			w.record(id, threadlog.Append{Type: threadlog.TypeDelete, Ordinal: 1,
				Body: &threadlog.DeleteBody{ByModerator: true, Auto: true}})
			w.sees("carol", 0, "auto-hidden")
			w.sees("bob", 0, "auto-hidden, to a moderator")
		}},
		{"(d) hard-redacted", func(t *testing.T, w *notifyWorld) {
			id := mentioned(w, w.forum, "carol", "bob")
			w.sees("carol", 1, "posted")
			w.redactOffline(id, 1)
			w.sees("carol", 0, "redacted")
			w.sees("bob", 0, "redacted, to a moderator")
		}},
		{"(e) moved out by a split", func(t *testing.T, w *notifyWorld) {
			id := mentioned(w, w.forum, "carol")
			w.sql(`UPDATE post_meta SET moved = 1 WHERE thread_id = ? AND ordinal = 1`, int64(id))
			w.sees("carol", 0, "moved")
		}},
		{"(f) thread moved into a forum the viewer cannot read", func(t *testing.T, w *notifyWorld) {
			id := mentioned(w, w.forum, "carol", "bob")
			w.record(id, threadlog.Append{Type: threadlog.TypeThreadMoved, Author: uint64(w.users["bob"]),
				Body: &threadlog.ThreadMovedBody{ForumID: uint64(w.restricted)}})
			w.sees("carol", 0, "moved to a restricted forum")
			w.sees("bob", 0, "moved to a restricted forum, to a moderator by role")
			w.record(id, threadlog.Append{Type: threadlog.TypeThreadMoved, Author: uint64(w.users["bob"]),
				Body: &threadlog.ThreadMovedBody{ForumID: uint64(w.forum)}})
			w.sees("carol", 1, "moved back")
		}},
		{"(g) the group that granted read removed", func(t *testing.T, w *notifyWorld) {
			w.sql(`INSERT INTO groups (id, name, slug, created_at, updated_at) VALUES (100, 'Insiders', 'insiders', 1, 1)`)
			w.sql(`INSERT INTO group_members (group_id, user_id, source, added_at) VALUES (100, ?, 'admin', 1)`, w.users["carol"])
			w.sql(`INSERT INTO permissions (forum_id, group_id, permission, created_at) VALUES (?, 100, 'read', 1)`, w.restricted)
			mentioned(w, w.restricted, "carol")
			w.sees("carol", 1, "a reader of the forum")
			w.sql(`DELETE FROM group_members WHERE group_id = 100 AND user_id = ?`, w.users["carol"])
			w.sees("carol", 0, "out of the group")
		}},
		{"(h) a moderator by group grant", func(t *testing.T, w *notifyWorld) {
			w.sql(`INSERT INTO groups (id, name, slug, created_at, updated_at) VALUES (100, 'Mods', 'mods', 1, 1)`)
			w.sql(`INSERT INTO group_members (group_id, user_id, source, added_at) VALUES (100, ?, 'admin', 1)`, w.users["dave"])
			w.sql(`INSERT INTO permissions (forum_id, group_id, permission, created_at) VALUES
				(?, 100, 'read', 1), (?, 100, 'moderate', 1)`, w.restricted, w.restricted)
			id := mentioned(w, w.restricted, "dave")
			w.sees("dave", 1, "moderating the forum")
			hide(w, id, threadlog.DeleteBody{ByModerator: true}, "dave")
			w.sees("dave", 0, "hidden, to its forum's moderator")
			restore(w, id)
			w.sees("dave", 1, "restored")
			w.sql(`DELETE FROM group_members WHERE group_id = 100`)
			w.sees("dave", 0, "the grant removed")
		}},
		{"(i) a suspended author's posts hidden, then restored", func(t *testing.T, w *notifyWorld) {
			id := mentioned(w, w.forum, "carol")
			w.sql(`UPDATE users SET status = 'suspended' WHERE id = ?`, w.users["alice"])
			hide(w, id, threadlog.DeleteBody{ByModerator: true, Suspension: 7, Reason: "suspended"}, "bob")
			w.sees("carol", 0, "the author suspended")
			w.sql(`UPDATE users SET status = 'active' WHERE id = ?`, w.users["alice"])
			restore(w, id)
			w.sees("carol", 1, "the suspension lifted")
		}},
		{"(j) a quarantined thread", func(t *testing.T, w *notifyWorld) {
			id := mentioned(w, w.forum, "carol", "bob")
			w.svc.Quarantined = func(q threadid.ID) bool { return q == id }
			w.sees("carol", 0, "quarantined")
			w.sees("bob", 0, "quarantined, to a moderator")
			w.svc.Quarantined = nil
			w.sees("carol", 1, "repaired")
		}},
		{"(k) new_application after a demotion", func(t *testing.T, w *notifyWorld) {
			w.sql(`INSERT INTO applications (id, form_id, form_version, answers_json, created_at) VALUES (5, 1, 1, '{}', 4)`)
			w.sql(`INSERT INTO notifications (user_id, kind, application_id, at) VALUES (?, 'new_application', 5, 4)`,
				w.users["bob"])
			w.sees("bob", 1, "a moderator, the application pending")
			w.sql(`UPDATE users SET role = 'member' WHERE id = ?`, w.users["bob"])
			w.sees("bob", 0, "demoted")
		}},
		{"(l) a thread muted after its notification", func(t *testing.T, w *notifyWorld) {
			id := mentioned(w, w.forum, "carol")
			w.sql(`INSERT INTO watches (user_id, thread_id, level, created_at, updated_at) VALUES (?, ?, 'mute', 1, 1)`,
				w.users["carol"], int64(id))
			w.sees("carol", 0, "the thread muted")
			w.sql(`DELETE FROM watches`)
			w.sees("carol", 1, "unmuted")
			w.sql(`INSERT INTO watches (user_id, forum_id, level, created_at, updated_at) VALUES (?, ?, 'mute', 1, 1)`,
				w.users["carol"], w.forum)
			w.sees("carol", 0, "the forum muted")
			w.sql(`DELETE FROM watches`)
			w.sees("carol", 1, "the forum unmuted")
		}},
	} {
		t.Run(c.name, func(t *testing.T) { c.run(t, newNotifyWorld(t)) })
	}
}

// TestNotificationsTrailingProjection is AC6's other half: a Delete the log
// has and post_meta does not yet hides the notification from the list and
// the badge at once, through the thread's index.
func TestNotificationsTrailingProjection(t *testing.T) {
	w := newNotifyWorld(t)
	id := w.threadIn(w.forum, "Trailing")
	w.say(id, "alice", "@carol", 0, "carol")
	w.sees("carol", 1, "posted")
	if err := w.cache.With(id, func(th *threadcache.Thread) error {
		_, err := th.Actor.Append(threadlog.Append{Type: threadlog.TypeDelete, Author: uint64(w.users["alice"]),
			Ordinal: 1, TimeMS: w.clock + 1, Body: &threadlog.DeleteBody{}})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if n := w.count(`SELECT count(*) FROM post_meta WHERE thread_id = ? AND deleted = 0`, int64(id)); n != 1 {
		t.Fatalf("post_meta has %d live posts; the projection was meant to trail", n)
	}
	w.sees("carol", 0, "deleted in the log, not yet in post_meta")
}

// TestNotificationsTrailingMove (Terva review 1781): a ThreadMoved the log
// has and the projection does not leaves the thread's row naming the forum
// it left. The notification is withheld from the list and the badge until
// the projection reaches the move, and then the forum it is now in decides.
// A move back into a readable forum is withheld the same way until
// projected, which costs a reader a moment and never shows a reader of the
// old forum a thread now somewhere else.
func TestNotificationsTrailingMove(t *testing.T) {
	w := newNotifyWorld(t)
	id := w.threadIn(w.forum, "Trailing move")
	w.say(id, "alice", "@carol", 0, "carol")
	w.sees("carol", 1, "posted")
	unprojected := func(forum int64) {
		t.Helper()
		if err := w.cache.With(id, func(th *threadcache.Thread) error {
			_, err := th.Actor.Append(threadlog.Append{Type: threadlog.TypeThreadMoved, Author: uint64(w.users["bob"]),
				TimeMS: w.clock + 1, Body: &threadlog.ThreadMovedBody{ForumID: uint64(forum)}})
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	project := func() {
		t.Helper()
		size, err := w.cache.LogSize(id)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.proj.ProjectThread(t.Context(), id, size); err != nil {
			t.Fatal(err)
		}
	}

	unprojected(w.restricted)
	if n := w.count(`SELECT count(*) FROM threads WHERE id = ? AND forum_id = ?`, int64(id), w.forum); n != 1 {
		t.Fatal("the projection was meant to trail the move")
	}
	w.sees("carol", 0, "moved to a restricted forum in the log, not yet in the projection")
	project()
	w.sees("carol", 0, "moved to a restricted forum, projected")

	unprojected(w.forum)
	w.sees("carol", 0, "moved back in the log, not yet in the projection")
	project()
	w.sees("carol", 1, "moved back, projected")
}

// count runs a counting query.
func (w *world) count(query string, args ...any) int64 {
	w.t.Helper()
	var n int64
	if err := w.db.Read(w.t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		return tx.QueryRowContext(w.t.Context(), query, args...).Scan(&n)
	}); err != nil {
		w.t.Fatal(err)
	}
	return n
}

// TestNotificationList is AC7's list: the golden notifications.member.json,
// the same rows page by page, and a malformed cursor refused.
func TestNotificationList(t *testing.T) {
	w := newNotifyWorld(t)
	w.sql(`INSERT INTO watches (user_id, forum_id, level, created_at, updated_at) VALUES (?, ?, 'watch', 1, 1)`,
		w.users["carol"], w.forum)
	id := w.threadIn(w.forum, "Why we left Slack")
	w.say(id, "alice", "The bill was the smaller half of it.", 0)
	w.say(id, "carol", "And the search.", 1)
	w.say(id, "bob", "> [quote=#2]\n> And the search.\n\nYes.", 0)
	w.say(id, "alice", "@carol, exactly", 2, "carol")
	w.sql(`UPDATE notifications SET read_at = 1 WHERE kind = 'thread_you_watch'`)
	w.sql(`INSERT INTO applications (id, form_id, form_version, answers_json, created_at, status, decided_at, user_id)
		VALUES (5, 1, 1, '{}', 4, 'approved', 4, ?)`, w.users["carol"])
	w.sql(`INSERT INTO notifications (user_id, kind, application_id, actor_id, at) VALUES (?, 'application_decided', 5, ?, 4)`,
		w.users["carol"], w.users["bob"])

	rec := w.call("carol", http.MethodGet, "/api/v1/notifications", "")
	var page dto.NotificationList
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	got, err := json.MarshalIndent(page, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	path := filepath.Join("testdata", "golden", "notifications.member.json")
	if *update {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run with -update to create it)", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("the list differs from %s:\n%s", path, got)
	}

	all := w.list("carol")
	w.svc.NotificationsPage = 1
	paged := w.list("carol")
	ja, _ := json.Marshal(all)
	jb, _ := json.Marshal(paged)
	if a, b := string(ja), string(jb); a != b || len(paged) != 4 {
		t.Errorf("paged one at a time:\n%s\nwant\n%s", b, a)
	}
	for _, bad := range []string{"x", "MA", "LTE", "MDE", "MQ==", "OTk5OTk5OTk5OTk5OTk5OTk5OTk5"} {
		rec := w.call("carol", http.MethodGet, "/api/v1/notifications?cursor="+bad, "")
		if problemCode(t, rec) != "bad-request" {
			t.Errorf("cursor %q: %d %s", bad, rec.Code, rec.Body)
		}
	}
}

// TestMarkNotificationsRead is AC7's write: by ids and by up_to, only the
// viewer's own rows, and the badge follows. A row catch-up wrote for an
// older post, with a higher id and an earlier time, is first in the list
// and cleared by an up_to of the first row's id.
func TestMarkNotificationsRead(t *testing.T) {
	w := newNotifyWorld(t)
	late := w.threadIn(w.forum, "Late")
	// late's post is written first and generated last, as a catch-up would.
	held := w.append(late, threadlog.Append{Type: threadlog.TypePost, Author: uint64(w.users["alice"]),
		Body: &threadlog.PostBody{Source: "@carol, earlier", Mentions: []uint64{uint64(w.users["carol"])}}})
	id := w.threadIn(w.forum, "Marks")
	w.say(id, "alice", "@carol @bob", 0, "carol", "bob")
	w.say(id, "dave", "@carol again", 0, "carol")
	w.say(id, "dave", "@carol and again", 0, "carol")
	w.generate(late, held)
	list := w.list("carol")
	if len(list) != 4 || list[0].Thread.Title != "Late" {
		t.Fatalf("carol's list: %+v", list)
	}
	if list[0].At >= list[1].At {
		t.Fatalf("the catch-up row is not the older post: %+v", list[:2])
	}
	bobs := w.list("bob")

	mark := func(handle, body string) int {
		t.Helper()
		rec := w.call(handle, http.MethodPost, "/api/v1/notifications/read", body)
		if rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("POST %s as %s: %d %s", body, handle, rec.Code, rec.Body)
		}
		var res dto.NotificationsRead
		if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
			t.Fatal(err)
		}
		return res.Marked
	}
	if n := mark("carol", fmt.Sprintf(`{"ids":[%d,%d]}`, list[2].ID, bobs[0].ID)); n != 1 {
		t.Errorf("marked %d by ids, want carol's one", n)
	}
	if b := w.badge("bob"); b != 1 {
		t.Errorf("carol's request marked bob's: his badge is %d", b)
	}
	if b := w.badge("carol"); b != 3 {
		t.Errorf("carol's badge is %d after one was read, want 3", b)
	}
	if n := mark("carol", fmt.Sprintf(`{"ids":[%d]}`, list[2].ID)); n != 0 {
		t.Errorf("marked %d again", n)
	}
	if n := mark("carol", `{"ids":[]}`); n != 0 {
		t.Errorf("an empty list marked %d", n)
	}
	if n := mark("carol", fmt.Sprintf(`{"up_to":%d}`, list[0].ID)); n != 3 {
		t.Errorf("up_to the first row marked %d, want the other 3", n)
	}
	if b := w.badge("carol"); b != 0 {
		t.Errorf("carol's badge is %d with everything read", b)
	}
	for _, n := range w.list("carol") {
		if !n.Read {
			t.Errorf("%+v is unread", n)
		}
	}
	if b := w.badge("bob"); b != 1 {
		t.Errorf("bob's badge is %d after carol marked hers", b)
	}

	for _, bad := range []string{`{}`, `{"ids":[1],"up_to":2}`, `{"ids":null}`, `{"up_to":"3"}`, `{"read":true}`,
		`{"ids":[` + strings.TrimSuffix(strings.Repeat("1,", 201), ",") + `]}`} {
		if rec := w.call("carol", http.MethodPost, "/api/v1/notifications/read", bad); problemCode(t, rec) != "bad-request" {
			t.Errorf("%s: %d %s", bad, rec.Code, rec.Body)
		}
	}
}

// TestBadgeMatchesNotificationList is AC8: /me counts exactly the unread
// rows the list serves, through a muted thread, a hidden post, and a
// new_application whose application was decided, which the list serves as
// read and the badge does not count.
func TestBadgeMatchesNotificationList(t *testing.T) {
	w := newNotifyWorld(t)
	w.sql(`INSERT INTO watches (user_id, forum_id, level, created_at, updated_at) VALUES (?, ?, 'watch', 1, 1), (?, ?, 'watch', 1, 1)`,
		w.users["carol"], w.forum, w.users["bob"], w.forum)
	open := w.threadIn(w.forum, "Open")
	w.say(open, "alice", "@carol @bob", 0, "carol", "bob")
	w.say(open, "dave", "more", 1)
	muted := w.threadIn(w.forum, "Muted")
	w.say(muted, "alice", "@carol", 0, "carol")
	w.sql(`INSERT INTO watches (user_id, thread_id, level, created_at, updated_at) VALUES (?, ?, 'mute', 1, 1)`,
		w.users["carol"], int64(muted))
	hidden := w.threadIn(w.forum, "Hidden")
	w.say(hidden, "dave", "@bob", 0, "bob")
	w.record(hidden, threadlog.Append{Type: threadlog.TypeDelete, Author: uint64(w.users["bob"]), Ordinal: 1,
		Body: &threadlog.DeleteBody{ByModerator: true}})
	w.sql(`INSERT INTO applications (id, form_id, form_version, answers_json, created_at, status, decided_at) VALUES
		(5, 1, 1, '{}', 4, 'denied', 5), (6, 1, 1, '{}', 5, 'pending', NULL)`)
	w.sql(`INSERT INTO notifications (user_id, kind, application_id, at) VALUES (?, 'new_application', 5, 4), (?, 'new_application', 6, 5)`,
		w.users["bob"], w.users["bob"])

	for _, h := range []string{"alice", "bob", "carol", "dave"} {
		unread, decided := 0, 0
		for _, n := range w.list(h) {
			if !n.Read {
				unread++
			}
			if n.Kind == domain.NotifyNewApplication && n.Read {
				decided++
			}
		}
		if b := w.badge(h); b != unread {
			t.Errorf("%s: the badge counts %d, the list serves %d unread", h, b, unread)
		}
		if h == "bob" && decided != 1 {
			t.Errorf("bob's list serves %d decided applications as read, want 1", decided)
		}
	}
	// carol: the mention and the next post in Open, not the muted thread.
	if b := w.badge("carol"); b != 2 {
		t.Errorf("carol counts %d, want 2", b)
	}
	// bob: the mention and the next post in Open, Muted's post, which he
	// did not mute, and the pending application; not the hidden post.
	if b := w.badge("bob"); b != 4 {
		t.Errorf("bob counts %d, want 4", b)
	}
}

// TestNotificationsRefusals is AC7's refusals: both routes refuse anyone
// but a member, anonymous unauthenticated and an applicant forbidden, and
// neither answer may be cached.
func TestNotificationsRefusals(t *testing.T) {
	w := newNotifyWorld(t)
	w.sql(`INSERT INTO identities (id, provider_key, subject, issuer, created_at, updated_at)
		VALUES (1, 'github', 'applicant', 'https://github.com', 1, 1)`)
	pre := sessiontest.PreUser(t, w.db, 1, time.Now())
	w.sql(`UPDATE users SET status = 'suspended' WHERE id = ?`, w.users["dave"])
	for _, c := range []struct{ who, want string }{
		{"", "unauthenticated"}, {pre, "forbidden"}, {"dave", "unauthenticated"},
	} {
		for _, r := range []struct{ method, body string }{
			{http.MethodGet, ""}, {http.MethodPost, `{"up_to":1}`},
		} {
			path := "/api/v1/notifications"
			if r.method == http.MethodPost {
				path += "/read"
			}
			rec := w.call(c.who, r.method, path, r.body)
			if problemCode(t, rec) != c.want || rec.Header().Get("Cache-Control") != "no-store" {
				t.Errorf("%s %s as %q: %d, Cache-Control %q: %s", r.method, path, c.who, rec.Code,
					rec.Header().Get("Cache-Control"), rec.Body)
			}
		}
	}
}
