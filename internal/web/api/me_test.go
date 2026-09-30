package api

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/audit"
	"github.com/terva-sh/ketju/internal/auth/agentcred"
	"github.com/terva-sh/ketju/internal/auth/session"
	"github.com/terva-sh/ketju/internal/auth/session/sessiontest"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/perm"
	"github.com/terva-sh/ketju/internal/service/applications"
	formsvc "github.com/terva-sh/ketju/internal/service/forms"
	"github.com/terva-sh/ketju/internal/service/forums"
	"github.com/terva-sh/ketju/internal/service/threads"
	"github.com/terva-sh/ketju/internal/service/users"
	"github.com/terva-sh/ketju/internal/settings"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/db/dbtest"
	"github.com/terva-sh/ketju/internal/web"
	"github.com/terva-sh/ketju/internal/web/spa"
)

// statements counts what SQLite runs, less transaction control, through the
// database's trace hook.
type statements struct {
	mu  sync.Mutex
	on  bool
	ran []string
}

func (s *statements) trace(sql string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.on {
		return
	}
	switch strings.ToUpper(strings.Fields(sql + " x")[0]) {
	case "BEGIN", "COMMIT", "ROLLBACK", "END":
		return
	case "PRAGMA":
		// Connection setup: the pool configures a connection the first time
		// it opens one, whichever request happens to be using it.
		return
	}
	s.ran = append(s.ran, strings.Join(strings.Fields(sql), " "))
}

// during returns the statements fn ran.
func (s *statements) during(fn func()) []string {
	s.mu.Lock()
	s.on, s.ran = true, nil
	s.mu.Unlock()
	fn()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.on = false
	return s.ran
}

type meWorld struct {
	t       *testing.T
	db      *db.DB
	stmts   *statements
	users   *users.Service
	handler http.Handler
	now     time.Time
	id      map[string]int64
}

