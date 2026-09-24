package session

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/logging"
	"github.com/terva-sh/ketju/internal/perm"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/web"
)

// clock is a settable time source.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

type world struct {
	t         *testing.T
	db        *db.DB
	clock     *clock
	store     *Store
	member    int64
	suspended int64
	identity  int64
	forum     int64
}

func newWorld(t *testing.T) *world {
	t.Helper()
	d, err := db.Open(t.Context(), db.Options{Path: filepath.Join(t.TempDir(), "ketju.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if err := d.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	w := &world{t: t, db: d, clock: &clock{t: time.UnixMilli(1_757_500_000_000)}}
	w.store = &Store{DB: d, Now: w.clock.now}
	if err := d.Write(t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
		var err error
		user := func(handle, status, role string) int64 {
			if err != nil {
				return 0
			}
			var id int64
			id, err = q.CreateUser(t.Context(), dbq.CreateUserParams{Handle: handle, DisplayName: strings.ToUpper(handle),
				Status: status, Role: role, Kind: "human", CreatedAt: 1, UpdatedAt: 1})
			return id
		}
		w.member = user("alice", "active", "moderator")
		// An admin, suspended: the case where a role left on the viewer
		// would matter most.
		w.suspended = user("mallory", "suspended", "admin")
		if err != nil {
			return err
		}
		// A members-only forum, which only a member (or an admin) may read.
		if w.forum, err = q.CreateForum(t.Context(), dbq.CreateForumParams{Slug: "staff", Name: "Staff",
			Visibility: domain.VisibilityMembers, CreatedAt: 1, UpdatedAt: 1}); err != nil {
			return err
		}
		w.identity, err = q.CreateIdentity(t.Context(), dbq.CreateIdentityParams{ProviderKey: "github",
			Subject: "applicant-1", Issuer: "https://github.com", CreatedAt: 1, UpdatedAt: 1})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return w
}

func (w *world) signIn(userID int64) string {
	w.t.Helper()
	tok, _, err := w.store.insert(w.t.Context(), newSession{Kind: KindUser, UserID: userID, IP: "192.0.2.1", UserAgent: "test"})
	if err != nil {
		w.t.Fatal(err)
	}
	return tok
}

func (w *world) preUser() string {
	w.t.Helper()
	tok, _, err := w.store.insert(w.t.Context(), newSession{Kind: KindPreUser, IdentityID: w.identity, Provider: "github"})
	if err != nil {
		w.t.Fatal(err)
	}
	return tok
}

// server is the real chain from internal/web with this package's middleware
// in the session slot, and one protected route that a member may reach.
func (w *world) server(log *slog.Logger) http.Handler {
	api := http.NewServeMux()
	api.HandleFunc("GET /api/v1/private", func(rw http.ResponseWriter, r *http.Request) {
		v := domain.ViewerFrom(r.Context())
		if !v.Member() {
			rw.WriteHeader(http.StatusUnauthorized)
			return
		}
		fmt.Fprintf(rw, "hello %d", v.UserID)
	})
	pass := func(h http.Handler) http.Handler { return h }
	return web.New(web.Groups{API: api}, web.Options{Logger: log, Session: Middleware(w.store, log),
		RateLimit: pass, CSRF: pass})
}

func get(h http.Handler, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/private", nil)
	if token != "" {
		req.AddCookie(&http.Cookie{Name: CookieName, Value: token})
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// cleared reports whether the response tells the browser to drop the cookie.
func cleared(rec *httptest.ResponseRecorder) bool {
	for _, c := range rec.Result().Cookies() {
		if c.Name == CookieName && c.MaxAge < 0 && c.Value == "" {
			return true
		}
	}
	return false
}

// TestNothingStoredIsACookie is AC1: after a session is created, no column
// of the table holds anything usable as the cookie value, in any encoding.
func TestNothingStoredIsACookie(t *testing.T) {
	w := newWorld(t)
	tokens := []string{w.signIn(w.member), w.preUser()}
	var forbidden [][]byte
	for _, tok := range tokens {
		raw, err := base64.RawURLEncoding.DecodeString(tok)
		if err != nil || len(raw) != tokenBytes {
			t.Fatalf("token %q is not 32 bytes of base64url", tok)
		}
		forbidden = append(forbidden, []byte(tok), raw,
			[]byte(base64.StdEncoding.EncodeToString(raw)), []byte(base64.URLEncoding.EncodeToString(raw)),
			[]byte(fmt.Sprintf("%x", raw)), []byte(fmt.Sprintf("%X", raw)))
	}

	var cells [][]byte
	if err := w.db.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		rows, err := tx.QueryContext(t.Context(), `SELECT * FROM sessions`)
		if err != nil {
			return err
		}
		defer rows.Close()
		cols, _ := rows.Columns()
		for rows.Next() {
			vals := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				return err
			}
			for _, v := range vals {
				switch v := v.(type) {
				case []byte:
					cells = append(cells, v)
				case string:
					cells = append(cells, []byte(v))
				case nil:
				default:
					cells = append(cells, []byte(fmt.Sprint(v)))
				}
			}
		}
		return rows.Err()
	}); err != nil {
		t.Fatal(err)
	}
	if len(cells) < 2*7 {
		t.Fatalf("read only %d cells; the dump is not reading the rows", len(cells))
	}
	for _, c := range cells {
		for _, f := range forbidden {
			if bytes.Contains(c, f) || bytes.Contains(f, c) && len(c) >= 16 {
				t.Fatalf("a stored value %q contains or is contained in a cookie form %q", c, f)
			}
		}
	}
	// And what is stored is what a stored hash should be: sha256 of the raw id.
	raw, _ := base64.RawURLEncoding.DecodeString(tokens[0])
	h := sha256.Sum256(raw)
	if err := w.db.Read(t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
		_, err := q.GetSession(t.Context(), h)
		return err
	}); err != nil {
		t.Fatalf("the row is not keyed by sha256 of the id: %v", err)
	}
}

// TestExpiryBoundaries is AC2: idle and hard expiry both hold, exactly at
// their boundaries, and a session kept busy still ends at the hard limit.
func TestExpiryBoundaries(t *testing.T) {
	for _, c := range []struct {
		name  string
		after time.Duration
		live  bool
	}{
		{"idle, a millisecond before", IdleExpiry - time.Millisecond, true},
		{"idle, at the boundary", IdleExpiry, false},
		{"idle, after", IdleExpiry + time.Millisecond, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := newWorld(t)
			tok := w.signIn(w.member)
			w.clock.add(c.after)
			_, err := w.store.Lookup(t.Context(), tok)
			if live := err == nil; live != c.live {
				t.Fatalf("live=%v (%v), want %v", live, err, c.live)
			}
			if !c.live && !errors.Is(err, ErrNoSession) {
				t.Fatalf("an expired session is %v, want ErrNoSession", err)
			}
		})
	}

	// Touched every day, so never idle: alive to the last millisecond of the
	// hard limit, and not a millisecond past it.
	w := newWorld(t)
	tok := w.signIn(w.member)
	for day := 1; day < 90; day++ {
		w.clock.add(24 * time.Hour)
		s, err := w.store.Lookup(t.Context(), tok)
		if err != nil {
			t.Fatalf("day %d: %v", day, err)
		}
		if wrote, err := w.store.Touch(t.Context(), s); err != nil || !wrote {
			t.Fatalf("day %d: touch wrote=%v err=%v", day, wrote, err)
		}
	}
	w.clock.add(24*time.Hour - time.Millisecond) // 90d - 1ms since creation
	if _, err := w.store.Lookup(t.Context(), tok); err != nil {
		t.Fatalf("at 90d-1ms, a busy session is %v", err)
	}
	w.clock.add(time.Millisecond)
	if _, err := w.store.Lookup(t.Context(), tok); !errors.Is(err, ErrNoSession) {
		t.Fatalf("at 90d, a busy session is %v, want ErrNoSession", err)
	}
}

// TestCutoffAgreesWithTheQuery: Live is the SQL's rule, so the sweeper that
// uses it deletes exactly what Lookup refuses.
func TestCutoffAgreesWithTheQuery(t *testing.T) {
	c := ExpiredBefore(time.UnixMilli(1_000_000_000_000))
	for _, x := range []struct {
		exp, used int64
		live      bool
	}{
		{c.ExpiresAt + 1, c.LastUsedAt + 1, true},
		{c.ExpiresAt, c.LastUsedAt + 1, false},
		{c.ExpiresAt + 1, c.LastUsedAt, false},
	} {
		if c.Live(x.exp, x.used) != x.live {
			t.Errorf("Live(%d, %d) != %v", x.exp, x.used, x.live)
		}
	}
}

// countTouches installs a trigger that counts writes to last_used_at, so a
// test counts UPDATEs rather than inferring them.
func countTouches(t *testing.T, d *db.DB) func() int {
	t.Helper()
	if err := d.Write(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		for _, s := range []string{
			`CREATE TABLE touch_count (n INTEGER NOT NULL)`,
			`INSERT INTO touch_count VALUES (0)`,
			`CREATE TRIGGER count_touch AFTER UPDATE OF last_used_at ON sessions
			 BEGIN UPDATE touch_count SET n = n + 1; END`,
		} {
			if _, err := tx.ExecContext(t.Context(), s); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return func() int {
		var n int
		if err := d.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
			return tx.QueryRowContext(t.Context(), `SELECT n FROM touch_count`).Scan(&n)
		}); err != nil {
			t.Fatal(err)
		}
		return n
	}
}

// TestTouchIsRateLimited is AC3: a hundred requests inside five minutes write
// last_used_at once, and the next write waits until it is due again.
func TestTouchIsRateLimited(t *testing.T) {
	w := newWorld(t)
	touches := countTouches(t, w.db)
	h := w.server(nil)
	tok := w.signIn(w.member)

	// Just created: last_used_at is now, nothing is due.
	for range 10 {
		if rec := get(h, tok); rec.Code != http.StatusOK {
			t.Fatalf("status %d", rec.Code)
		}
	}
	if n := touches(); n != 0 {
		t.Fatalf("a fresh session was touched %d times", n)
	}

	w.clock.add(TouchInterval) // due, exactly
	for i := range 100 {
		get(h, tok)
		if i < 99 {
			w.clock.add(2 * time.Second) // 198s in all: inside the interval
		}
	}
	if n := touches(); n != 1 {
		t.Fatalf("100 requests in under five minutes wrote %d times, want 1", n)
	}
	w.clock.add(TouchInterval - 198*time.Second - time.Millisecond)
	get(h, tok)
	if n := touches(); n != 1 {
		t.Fatalf("a request at 5m-1ms after the write wrote again (%d)", n)
	}
	w.clock.add(time.Millisecond)
	get(h, tok)
	if n := touches(); n != 2 {
		t.Fatalf("a request at 5m after the write: %d writes, want 2", n)
	}

	// Two requests that both read a due value: the statement's own guard
	// makes the second a no-op.
	w.clock.add(TouchInterval)
	s1, _ := w.store.Lookup(t.Context(), tok)
	s2, _ := w.store.Lookup(t.Context(), tok)
	a, err1 := w.store.Touch(t.Context(), s1)
	b, err2 := w.store.Touch(t.Context(), s2)
	if err1 != nil || err2 != nil || !a || b {
		t.Fatalf("racing touches wrote %v and %v (%v, %v), want true then false", a, b, err1, err2)
	}
	if n := touches(); n != 3 {
		t.Fatalf("racing touches: %d writes in all, want 3", n)
	}
}

// TestRevokeSignsOutOnTheNextRequest is AC4, end to end through the server's
// real middleware chain.
func TestRevokeSignsOutOnTheNextRequest(t *testing.T) {
	w := newWorld(t)
	h := w.server(nil)
	tok := w.signIn(w.member)
	other := w.signIn(w.member)

	rec := get(h, tok)
	if rec.Code != http.StatusOK || rec.Body.String() != fmt.Sprintf("hello %d", w.member) || cleared(rec) {
		t.Fatalf("signed in: %d %q", rec.Code, rec.Body)
	}
	if err := w.store.RevokeUser(t.Context(), w.member); err != nil {
		t.Fatal(err)
	}
	for _, tk := range []string{tok, other} {
		rec := get(h, tk)
		if rec.Code != http.StatusUnauthorized || !cleared(rec) {
			t.Fatalf("after RevokeUser: %d, cookie cleared %v", rec.Code, cleared(rec))
		}
	}

	// Signing out one device leaves the others.
	a, b := w.signIn(w.member), w.signIn(w.member)
	if err := w.store.Revoke(t.Context(), a); err != nil {
		t.Fatal(err)
	}
	if get(h, a).Code != http.StatusUnauthorized || get(h, b).Code != http.StatusOK {
		t.Fatal("Revoke ended the wrong sessions")
	}
}

// TestViewerPredicates is AC5, and the rest of the table: a pre-user session
// is never a member, and nor is a suspended user.
func TestViewerPredicates(t *testing.T) {
	w := newWorld(t)
	h := w.server(nil)
	for _, c := range []struct {
		name            string
		token           string
		member, preUser bool
		userID          int64
		role            string
	}{
		{"anonymous", "", false, false, 0, ""},
		{"member", w.signIn(w.member), true, false, w.member, "moderator"},
		// Suspended: no member rights, and no role either, though the row
		// says admin.
		{"suspended admin", w.signIn(w.suspended), false, false, w.suspended, ""},
		{"pre-user", w.preUser(), false, true, 0, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			v := domain.Anonymous()
			if c.token != "" {
				s, err := w.store.Lookup(t.Context(), c.token)
				if err != nil {
					t.Fatal(err)
				}
				v = s.Viewer()
				if v.SessionKey == "" || strings.Contains(c.token, v.SessionKey) ||
					strings.Contains(storedHex(t, c.token), v.SessionKey) {
					t.Fatalf("SessionKey %q is empty, or part of the cookie or the stored hash", v.SessionKey)
				}
			}
			if v.Member() != c.member || v.PreUser != c.preUser || v.UserID != c.userID || v.Role != c.role {
				t.Fatalf("viewer %+v: member=%v preUser=%v", v, v.Member(), v.PreUser)
			}
			d, err := perm.New(w.db).Check(t.Context(), v, w.forum, domain.PermRead)
			if err != nil || (d == perm.Allow) != c.member {
				t.Fatalf("reading a members-only forum: %v, %v; want allowed=%v", d, err, c.member)
			}
			if c.preUser && v.IdentityID != w.identity {
				t.Fatalf("a pre-user viewer carries identity %d, want %d", v.IdentityID, w.identity)
			}
			want := http.StatusUnauthorized
			if c.member {
				want = http.StatusOK
			}
			if rec := get(h, c.token); rec.Code != want {
				t.Fatalf("the protected route answered %d, want %d", rec.Code, want)
			}
		})
	}
}

