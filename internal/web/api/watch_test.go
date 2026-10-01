package api

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/auth/session"
	"github.com/terva-sh/ketju/internal/auth/session/sessiontest"
	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/web"
)

// Tests for watches and mutes (TKT-01M29CRJ29, docs/04 "watches", docs/07):
// POST /threads/{id}/watch and POST /forums/{slug}/watch, the levels in the
// thread page's viewer block and on the thread list, and mute in every
// unread count.

func watchPath(id threadid.ID) string   { return "/api/v1/threads/" + id.String() + "/watch" }
func forumWatchPath(slug string) string { return "/api/v1/forums/" + slug + "/watch" }

func levelBody(level string) string { return `{"level":"` + level + `"}` }

// watchRows is how many watches rows who has.
func (w *world) watchRows(who string) int {
	w.t.Helper()
	var n int
	if err := w.db.Read(w.t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		return tx.QueryRowContext(w.t.Context(), `SELECT count(*) FROM watches WHERE user_id = ?`, w.users[who]).Scan(&n)
	}); err != nil {
		w.t.Fatal(err)
	}
	return n
}

// fieldCode is the one field error of a validation-failed answer, as
// "field code".
func fieldCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var p struct {
		Type   string `json:"type"`
		Errors []struct {
			Field, Code string
		} `json:"errors"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil || rec.Code != http.StatusUnprocessableEntity ||
		p.Type != "validation-failed" || len(p.Errors) != 1 {
		t.Fatalf("status %d, not validation-failed with one field error: %s", rec.Code, rec.Body)
	}
	return p.Errors[0].Field + " " + p.Errors[0].Code
}

// TestWatchRoutes is AC5: each route sets, changes and clears a level and
// answers the levels as they then stand, and refuses as the thread page and
// the thread list do. A thread or forum hidden from the viewer is not-found
// whatever the body says, so neither route tells anyone it exists. Every
// refusal writes nothing.
func TestWatchRoutes(t *testing.T) {
	w := newWorld(t)
	id, _ := w.seed()
	h := w.chain()

	// Set, change and clear, on the thread and on the forum. Each answer is
	// what the thread page then says.
	steps := []struct {
		path, level string
		want        dto.ThreadWatch // ForumWatch unused for the forum route
		rows        int
	}{
		{watchPath(id), "watch", dto.ThreadWatch{Watch: "watch"}, 1},
		{watchPath(id), "mute", dto.ThreadWatch{Watch: "mute"}, 1},
		{forumWatchPath("general"), "watch", dto.ThreadWatch{Watch: "watch"}, 2},
		{watchPath(id), "mute", dto.ThreadWatch{Watch: "mute", ForumWatch: "watch"}, 2},
		{forumWatchPath("general"), "mute", dto.ThreadWatch{Watch: "mute"}, 2},
		{watchPath(id), "", dto.ThreadWatch{Watch: "", ForumWatch: "mute"}, 1},
		{forumWatchPath("general"), "", dto.ThreadWatch{Watch: ""}, 0},
		// Clearing what is not there is not an error.
		{watchPath(id), "", dto.ThreadWatch{}, 0},
	}
	for i, s := range steps {
		rec := send(h, "alice", http.MethodPost, s.path, levelBody(s.level))
		if strings.HasSuffix(s.path, "/general/watch") {
			if got := ok[dto.ForumWatch](t, rec); got.Watch != s.want.Watch {
				t.Errorf("step %d: forum level %q, want %q", i, got.Watch, s.want.Watch)
			}
		} else if got := ok[dto.ThreadWatch](t, rec); got != s.want {
			t.Errorf("step %d: thread levels %+v, want %+v", i, got, s.want)
		}
		if n := w.watchRows("alice"); n != s.rows {
			t.Errorf("step %d: alice has %d watches rows, want %d", i, n, s.rows)
		}
	}
	// The page and the list read what the routes wrote.
	ok[dto.ThreadWatch](t, send(h, "alice", http.MethodPost, watchPath(id), levelBody("watch")))
	ok[dto.ForumWatch](t, send(h, "alice", http.MethodPost, forumWatchPath("general"), levelBody("mute")))
	if v := page(t, w.get(w.viewer("alice"), "/api/v1/threads/"+id.String())).Viewer; v.Watch != "watch" ||
		v.ForumWatch != "mute" {
		t.Errorf("the thread page says watch %q, forum_watch %q", v.Watch, v.ForumWatch)
	}
	if l := decodeAs[dto.ThreadList](t, w.get(w.viewer("alice"), "/api/v1/forums/general/threads")); l.Watch != "mute" {
		t.Errorf("the thread list says watch %q", l.Watch)
	}
	if l := decodeAs[dto.ThreadList](t, w.get(domain.Anonymous(), "/api/v1/forums/general/threads")); l.Watch != "" {
		t.Errorf("the thread list says watch %q to an anonymous viewer", l.Watch)
	}
	// Another member's levels are their own.
	if v := page(t, w.get(w.viewer("carol"), "/api/v1/threads/"+id.String())).Viewer; v.Watch != "" ||
		v.ForumWatch != "watch" {
		t.Errorf("carol's page says watch %q, forum_watch %q", v.Watch, v.ForumWatch)
	}
	before := w.watchRows("alice")

	// Who may not keep viewer state, refused before the body is read.
	routes := []string{watchPath(id), forumWatchPath("general")}
	h = w.chain()
	for who, want := range map[string]string{"": "unauthenticated", "pre-user": "forbidden", "inactive": "unauthenticated"} {
		for _, p := range routes {
			if rec := send(h, who, http.MethodPost, p, `{nonsense`); problemCode(t, rec) != want {
				t.Errorf("%q POST %s: %d %s, want %s", who, p, rec.Code, rec.Body, want)
			}
		}
	}
	// What a body can get wrong.
	for _, p := range routes {
		for body, want := range map[string]string{
			`{}`:                      "bad-request",
			`{"level":"watch","x":1}`: "bad-request",
			`not json`:                "bad-request",
		} {
			if rec := send(h, "alice", http.MethodPost, p, body); problemCode(t, rec) != want {
				t.Errorf("POST %s %s: %d %s, want %s", p, body, rec.Code, rec.Body, want)
			}
		}
		for body, want := range map[string]string{
			`{"level":"loud"}`:   "level not_an_option",
			`{"level":"Watch"}`:  "level not_an_option",
			`{"level":1}`:        "level wrong_type",
			`{"level":null}`:     "level wrong_type",
			`{"level":["mute"]}`: "level wrong_type",
		} {
			if got := fieldCode(t, send(h, "alice", http.MethodPost, p, body)); got != want {
				t.Errorf("POST %s %s: %s, want %s", p, body, got, want)
			}
		}
		// A form post is refused before the handler, as for every JSON route.
		r := httptest.NewRequest(http.MethodPost, p, strings.NewReader("level=mute"))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("X-Test-Viewer", "alice")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		if rec.Code != http.StatusUnsupportedMediaType {
			t.Errorf("a form post to %s: %d %s", p, rec.Code, rec.Body)
		}
	}
	for name, c := range map[string]struct{ path, want string }{
		"a thread id that is not base32": {"/api/v1/threads/!!/watch", "bad-request"},
		"a thread that does not exist":   {watchPath(threadid.ID(99)), "not-found"},
		"a forum that does not exist":    {forumWatchPath("nowhere"), "not-found"},
	} {
		if rec := send(h, "alice", http.MethodPost, c.path, levelBody("mute")); problemCode(t, rec) != c.want {
			t.Errorf("%s: %d %s, want %s", name, rec.Code, rec.Body, c.want)
		}
	}
	if n := w.watchRows("alice"); n != before {
		t.Errorf("refused requests changed alice's watches rows from %d to %d", before, n)
	}

	// Archived: watching needs read only.
	w.write(func(q *dbq.Queries) error {
		return q.SetForumArchived(t.Context(), dbq.SetForumArchivedParams{Archived: 1, UpdatedAt: 2, ID: w.forum})
	})
	h = newHandlerFor(w).chain()
	ok[dto.ThreadWatch](t, send(h, "carol", http.MethodPost, watchPath(id), levelBody("mute")))
	ok[dto.ForumWatch](t, send(h, "carol", http.MethodPost, forumWatchPath("general"), levelBody("mute")))
	w.write(func(q *dbq.Queries) error {
		return q.SetForumArchived(t.Context(), dbq.SetForumArchivedParams{Archived: 0, UpdatedAt: 3, ID: w.forum})
	})

	// A thread or forum the viewer may not see is not-found, as on its
	// page and its list, whatever level is asked for, and nothing is
	// written. The answer is the page's own.
	w.hideForum()
	w.revokeForumRead()
	cold := newHandlerFor(w)
	h = cold.chain()
	if rec := cold.get(w.viewer("alice"), "/api/v1/threads/"+id.String()); problemCode(t, rec) != "not-found" {
		t.Fatalf("the hidden thread's page answers %d %s", rec.Code, rec.Body)
	}
	if rec := cold.get(w.viewer("alice"), "/api/v1/forums/general/threads"); problemCode(t, rec) != "not-found" {
		t.Fatalf("the hidden forum's list answers %d %s", rec.Code, rec.Body)
	}
	for _, p := range routes {
		for _, body := range []string{levelBody("watch"), levelBody(""), levelBody("loud")} {
			if rec := send(h, "alice", http.MethodPost, p, body); problemCode(t, rec) != "not-found" {
				t.Errorf("hidden: POST %s %s: %d %s", p, body, rec.Code, rec.Body)
			}
		}
	}
	if n := w.watchRows("alice"); n != before {
		t.Errorf("refusals on a hidden forum changed alice's watches rows from %d to %d", before, n)
	}
}

// muteWorld is newForumWorld with alice's levels set through the routes
// and one unread thread for each case TestMutedThreadHasNoUnreadBadge names.
// bob writes every post, and alice has read nothing, so every thread is
// unread to her but for her levels.
type muteWorld struct {
	*forumWorld
	threads map[string]threadid.ID
}

func newMuteWorld(t *testing.T) *muteWorld {
	t.Helper()
	w := &muteWorld{forumWorld: newForumWorld(t), threads: map[string]threadid.ID{}}
	var seq uint64
	for _, c := range []struct{ name, forum string }{
		// zeta is watched: a plain thread counts, a muted one does not.
		{"zeta-plain", "zeta"}, {"zeta-muted", "zeta"},
		// general is muted: a plain thread does not count, a watched one
		// does.
		{"general-plain", "general"}, {"general-watched", "general"},
		// g-b is general's child, and general's mute does not reach it.
		{"child-plain", "g-b"},
		// alpha has no row: a muted thread does not count.
		{"alpha-plain", "alpha"}, {"alpha-muted", "alpha"},
	} {
		seq++
		id := w.threadIn(c.forum, seq, c.name)
		w.post(id, "bob", "a post", 0)
		w.post(id, "bob", "another", 0)
		w.threads[c.name] = id
	}
	h := w.chain()
	for path, level := range map[string]string{
		forumWatchPath("zeta"):                  "watch",
		forumWatchPath("general"):               "mute",
		watchPath(w.threads["zeta-muted"]):      "mute",
		watchPath(w.threads["general-watched"]): "watch",
		watchPath(w.threads["alpha-muted"]):     "mute",
	} {
		if rec := send(h, "alice", http.MethodPost, path, levelBody(level)); rec.Code != http.StatusOK {
			t.Fatalf("POST %s %s: %d %s", path, level, rec.Code, rec.Body)
		}
	}
	return w
}

// me is GET /api/v1/me as who, signed in through the real session
// middleware: /me reads the account from the session, so a bare viewer in
// the context gets the anonymous shape.
func (w *muteWorld) me(who string) dto.Me {
	w.t.Helper()
	now := time.UnixMilli(w.clock)
	store := &session.Store{DB: w.db, Now: func() time.Time { return now }}
	pass := func(h http.Handler) http.Handler { return h }
	h := web.New(web.Groups{API: w.handler}, web.Options{Session: session.Middleware(store, nil), RateLimit: pass})
	r := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	r.AddCookie(sessiontest.Cookie(sessiontest.SignIn(w.t, w.db, w.users[who], now)))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	me := decodeAs[dto.Me](w.t, rec)
	if me.User == nil || me.User.Handle != who {
		w.t.Fatalf("/me as %s answered %+v", who, me.User)
	}
	return me
}

// unreadByForum is /forums's unread column for who, by slug.
func (w *muteWorld) unreadByForum(who string) map[string]int {
	tree, _ := w.tree(who)
	out := map[string]int{}
	for _, f := range tree.Forums {
		out[f.Slug] = f.Unread
		for _, c := range f.Children {
			out[c.Slug] = c.Unread
		}
	}
	return out
}

// rowUnread is each row's unread on forum's thread list, by thread name.
func (w *muteWorld) rowUnread(who, forum string) map[string]int {
	names := map[string]string{}
	for name, id := range w.threads {
		names[id.String()] = name
	}
	out := map[string]int{}
	for _, r := range w.threadList(who, forum, "", "").Threads {
		out[names[r.ID]] = r.Unread
		if r.LastReadOrdinal != 0 {
			w.t.Errorf("%s: last_read_ordinal %d with nothing read", names[r.ID], r.LastReadOrdinal)
		}
	}
	return out
}

// TestMutedThreadHasNoUnreadBadge is AC2: a muted thread or forum gives no
// unread badge in /me, the forum index or the thread list's row, by the
// precedence of docs/04 "watches". A thread-level watch in a muted forum
// still counts; a thread-level mute in a watched forum does not; a forum's
// mute does not reach its child. Clearing the mute brings the badges back,
// since nothing was deleted.
func TestMutedThreadHasNoUnreadBadge(t *testing.T) {
	w := newMuteWorld(t)

	byForum := w.unreadByForum("alice")
	for slug, want := range map[string]int{"zeta": 1, "general": 1, "g-b": 1, "alpha": 1} {
		if byForum[slug] != want {
			t.Errorf("/forums: %s unread %d, want %d", slug, byForum[slug], want)
		}
	}
	me := w.me("alice")
	// Four threads count; of them, general-watched is a thread-level watch.
	if me.Unread.Threads != 4 || me.Unread.Watching != 1 || me.Unread.Participated != 0 {
		t.Errorf("/me unread %+v, want 4 threads, 1 watching, 0 participated", me.Unread)
	}
	for forum, want := range map[string]map[string]int{
		"zeta":    {"zeta-plain": 2, "zeta-muted": 0},
		"general": {"general-plain": 0, "general-watched": 2},
		"g-b":     {"child-plain": 2},
		"alpha":   {"alpha-plain": 2, "alpha-muted": 0},
	} {
		got := w.rowUnread("alice", forum)
		for name, n := range want {
			if got[name] != n {
				t.Errorf("%s's list: %s unread %d, want %d", forum, name, got[name], n)
			}
		}
	}

	// Another member's view is untouched by alice's levels.
	if byForum := w.unreadByForum("carol"); byForum["general"] != 2 || byForum["zeta"] != 2 {
		t.Errorf("carol's /forums unread %v, want general 2 and zeta 2", byForum)
	}

	// A post alice wrote in a muted forum is not participation either.
	w.post(w.threads["general-plain"], "alice", "mine", 0)
	w.post(w.threads["general-plain"], "bob", "after mine", 0)
	w.post(w.threads["zeta-plain"], "alice", "mine", 0)
	w.post(w.threads["zeta-plain"], "bob", "after mine", 0)
	me = w.me("alice")
	if me.Unread.Threads != 4 || me.Unread.Participated != 1 {
		t.Errorf("/me unread %+v after posting, want 4 threads, 1 participated", me.Unread)
	}

	// Clearing the forum's mute brings general-plain back everywhere.
	ok[dto.ForumWatch](t, send(w.chain(), "alice", http.MethodPost, forumWatchPath("general"), levelBody("")))
	if byForum := w.unreadByForum("alice"); byForum["general"] != 2 {
		t.Errorf("/forums: general unread %d after unmuting, want 2", byForum["general"])
	}
	if got := w.rowUnread("alice", "general"); got["general-plain"] != 4 {
		t.Errorf("general-plain's row unread %d after unmuting, want 4", got["general-plain"])
	}
	me = w.me("alice")
	if me.Unread.Threads != 5 || me.Unread.Participated != 2 {
		t.Errorf("/me unread %+v after unmuting, want 5 threads, 2 participated", me.Unread)
	}
}

// TestReplyDoesNotWatch is AC3: starting a thread and replying to one leave
// the viewer's watch level as it was (docs/06 "Thread list views").
func TestReplyDoesNotWatch(t *testing.T) {
	w := newWorld(t)
	id, _ := w.seed()
	h := w.chain()
	res := created[dto.CreatedThread](t, send(h, "alice", http.MethodPost, threadsPath("general"),
		`{"title":"Started here","source":"First.","tags":[]}`))
	started, err := threadid.Parse(res.Thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	created[dto.CreatedPost](t, send(h, "alice", http.MethodPost, postsPath(id), `{"source":"A reply."}`))
	created[dto.CreatedPost](t, send(h, "alice", http.MethodPost, postsPath(started), `{"source":"And another."}`))
	for _, th := range []threadid.ID{id, started} {
		if v := page(t, w.get(w.viewer("alice"), "/api/v1/threads/"+th.String())).Viewer; v.Watch != "" || v.ForumWatch != "" {
			t.Errorf("%s: watch %q, forum_watch %q after posting", th, v.Watch, v.ForumWatch)
		}
	}
	if n := w.watchRows("alice"); n != 0 {
		t.Errorf("posting wrote %d watches rows", n)
	}
	// A level set before a reply is the level after it.
	ok[dto.ThreadWatch](t, send(h, "alice", http.MethodPost, watchPath(id), levelBody("mute")))
	created[dto.CreatedPost](t, send(h, "alice", http.MethodPost, postsPath(id), `{"source":"Muted, and still replying."}`))
	if v := page(t, w.get(w.viewer("alice"), "/api/v1/threads/"+id.String())).Viewer; v.Watch != "mute" {
		t.Errorf("a reply moved alice's level from mute to %q", v.Watch)
	}
}

// TestWatchStaysOutOfTheResponseCache is AC4's cache half, the response
// cache's key invariant (docs/13 "Invariants"): two members with different
// levels read one cached page of posts, and only their viewer blocks and
// ETags differ. Changing a level is still a hit, and moves the ETag. The
// thread list and the forum index, which carry levels and mute-aware
// counts, are no-store and never touch the cache.
func TestWatchStaysOutOfTheResponseCache(t *testing.T) {
	w := newWorld(t)
	id, _ := w.seed() // carol watches the forum
	path := "/api/v1/threads/" + id.String()
	ok[dto.ThreadWatch](t, send(w.chain(), "alice", http.MethodPost, watchPath(id), levelBody("mute")))

	get := func(who string) (dto.ThreadPage, string, []byte) {
		t.Helper()
		rec := w.get(w.viewer(who), path)
		p := page(t, rec)
		posts, err := json.Marshal(p.Posts)
		if err != nil {
			t.Fatal(err)
		}
		return p, rec.Header().Get("ETag"), posts
	}
	carol, carolTag, carolPosts := get("carol")
	s0 := w.svc.Responses.Stats()
	alice, aliceTag, alicePosts := get("alice")
	s1 := w.svc.Responses.Stats()
	if s1.Hits != s0.Hits+1 || s1.Inserts != s0.Inserts || s1.Entries != s0.Entries {
		t.Fatalf("alice's page was not carol's cached entry: %+v then %+v", s0, s1)
	}
	if !bytes.Equal(carolPosts, alicePosts) {
		t.Errorf("the two members' posts differ")
	}
	if carol.Viewer.Watch != "" || carol.Viewer.ForumWatch != "watch" ||
		alice.Viewer.Watch != "mute" || alice.Viewer.ForumWatch != "" {
		t.Errorf("viewer blocks: carol %+v, alice %+v", carol.Viewer, alice.Viewer)
	}
	if carolTag == aliceTag {
		t.Errorf("two viewer blocks share the ETag %s", carolTag)
	}

	ok[dto.ThreadWatch](t, send(w.chain(), "alice", http.MethodPost, watchPath(id), levelBody("watch")))
	again, againTag, againPosts := get("alice")
	s2 := w.svc.Responses.Stats()
	if s2.Hits != s1.Hits+1 || s2.Inserts != s1.Inserts {
		t.Errorf("a level change was not a cache hit: %+v then %+v", s1, s2)
	}
	if again.Viewer.Watch != "watch" || againTag == aliceTag || !bytes.Equal(againPosts, alicePosts) {
		t.Errorf("after the change: watch %q, ETag %s (was %s)", again.Viewer.Watch, againTag, aliceTag)
	}
	// A revalidation with the old ETag is not a 304.
	if rec := w.get(w.viewer("alice"), path, "If-None-Match", aliceTag); rec.Code != http.StatusOK {
		t.Errorf("the old ETag revalidated to %d", rec.Code)
	}

	before := w.svc.Responses.Stats()
	for _, p := range []string{"/api/v1/forums/general/threads", "/api/v1/forums", "/api/v1/me"} {
		for _, who := range []string{"alice", "carol"} {
			rec := w.get(w.viewer(who), p)
			if rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != "no-store" {
				t.Errorf("GET %s as %s: %d, Cache-Control %q", p, who, rec.Code, rec.Header().Get("Cache-Control"))
			}
		}
	}
	if after := w.svc.Responses.Stats(); after.Hits+after.Misses != before.Hits+before.Misses ||
		after.Inserts != before.Inserts {
		t.Errorf("a list or the index touched the response cache: %+v then %+v", before, after)
	}
}

// TestAgentWatchIsUncharged is AC5's agent half: a watch records the
// agent's own viewer state, as a read position does, so an agent whose
// bucket is empty can still set one (docs/07 "Agents on the API").
func TestAgentWatchIsUncharged(t *testing.T) {
	w := newAgentWorld(t)
	id, _ := w.seed()
	token := w.issue("scribe")
	for i := 0; ; i++ {
		rec := w.bearer(token, http.MethodPost, postsPath(id), reply("spending the bucket"))
		if rec.Code == http.StatusTooManyRequests {
			break
		}
		if rec.Code != http.StatusCreated || i > 100 {
			t.Fatalf("write %d: %d %s", i+1, rec.Code, rec.Body)
		}
	}
	for path, level := range map[string]string{watchPath(id): "mute", forumWatchPath("general"): "watch"} {
		if rec := w.bearer(token, http.MethodPost, path, levelBody(level)); rec.Code != http.StatusOK {
			t.Errorf("POST %s with an empty bucket: %d %s", path, rec.Code, rec.Body)
		}
	}
	if n := w.count(`SELECT count(*) FROM watches WHERE user_id = ?`, w.users["scribe"]); n != 2 {
		t.Errorf("the agent has %d watches rows, want 2", n)
	}
	// The bucket is still empty: the watches spent nothing and refilled
	// nothing.
	if rec := w.bearer(token, http.MethodPost, postsPath(id), reply("still empty")); rec.Code != http.StatusTooManyRequests {
		t.Errorf("a write after the watches: %d", rec.Code)
	}
}