func newMeWorld(t *testing.T) *meWorld {
	t.Helper()
	st := &statements{}
	d := dbtest.Open(t, db.Options{Path: filepath.Join(t.TempDir(), "ketju.db"), Trace: st.trace})
	w := &meWorld{t: t, db: d, stmts: st, now: time.UnixMilli(1_757_500_000_000), id: map[string]int64{}}
	// One evaluator, as serve shares one: the queue's scope warms the read
	// decisions /me makes next (perm.ModeratedForums).
	perms := perm.New(d)
	threadSvc := &threads.Service{DB: d, Perm: perms}
	w.users = &users.Service{DB: d, Perm: perms, QueueCount: threadSvc.QueueCount}
	store := &session.Store{DB: d, Now: func() time.Time { return w.now }}
	shell, err := web.NewShell(mustShell(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	shell.SetPage(ShellPage(w.users))
	creds := &agentcred.Store{DB: d, Now: func() time.Time { return w.now }}
	pass := func(h http.Handler) http.Handler { return h }
	// The bearer middleware in front of the session middleware, as serve
	// composes them: a request with no Authorization header goes straight to
	// the session middleware, so the session cases see what they always did.
	w.handler = web.New(web.Groups{API: New(Deps{Users: w.users, Threads: threadSvc, Forums: &forums.Service{DB: d, Perm: perms}, Forms: &formsvc.Service{DB: d}, Applications: &applications.Service{DB: d}, Perm: perms}), SPA: shell},
		web.Options{Session: agentcred.Middleware(creds, session.Middleware(store, nil), nil), RateLimit: pass, CSRF: pass})
	w.seed()
	return w
}

func mustShell(t *testing.T) string {
	s, err := spa.Shell()
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func (w *meWorld) exec(stmts ...string) {
	w.t.Helper()
	if err := w.db.Write(w.t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		for _, s := range stmts {
			if _, err := tx.ExecContext(w.t.Context(), s); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		w.t.Fatal(err)
	}
}

// seed writes the projections /me reads, and no thread log at all: the
// counts must come from the database alone (AC2).
//
// alice, in the public forum:
//
//	thread 1: she wrote in it, and has read 3 of 5 posts   unread, participated
//	thread 2: she watches it, never opened                 unread, watching
//	thread 3: read to the end                              read
//	thread 4: she wrote in it, but muted it                not counted
//	thread 5: last post before her mark-all-read           read
//	thread 6: never opened                                 unread
//
// and thread 7, unread, in a restricted forum she cannot see.
func (w *meWorld) seed() {
	const mark = 1_757_000_000_000
	w.exec(
		`INSERT INTO users (id, handle, display_name, avatar_blob, status, role, kind, timezone, locale, prefs_json,
		                    mark_all_read_at, created_at, updated_at) VALUES
		 (1, 'alice', 'Alice </script><script>alert(1)</script>', 'b2c3d4', 'active', 'member', 'human',
		  'Europe/Helsinki', 'fi', '{"page_size": 30, "theme": "dark"}', `+itoa(mark)+`, 1, 1),
		 (2, 'bob', 'Bob', NULL, 'active', 'moderator', 'agent', NULL, NULL, '{}', NULL, 1, 1),
		 (3, 'carol', 'Carol', NULL, 'suspended', 'admin', 'human', NULL, NULL, '{}', NULL, 1, 1)`,
		`INSERT INTO identities (id, provider_key, subject, issuer, created_at, updated_at)
		 VALUES (1, 'github', 'applicant', 'https://github.com', 1, 1)`,
		`INSERT INTO forums (id, slug, name, visibility, created_at, updated_at) VALUES
		 (1, 'general', 'General', 'public', 1, 1), (2, 'staff', 'Staff', 'restricted', 1, 1)`,
	)
	thread := func(id, forum, posts int, lastAt int64) string {
		return `INSERT INTO threads (id, forum_id, slug, title, author_id, created_at, post_count, last_post_at,
		        last_post_author_id, last_post_ordinal, log_length_projected)
		        VALUES (` + itoa(int64(id)) + `, ` + itoa(int64(forum)) + `, 't', 'T', 2, 1, ` + itoa(int64(posts)) + `, ` +
			itoa(lastAt) + `, 2, ` + itoa(int64(posts)) + `, 0)`
	}
	later := int64(mark + 1000)
	w.exec(
		thread(1, 1, 5, later), thread(2, 1, 2, later), thread(3, 1, 4, later), thread(4, 1, 3, later),
		thread(5, 1, 6, mark), thread(6, 1, 1, later), thread(7, 2, 2, later),
		// alice's posts: in thread 1 and in the muted thread 4.
		`INSERT INTO post_meta (thread_id, ordinal, author_id, created_at, log_offset) VALUES
		 (1, 2, 1, `+itoa(later-10)+`, 0), (4, 1, 1, `+itoa(later-10)+`, 0)`,
		`INSERT INTO read_positions (user_id, thread_id, last_read_ordinal, last_read_at) VALUES
		 (1, 1, 3, 1), (1, 3, 4, 1)`,
		`INSERT INTO watches (user_id, thread_id, level, created_at, updated_at) VALUES
		 (1, 2, 'watch', 1, 1), (1, 4, 'mute', 1, 1)`,
		`INSERT INTO notifications (user_id, kind, thread_id, ordinal, at, read_at) VALUES
		 (1, 'mention', 1, 4, 1, NULL), (1, 'reply_to_you', 1, 5, 1, NULL), (1, 'quoted', 1, 3, 1, 5),
		 (2, 'mention', 1, 4, 1, NULL)`,
		// The moderator queue (TKT-01M29CRJ4E): dave moderates the general
		// forum through a group; one post there is flagged, twice, and one
		// flag in the staff forum only an admin sees. One application is
		// pending, which staff see and dave does not.
		`INSERT INTO users (id, handle, display_name, status, role, kind, created_at, updated_at)
		 VALUES (4, 'dave', 'Dave', 'active', 'member', 'human', 1, 1)`,
		`INSERT INTO groups (id, name, slug, created_at, updated_at) VALUES (100, 'Forum mods', 'forum-mods', 1, 1)`,
		`INSERT INTO group_members (group_id, user_id, source, added_at) VALUES (100, 4, 'admin', 1)`,
		`INSERT INTO permissions (forum_id, group_id, permission, created_at) VALUES (1, 100, 'moderate', 1)`,
		`INSERT INTO flags (thread_id, ordinal, reporter_id, reason, created_at) VALUES
		 (1, 2, 2, 'spam', 1), (1, 2, 4, 'abuse', 2), (7, 1, 1, 'spam', 3)`,
		`INSERT INTO applications (form_id, form_version, answers_json, created_at) VALUES (1, 1, '{}', 4)`,
		// scribe is an agent that authenticates by its credential, never a
		// session (TKT-01M3QH3X58). It has read thread 1 to the end, watches
		// thread 2, wrote in thread 3, and has one unread mention.
		`INSERT INTO users (id, handle, display_name, avatar_blob, status, role, kind, timezone, locale, prefs_json,
		                    created_at, updated_at)
		 VALUES (5, 'scribe', 'Scribe', 'e5f6a7', 'active', 'member', 'agent', 'America/Toronto', 'fr',
		         '{"page_size": 40}', 1, 1)`,
		`INSERT INTO agents (user_id, created_at) VALUES (5, 1)`,
		`INSERT INTO post_meta (thread_id, ordinal, author_id, created_at, log_offset) VALUES (3, 3, 5, `+itoa(later-10)+`, 0)`,
		`INSERT INTO read_positions (user_id, thread_id, last_read_ordinal, last_read_at) VALUES (5, 1, 5, 1)`,
		`INSERT INTO watches (user_id, thread_id, level, created_at, updated_at) VALUES (5, 2, 'watch', 1, 1)`,
		`INSERT INTO notifications (user_id, kind, thread_id, ordinal, at, read_at) VALUES (5, 'mention', 2, 1, 1, NULL)`,
	)
	w.id["alice"], w.id["bob"], w.id["carol"], w.id["dave"], w.id["scribe"] = 1, 2, 3, 4, 5
}

// issue gives the agent handle a credential, as an admin would, and returns
// its token.
func (w *meWorld) issue(handle string) string {
	w.t.Helper()
	var token string
	if err := w.db.Write(w.t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
		out, err := agentcred.Issue(w.t.Context(), q, w.now, audit.By{}, w.id[handle])
		token = out.Token
		return err
	}); err != nil {
		w.t.Fatal(err)
	}
	return token
}

// bearer is a GET authenticated by an agent's credential.
func (w *meWorld) bearer(path, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	w.handler.ServeHTTP(rec, req)
	return rec
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func (w *meWorld) get(path, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if token != "" {
		req.AddCookie(sessiontest.Cookie(token))
	}
	rec := httptest.NewRecorder()
	w.handler.ServeHTTP(rec, req)
	return rec
}

func decodeMe(t *testing.T, b []byte) dto.Me {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	var me dto.Me
	if err := dec.Decode(&me); err != nil {
		t.Fatalf("decoding %s: %v", b, err)
	}
	return me
}

// siteName is the site_name TestMeShapes sets, so every shape shows one an
// admin chose rather than the default.
const siteName = "The Supper Club"

var anonMe = dto.Me{Settings: dto.Settings{Timezone: "UTC", Locale: "en", PageSize: 25}, SiteName: siteName}

// TestMeShapes is AC1: the documented shape for an anonymous viewer, a member,
// a moderator, a pre-user session and an agent by its credential, from seeded
// data. A suspended user gets the anonymous shape. Every shape carries the
// site name (TKT-01M29CXK2H).
func TestMeShapes(t *testing.T) {
	w := newMeWorld(t)
	if err := settings.Set(t.Context(), w.db, settings.SiteName.Name, json.RawMessage(strconv.Quote(siteName)), 0, w.now); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name  string
		token string
		want  dto.Me
	}{
		{"anonymous", "", anonMe},
		{"member", sessiontest.SignIn(t, w.db, w.id["alice"], w.now), dto.Me{
			User: &dto.Author{Handle: "alice", DisplayName: "Alice </script><script>alert(1)</script>", Avatar: "b2c3d4",
				Role: "member", Kind: "human"},
			// Threads 1, 2 and 6; 1 is hers; 2 is watched. 4 is muted, 3 and 5 are
			// read, 7 is in a forum she cannot see.
			Unread:        dto.UnreadCounts{Threads: 3, Participated: 1, Watching: 1},
			Notifications: 2,
			Settings:      dto.Settings{Timezone: "Europe/Helsinki", Locale: "fi", PageSize: 30},
			SiteName:      siteName,
		}},
		{"moderator", sessiontest.SignIn(t, w.db, w.id["bob"], w.now), dto.Me{
			User: &dto.Author{Handle: "bob", DisplayName: "Bob", Role: "moderator", Kind: "agent"},
			// Every thread in the public forum; no read positions, no mark. Not
			// thread 7: a moderator is not an admin.
			Unread:        dto.UnreadCounts{Threads: 6},
			Notifications: 1,
			// The flagged post in the general forum, as one item, and the
			// application; not the staff forum's flag.
			Queue:    new(2),
			Settings: dto.Settings{Timezone: "UTC", Locale: "en", PageSize: 25},
			SiteName: siteName,
		}},
		// A member granted moderate on one forum through a group has a
		// queue: that forum's items, and no applications (TKT-01M29CRJ4E).
		{"forum moderator", sessiontest.SignIn(t, w.db, w.id["dave"], w.now), dto.Me{
			User:     &dto.Author{Handle: "dave", DisplayName: "Dave", Role: "member", Kind: "human"},
			Unread:   dto.UnreadCounts{Threads: 6},
			Queue:    new(1),
			Settings: dto.Settings{Timezone: "UTC", Locale: "en", PageSize: 25},
			SiteName: siteName,
		}},
		{"suspended admin", sessiontest.SignIn(t, w.db, w.id["carol"], w.now), anonMe},
		{"pre-user", sessiontest.PreUser(t, w.db, 1, w.now), dto.Me{PreUser: &dto.PreUser{Provider: "github"},
			Settings: anonMe.Settings, SiteName: siteName}},
		// An agent by its credential, with no session: the member it is
		// (TKT-01M3QH3X58). Threads 2 to 6 are unread; 1 it read to the end.
		// 3 it wrote in; 2 it watches.
		{"agent by credential", "bearer:" + w.issue("scribe"), dto.Me{
			User:          &dto.Author{Handle: "scribe", DisplayName: "Scribe", Avatar: "e5f6a7", Role: "member", Kind: "agent"},
			Unread:        dto.UnreadCounts{Threads: 5, Participated: 1, Watching: 1},
			Notifications: 1,
			Settings:      dto.Settings{Timezone: "America/Toronto", Locale: "fr", PageSize: 40},
			SiteName:      siteName,
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			var rec *httptest.ResponseRecorder
			if token, ok := strings.CutPrefix(c.token, "bearer:"); ok {
				rec = w.bearer("/api/v1/me", token)
			} else {
				rec = w.get("/api/v1/me", c.token)
			}
			if rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("%d, Cache-Control %q: %s", rec.Code, rec.Header().Get("Cache-Control"), rec.Body)
			}
			got := decodeMe(t, rec.Body.Bytes())
			gb, _ := json.Marshal(got)
			wb, _ := json.Marshal(c.want)
			if !bytes.Equal(gb, wb) {
				t.Fatalf("got  %s\nwant %s", gb, wb)
			}
		})
	}
}

// TestMeBudget is AC3, as settled in grooming: the handler runs at most
// three statements, with the session middleware's one lookup outside that
// budget. Cold, the three are perm's batch, the counts and the site name;
// warm, the counts and the site name. The site name (TKT-01M29CXK2H) is one
// GetSetting, read on every request so an admin's change shows on the next
// page, and it is all an anonymous viewer costs. The shell, which inlines
// /me, costs the same.
//
// The batch is the queue's scope, every forum at once
// (perm.ModeratedForums), which answers the read checks the counts need
// too, so a member who moderates nothing pays nothing for the queue
// (TKT-01M29CRJ4E). A viewer who moderates somewhere pays one count
// statement more: four cold, three warm.
func TestMeBudget(t *testing.T) {
	w := newMeWorld(t)
	// The first request this process serves, from a brand-new perm cache.
	alice := sessiontest.SignIn(t, w.db, w.id["alice"], w.now)
	bob := sessiontest.SignIn(t, w.db, w.id["bob"], w.now)
	dave := sessiontest.SignIn(t, w.db, w.id["dave"], w.now)
	scribe := "bearer:" + w.issue("scribe")
	for _, c := range []struct {
		name, path, token string
		want              []string // statement names, in order
		// middleware is how many of want, from the front, the middleware
		// ran: the session lookup, or an agent's credential statements.
		// Counted per case rather than by name, because the handler's site
		// name read is a GetSetting too.
		middleware int
		budget     int
	}{
		{"member /me, cold", "/api/v1/me", alice, []string{"LookupSession", "ForumAccessRowsAll", "MeCounts", "GetSetting"}, 1, 3},
		{"member /me, warm", "/api/v1/me", alice, []string{"LookupSession", "MeCounts", "GetSetting"}, 1, 2},
		{"member shell", "/", alice, []string{"LookupSession", "MeCounts", "GetSetting"}, 1, 2},
		{"anonymous /me", "/api/v1/me", "", []string{"GetSetting"}, 0, 1},
		{"anonymous shell", "/", "", []string{"GetSetting"}, 0, 1},
		{"moderator /me, cold", "/api/v1/me", bob, []string{"LookupSession", "ForumAccessRowsAll", "QueueCount", "MeCounts", "GetSetting"}, 1, 4},
		{"moderator /me, warm", "/api/v1/me", bob, []string{"LookupSession", "QueueCount", "MeCounts", "GetSetting"}, 1, 3},
		{"forum moderator /me, cold", "/api/v1/me", dave, []string{"LookupSession", "ForumAccessRowsAll", "QueueCount", "MeCounts", "GetSetting"}, 1, 4},
		{"forum moderator shell", "/", dave, []string{"LookupSession", "QueueCount", "MeCounts", "GetSetting"}, 1, 3},
		// An agent by its credential (TKT-01M3QH3X58). The middleware
		// resolves the credential with the site's two bucket defaults, and
		// touches it on first use because last_used_at is null; the clock
		// does not move, so the second request does not touch. After the
		// middleware, the agent costs what a member does.
		{"agent /me, cold", "/api/v1/me", scribe, []string{"LookupAgentCredential", "GetSetting", "GetSetting",
			"TouchAgentCredential", "ForumAccessRowsAll", "MeCounts", "GetSetting"}, 4, 3},
		{"agent /me, warm", "/api/v1/me", scribe, []string{"LookupAgentCredential", "GetSetting", "GetSetting",
			"MeCounts", "GetSetting"}, 3, 2},
	} {
		var rec *httptest.ResponseRecorder
		ran := w.stmts.during(func() {
			if token, ok := strings.CutPrefix(c.token, "bearer:"); ok {
				rec = w.bearer(c.path, token)
			} else {
				rec = w.get(c.path, c.token)
			}
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: %d", c.name, rec.Code)
		}
		var names []string
		for _, s := range ran {
			names = append(names, queryName(s))
		}
		if strings.Join(names, " ") != strings.Join(c.want, " ") {
			t.Errorf("%s ran %v, want %v:\n  %s", c.name, names, c.want, strings.Join(ran, "\n  "))
		}
		// The budget proper: everything after the middleware's lookup, and
		// for an agent after the middleware's credential statements.
		if handler := len(names) - c.middleware; handler > c.budget {
			t.Errorf("%s: the handler ran %d statements, budget %d", c.name, handler, c.budget)
		}
	}
}

// queryName is the sqlc name at the head of a statement, or the statement.
func queryName(stmt string) string {
	if m := regexp.MustCompile(`^-- name: (\w+)`).FindStringSubmatch(stmt); m != nil {
		return m[1]
	}
	return stmt
}

// TestMeBudgetWhenTouchIsDue: once every five minutes a session's
// last_used_at is due to slide (docs/05), and that request also writes it.
// The write is the session middleware's, outside the handler's budget, and
// is pinned here so the handler cannot quietly grow a third statement.
func TestMeBudgetWhenTouchIsDue(t *testing.T) {
	w := newMeWorld(t)
	alice := sessiontest.SignIn(t, w.db, w.id["alice"], w.now)
	w.get("/api/v1/me", alice)
	w.now = w.now.Add(session.TouchInterval)
	var names []string
	for _, s := range w.stmts.during(func() { w.get("/api/v1/me", alice) }) {
		names = append(names, queryName(s))
	}
	if strings.Join(names, " ") != "LookupSession TouchSession MeCounts GetSetting" {
		t.Fatalf("with a touch due: %v", names)
	}
	if ran := w.stmts.during(func() { w.get("/api/v1/me", alice) }); len(ran) != 3 {
		t.Fatalf("the request after the touch ran %d statements", len(ran))
	}
}

// TestMeRefusesADeadCredential is AC3 of TKT-01M3QH3X58: a credential that
// no longer authenticates gets 401 from /me, not the anonymous shape, so an
// agent learns its token is dead rather than reading as a stranger. Each
// agent answers as itself first, so the refusal is the kill's doing. None of
// it, the live answers included, writes a session or sets a cookie.
func TestMeRefusesADeadCredential(t *testing.T) {
	w := newMeWorld(t)
	sessions := func() int {
		var n int
		if err := w.db.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
			return tx.QueryRowContext(t.Context(), `SELECT count(*) FROM sessions`).Scan(&n)
		}); err != nil {
			t.Fatal(err)
		}
		return n
	}
	before := sessions()
	for i, c := range []struct{ name, kill string }{
		{"revoked", `UPDATE agent_credentials SET revoked_at = 1 WHERE user_id = %d`},
		{"suspended", `UPDATE users SET status = 'suspended' WHERE id = %d`},
		{"deactivated", `UPDATE users SET status = 'deactivated' WHERE id = %d`},
	} {
		t.Run(c.name, func(t *testing.T) {
			id := int64(10 + i)
			handle := "agent" + itoa(id)
			w.exec(`INSERT INTO users (id, handle, display_name, status, role, kind, created_at, updated_at)
			        VALUES (`+itoa(id)+`, '`+handle+`', 'Agent', 'active', 'member', 'agent', 1, 1)`,
				`INSERT INTO agents (user_id, created_at) VALUES (`+itoa(id)+`, 1)`)
			w.id[handle] = id
			token := w.issue(handle)
			live := w.bearer("/api/v1/me", token)
			if live.Code != http.StatusOK {
				t.Fatalf("the live credential: %d %s", live.Code, live.Body)
			}
			if me := decodeMe(t, live.Body.Bytes()); me.User == nil || me.User.Handle != handle {
				t.Fatalf("the live credential answered %s", live.Body)
			}
			w.exec(strings.ReplaceAll(c.kill, "%d", itoa(id)))
			rec := w.bearer("/api/v1/me", token)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("%d, want 401: %s", rec.Code, rec.Body)
			}
			if got := rec.Header().Get("WWW-Authenticate"); !strings.HasPrefix(got, "Bearer") {
				t.Errorf("WWW-Authenticate %q", got)
			}
			if strings.Contains(rec.Body.String(), `"settings"`) {
				t.Errorf("the refusal carries a /me shape: %s", rec.Body)
			}
			for _, r := range []*httptest.ResponseRecorder{live, rec} {
				if cookies := r.Header().Values("Set-Cookie"); len(cookies) != 0 {
					t.Errorf("a bearer /me set cookies %q", cookies)
				}
			}
		})
	}
	if after := sessions(); after != before {
		t.Errorf("bearer requests to /me took sessions from %d to %d", before, after)
	}
}