// TestMalformedCookiesCostNoQuery: a value that cannot be a session id is
// anonymous without touching the database, so a flood of junk cookies costs
// nothing. The database here is closed, so any query would fail loudly.
func TestMalformedCookiesCostNoQuery(t *testing.T) {
	w := newWorld(t)
	h := w.server(nil)
	w.db.Close()
	raw := make([]byte, tokenBytes)
	// The canonical token for raw ends in 'A': its last character carries two
	// bits beyond the 256, and they must be zero. 'B' decodes to the same
	// bytes under a lax decoder, which would make one session answer to four
	// spellings of its cookie.
	canonical := base64.RawURLEncoding.EncodeToString(raw)
	noncanonical := canonical[:len(canonical)-1] + "B"
	for _, tok := range []string{"x", "not base64!", noncanonical, base64.RawURLEncoding.EncodeToString(raw[:31]),
		base64.RawURLEncoding.EncodeToString(append(raw, 0)), base64.StdEncoding.EncodeToString(raw),
		base64.RawURLEncoding.EncodeToString(raw) + "="} {
		rec := get(h, tok)
		if rec.Code != http.StatusUnauthorized || !cleared(rec) {
			t.Errorf("cookie %q: %d, cleared %v; want an anonymous 401 and the cookie cleared", tok, rec.Code, cleared(rec))
		}
	}
	// A well-formed one does query, and a database that cannot answer is a
	// 500, not a quiet sign-out.
	if rec := get(h, base64.RawURLEncoding.EncodeToString(raw)); rec.Code != http.StatusInternalServerError {
		t.Fatalf("a lookup against a dead database answered %d, want 500", rec.Code)
	}
}

