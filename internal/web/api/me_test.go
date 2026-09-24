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

	"github.com/terva-sh/ketju/internal/auth/session"
	"github.com/terva-sh/ketju/internal/auth/session/sessiontest"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/perm"
	"github.com/terva-sh/ketju/internal/service/users"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
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
	d, err := db.Open(t.Context(), db.Options{Path: filepath.Join(t.TempDir(), "ketju.db"), Trace: st.trace})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if err := d.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	w := &meWorld{t: t, db: d, stmts: st, now: time.UnixMilli(1_757_500_000_000), id: map[string]int64{}}
	w.users = &users.Service{DB: d, Perm: perm.New(d)}
	store := &session.Store{DB: d, Now: func() time.Time { return w.now }}
	shell, err := web.NewShell(mustShell(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	shell.SetPage(ShellPage(w.users))
	pass := func(h http.Handler) http.Handler { return h }
	w.handler = web.New(web.Groups{API: New(Deps{Users: w.users}), SPA: shell},
		web.Options{Session: session.Middleware(store, nil), RateLimit: pass, CSRF: pass})
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
	)
	w.id["alice"], w.id["bob"], w.id["carol"] = 1, 2, 3
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

var anonMe = dto.Me{Settings: dto.Settings{Timezone: "UTC", Locale: "en", PageSize: 25}}

// TestMeShapes is AC1: the documented shape for an anonymous viewer, a member,
// a moderator and a pre-user session, from seeded data. A suspended user gets
// the anonymous shape.
func TestMeShapes(t *testing.T) {
	w := newMeWorld(t)
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
		}},
		{"moderator", sessiontest.SignIn(t, w.db, w.id["bob"], w.now), dto.Me{
			User: &dto.Author{Handle: "bob", DisplayName: "Bob", Role: "moderator", Kind: "agent"},
			// Every thread in the public forum; no read positions, no mark. Not
			// thread 7: a moderator is not an admin.
			Unread:        dto.UnreadCounts{Threads: 6},
			Notifications: 1,
			Settings:      dto.Settings{Timezone: "UTC", Locale: "en", PageSize: 25},
		}},
		{"suspended admin", sessiontest.SignIn(t, w.db, w.id["carol"], w.now), anonMe},
		{"pre-user", sessiontest.PreUser(t, w.db, 1, w.now), dto.Me{PreUser: &dto.PreUser{Provider: "github"},
			Settings: anonMe.Settings}},
	} {
		t.Run(c.name, func(t *testing.T) {
			rec := w.get("/api/v1/me", c.token)
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

// TestMeBudget is AC3, as settled in grooming: the handler runs at most two
// statements, with the session middleware's one lookup outside that budget.
// Cold, the two are the counts and perm's batch; warm, the counts alone. An
// anonymous viewer costs nothing. The shell, which inlines /me, costs the
// same.
func TestMeBudget(t *testing.T) {
	w := newMeWorld(t)
	// The first request this process serves, from a brand-new perm cache.
	alice := sessiontest.SignIn(t, w.db, w.id["alice"], w.now)
	for _, c := range []struct {
		name, path, token string
		want              []string // statement names, in order
	}{
		{"member /me, cold", "/api/v1/me", alice, []string{"LookupSession", "MeCounts", "ForumAccessRows"}},
		{"member /me, warm", "/api/v1/me", alice, []string{"LookupSession", "MeCounts"}},
		{"member shell", "/", alice, []string{"LookupSession", "MeCounts"}},
		{"anonymous /me", "/api/v1/me", "", nil},
		{"anonymous shell", "/", "", nil},
	} {
		var rec *httptest.ResponseRecorder
		ran := w.stmts.during(func() { rec = w.get(c.path, c.token) })
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
		// The budget proper: everything after the middleware's lookup.
		if handler := len(names) - count(names, "LookupSession"); handler > 2 {
			t.Errorf("%s: the handler ran %d statements, budget 2", c.name, handler)
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

func count(xs []string, x string) int {
	n := 0
	for _, v := range xs {
		if v == x {
			n++
		}
	}
	return n
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
	if strings.Join(names, " ") != "LookupSession TouchSession MeCounts" {
		t.Fatalf("with a touch due: %v", names)
	}
	if ran := w.stmts.during(func() { w.get("/api/v1/me", alice) }); len(ran) != 2 {
		t.Fatalf("the request after the touch ran %d statements", len(ran))
	}
}

// TestShellInlinesMe: the shell's bootstrap block is the member's /me,
// escaped so the display name cannot close the element.
func TestShellInlinesMe(t *testing.T) {
	w := newMeWorld(t)
	alice := sessiontest.SignIn(t, w.db, w.id["alice"], w.now)
	page := w.get("/", alice)
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
}