// TestShellInlinesMe: the shell's bootstrap block is the member's /me,
// escaped so the display name cannot close the element, and the page's
// title is the site name /me carries, escaped as text (TKT-01M29CXK2H).
func TestShellInlinesMe(t *testing.T) {
	w := newMeWorld(t)
	alice := sessiontest.SignIn(t, w.db, w.id["alice"], w.now)
	if err := settings.Set(t.Context(), w.db, settings.SiteName.Name, json.RawMessage(`"Tom & Jerry's </title>"`), 0, w.now); err != nil {
		t.Fatal(err)
	}
	page := w.get("/", alice)
	if title := regexp.MustCompile(`<title>(.*?)</title>`).FindStringSubmatch(page.Body.String()); title == nil ||
		title[1] != "Tom &amp; Jerry&#39;s &lt;/title&gt;" {
		t.Errorf("the title is %q, want the site name escaped", title)
	}
	api := w.get("/api/v1/me", alice)
	m := regexp.MustCompile(`(?s)<script type="application/json" id="bootstrap" nonce="[^"]+">(.*?)</script>`).
		FindStringSubmatch(page.Body.String())
	if m == nil {
		t.Fatalf("no bootstrap block in:\n%s", page.Body)
	}
	if strings.Contains(m[1], "</script>") || strings.Contains(m[1], "<script>") {
		t.Fatalf("the display name was not escaped: %s", m[1])
	}
	inlined, served := decodeMe(t, []byte(m[1])), decodeMe(t, api.Body.Bytes())
	ib, _ := json.Marshal(inlined)
	sb, _ := json.Marshal(served)
	if !bytes.Equal(ib, sb) {
		t.Fatalf("the shell inlines %s; /me serves %s", ib, sb)
	}
	if inlined.SiteName != "Tom & Jerry's </title>" {
		t.Errorf("the inlined /me has the site name %q", inlined.SiteName)
	}
}