// TestTouchDoesNotRevive: a session looked up a moment before it went idle,
// and touched a moment after, stays expired. The touch rechecks expiry at the
// time of the write.
func TestTouchDoesNotRevive(t *testing.T) {
	for _, c := range []struct {
		name  string
		first time.Duration // after creation, to the lookup
		then  time.Duration // from the lookup to the touch
	}{
		{"idle", IdleExpiry - time.Millisecond, 2 * time.Millisecond},
		{"hard", 0, 0}, // set below
	} {
		t.Run(c.name, func(t *testing.T) {
			w := newWorld(t)
			tok := w.signIn(w.member)
			if c.name == "hard" {
				// Touched daily to the last day, so only the hard limit ends it.
				for range 89 {
					w.clock.add(24 * time.Hour)
					s, _ := w.store.Lookup(t.Context(), tok)
					w.store.Touch(t.Context(), s)
				}
				c.first, c.then = 24*time.Hour-time.Millisecond, 2*time.Millisecond
			}
			w.clock.add(c.first)
			s, err := w.store.Lookup(t.Context(), tok)
			if err != nil {
				t.Fatalf("just before the boundary: %v", err)
			}
			w.clock.add(c.then)
			if wrote, err := w.store.Touch(t.Context(), s); wrote || err != nil {
				t.Fatalf("a touch after the boundary wrote=%v err=%v", wrote, err)
			}
			if _, err := w.store.Lookup(t.Context(), tok); !errors.Is(err, ErrNoSession) {
				t.Fatalf("after the touch the session is %v, want ErrNoSession", err)
			}
		})
	}
}

// TestTouchNotDueCostsNoWrite: most requests are not due a touch, and they
// do not open a write transaction to find that out. The database is closed,
// so reaching it would be an error.
func TestTouchNotDueCostsNoWrite(t *testing.T) {
	w := newWorld(t)
	s, err := w.store.Lookup(t.Context(), w.signIn(w.member))
	if err != nil {
		t.Fatal(err)
	}
	w.clock.add(TouchInterval - time.Millisecond)
	w.db.Close()
	if wrote, err := w.store.Touch(t.Context(), s); wrote || err != nil {
		t.Fatalf("a touch not yet due: wrote=%v err=%v, want neither", wrote, err)
	}
}

// TestTheLogNamesTheViewerNotTheToken: the request log carries the user id
// and never the cookie.
func TestTheLogNamesTheViewerNotTheToken(t *testing.T) {
	w := newWorld(t)
	var buf bytes.Buffer
	log := slog.New(logging.Redact(slog.NewJSONHandler(&buf, nil)))
	h := w.server(log)
	tok := w.signIn(w.member)
	get(h, tok)
	if !strings.Contains(buf.String(), fmt.Sprintf(`"viewer":%d`, w.member)) {
		t.Fatalf("the request log does not name the viewer:\n%s", buf.String())
	}
	if strings.Contains(buf.String(), tok) {
		t.Fatal("the request log contains the session token")
	}
}

// TestListForUser: the list names each session by a key that is not the
// cookie, and says which have expired.
func TestListForUser(t *testing.T) {
	w := newWorld(t)
	old := w.signIn(w.member)
	w.clock.add(IdleExpiry)
	fresh := w.signIn(w.member)
	list, err := w.store.ListForUser(t.Context(), w.member)
	if err != nil || len(list) != 2 {
		t.Fatalf("%d sessions, %v", len(list), err)
	}
	if !list[0].Live || list[1].Live || list[0].IP != "192.0.2.1" {
		t.Fatalf("list %+v: want the fresh one live and first", list)
	}
	for _, i := range list {
		if strings.Contains(old, i.Key) || strings.Contains(fresh, i.Key) || i.Key == "" ||
			strings.Contains(storedHex(t, old), i.Key) || strings.Contains(storedHex(t, fresh), i.Key) {
			t.Fatalf("key %q", i.Key)
		}
	}
}

// storedHex is the hash the table stores for token, in hex.
func storedHex(t *testing.T, token string) string {
	t.Helper()
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256(raw)
	return fmt.Sprintf("%x", h)
}