// TestNewRequiresEveryService: a handler wired without a service refuses to
// be built, rather than answering its first request to that service's route
// with a panic (review 410).
func TestNewRequiresEveryService(t *testing.T) {
	d := newMeWorld(t).db
	u, th, f := &users.Service{DB: d, Perm: perm.New(d)}, &threads.Service{DB: d, Perm: perm.New(d)},
		&forums.Service{DB: d, Perm: perm.New(d)}
	p := perm.New(d)
	fs, as := &formsvc.Service{DB: d}, &applications.Service{DB: d}
	for name, deps := range map[string]Deps{"no users": {Threads: th, Forums: f, Forms: fs, Applications: as, Perm: p},
		"no threads":      {Users: u, Forums: f, Forms: fs, Applications: as, Perm: p},
		"no forums":       {Threads: th, Users: u, Forms: fs, Applications: as, Perm: p},
		"no forms":        {Threads: th, Users: u, Forums: f, Applications: as, Perm: p},
		"no applications": {Threads: th, Users: u, Forums: f, Forms: fs, Perm: p},
		"no perm":         {Threads: th, Users: u, Forums: f, Forms: fs, Applications: as}, "none": {}} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Error("New accepted it")
				}
			}()
			New(deps)
		})
	}
	New(Deps{Threads: th, Users: u, Forums: f, Forms: fs, Applications: as, Perm: p}) // all: no panic
	func() {
		defer func() {
			if recover() == nil {
				t.Error("ShellPage accepted a nil users service")
			}
		}()
		ShellPage(nil)
	}()
}
