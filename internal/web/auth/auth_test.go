package auth

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/auth/oidc"
	"github.com/terva-sh/ketju/internal/auth/oidc/oidctest"
	"github.com/terva-sh/ketju/internal/auth/seal"
	"github.com/terva-sh/ketju/internal/auth/session"
	"github.com/terva-sh/ketju/internal/config"
	"github.com/terva-sh/ketju/internal/perm"
	"github.com/terva-sh/ketju/internal/service/identity"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/web"
)

const site = "https://forum.example"

// logBuf is a buffer a logger may write to from any goroutine.
type logBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *logBuf) Write(p []byte) (int, error) { l.mu.Lock(); defer l.mu.Unlock(); return l.b.Write(p) }
func (l *logBuf) String() string              { l.mu.Lock(); defer l.mu.Unlock(); return l.b.String() }

type world struct {
	t       *testing.T
	db      *db.DB
	store   *session.Store
	idp     *oidctest.IdP
	sealer  *seal.Sealer
	srv     http.Handler
	handler *Handler
	log     *logBuf
	now     time.Time
}

// newWorld is a server with one provider, "fake", backed by the in-process
// IdP, through the real middleware chain. configure adjusts the provider.
func newWorld(t *testing.T, configure ...func(*config.Provider)) *world {
	t.Helper()
	d, err := db.Open(t.Context(), db.Options{Path: filepath.Join(t.TempDir(), "ketju.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if err := d.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	w := &world{t: t, db: d, idp: oidctest.New(t), log: &logBuf{}, now: time.Now()}
	log := slog.New(slog.NewTextHandler(w.log, nil))
	w.store = &session.Store{DB: d}
	pc := w.idp.Provider("fake")
	for _, c := range configure {
		c(&pc)
	}
	reg, err := oidc.New(t.Context(), &config.Config{SiteURL: site, OIDC: config.OIDC{Providers: []config.Provider{pc}}},
		oidc.Options{HTTPClient: w.idp.Client(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reg.Close() })
	w.sealer, err = seal.New(bytes.Repeat([]byte{7}, seal.KeySize))
	if err != nil {
		t.Fatal(err)
	}
	w.sealer.Now = func() time.Time { return w.now }
	w.handler = &Handler{Providers: reg, Sealer: w.sealer, Sessions: w.store, HTTPClient: w.idp.Client(), Logger: log,
		Login: &identity.Service{DB: d, Sessions: w.store, Logger: log, Perm: perm.New(d),
			Policy: identity.PolicyFrom([]config.Provider{pc})}}
	w.srv = web.New(web.Groups{Auth: New(w.handler)}, web.Options{Logger: log, SiteURL: site,
		Session: session.Middleware(w.store, log), RateLimit: func(h http.Handler) http.Handler { return h }})
	return w
}

// user makes a user of this status with an identity at the fake provider
// for subject, and returns the user's id.
func (w *world) user(handle, status, subject string) int64 {
	w.t.Helper()
	var id int64
	if err := w.db.Write(w.t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
		var err error
		id, err = q.CreateUser(w.t.Context(), dbq.CreateUserParams{Handle: handle, DisplayName: handle,
			Status: status, Role: "member", Kind: "human", CreatedAt: 1, UpdatedAt: 1})
		if err != nil {
			return err
		}
		_, err = q.CreateIdentity(w.t.Context(), dbq.CreateIdentityParams{UserID: sql.NullInt64{Int64: id, Valid: true},
			ProviderKey: "fake", Subject: subject, Issuer: w.idp.URL, CreatedAt: 1, UpdatedAt: 1})
		return err
	}); err != nil {
		w.t.Fatal(err)
	}
	return id
}

func (w *world) sessions() int {
	w.t.Helper()
	var n int
	if err := w.db.Read(w.t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		return tx.QueryRowContext(w.t.Context(), "SELECT count(*) FROM sessions").Scan(&n)
	}); err != nil {
		w.t.Fatal(err)
	}
	return n
}

// do sends a request through the whole chain as a browser on this site
// would, with cookies.
func (w *world) do(method, target string, cookies ...*http.Cookie) *http.Response {
	w.t.Helper()
	r := httptest.NewRequest(method, site+target, nil)
	r.RemoteAddr = "192.0.2.9:4000"
	r.Header.Set("User-Agent", "test-browser")
	if method != http.MethodGet {
		r.Header.Set("Origin", site)
		r.Header.Set("Sec-Fetch-Site", "same-origin")
	}
	for _, c := range cookies {
		if c != nil {
			r.AddCookie(&http.Cookie{Name: c.Name, Value: c.Value})
		}
	}
	rec := httptest.NewRecorder()
	w.srv.ServeHTTP(rec, r)
	return rec.Result()
}

func cookie(resp *http.Response, name string) *http.Cookie {
	var last *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == name {
			last = c
		}
	}
	return last
}

// begin starts a sign-in and returns the flow cookie and where the IdP was
// asked to send the browser.
func (w *world) begin(returnTo string) (*http.Cookie, string) {
	w.t.Helper()
	target := "/auth/fake/start"
	if returnTo != "" {
		target += "?return_to=" + url.QueryEscape(returnTo)
	}
	resp := w.do("GET", target)
	if resp.StatusCode != http.StatusFound {
		w.t.Fatalf("start answered %d", resp.StatusCode)
	}
	flow := cookie(resp, FlowCookie)
	if flow == nil {
		w.t.Fatal("start set no flow cookie")
	}
	return flow, resp.Header.Get("Location")
}

// back is the IdP's redirect to the callback, as a path on this site.
func (w *world) back(authURL string) string {
	w.t.Helper()
	cb, err := url.Parse(w.idp.Authorize(w.t, authURL))
	if err != nil {
		w.t.Fatal(err)
	}
	if got := cb.Scheme + "://" + cb.Host + cb.Path; got != site+"/auth/fake/callback" {
		w.t.Fatalf("the IdP sent the browser to %s", got)
	}
	return cb.RequestURI()
}

// signIn is the whole flow: start, the IdP, the callback.
func (w *world) signIn(returnTo string) *http.Response {
	w.t.Helper()
	flow, authURL := w.begin(returnTo)
	return w.do("GET", w.back(authURL), flow)
}

func body(resp *http.Response) string {
	b, _ := io.ReadAll(resp.Body)
	return string(b)
}

// refused checks a callback ended in the generic failure, set no session,
// and cleared the flow cookie.
func (w *world) refused(resp *http.Response, want int, title string) {
	w.t.Helper()
	if resp.StatusCode != want {
		w.t.Errorf("status %d, want %d", resp.StatusCode, want)
	}
	if b := body(resp); !strings.Contains(b, "<h1>"+title+"</h1>") {
		w.t.Errorf("the page is not %q:\n%s", title, b)
	}
	if c := cookie(resp, session.CookieName); c != nil && c.Value != "" {
		w.t.Errorf("a refused sign-in set a session cookie")
	}
	if n := w.sessions(); n != 0 {
		w.t.Errorf("a refused sign-in wrote %d sessions", n)
	}
}

func signedInAs(t *testing.T, w *world, resp *http.Response) string {
	t.Helper()
	c := cookie(resp, session.CookieName)
	if c == nil || c.Value == "" {
		t.Fatalf("no session cookie (status %d)", resp.StatusCode)
	}
	if !c.Secure || !c.HttpOnly || c.SameSite != http.SameSiteLaxMode || c.Path != "/" {
		t.Errorf("session cookie attributes %+v", c)
	}
	s, err := w.store.Lookup(context.Background(), c.Value)
	if err != nil || s.User == nil {
		t.Fatalf("the cookie names no user session: %v", err)
	}
	return s.User.Handle
}

// AC3: the whole flow, start to signed in, against the in-process issuer.
func TestTheWholeSignIn(t *testing.T) {
	w := newWorld(t)
	w.user("alice", "active", "user-1")

	flow, authURL := w.begin("/t/abc/a-thread?page=2")
	if !flow.Secure || !flow.HttpOnly || flow.SameSite != http.SameSiteLaxMode || flow.Path != "/" ||
		flow.MaxAge != int(FlowTTL/time.Second) {
		t.Errorf("flow cookie attributes %+v", flow)
	}
	if strings.Contains(flow.Value, "a-thread") {
		t.Error("the flow cookie shows its contents")
	}
	u, _ := url.Parse(authURL)
	q := u.Query()
	if u.Host != strings.TrimPrefix(w.idp.URL, "https://") || q.Get("code_challenge_method") != "S256" ||
		q.Get("state") == "" || q.Get("nonce") == "" || q.Get("redirect_uri") != site+"/auth/fake/callback" {
		t.Errorf("the IdP is asked %s", authURL)
	}

	resp := w.do("GET", w.back(authURL), flow)
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/t/abc/a-thread?page=2" {
		t.Fatalf("callback answered %d to %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	if h := signedInAs(t, w, resp); h != "alice" {
		t.Errorf("signed in as %s", h)
	}
	if c := cookie(resp, FlowCookie); c == nil || c.MaxAge >= 0 {
		t.Errorf("the flow cookie was not cleared: %+v", c)
	}
	var last sql.NullInt64
	w.db.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		return tx.QueryRowContext(t.Context(), "SELECT last_login_at FROM identities WHERE subject = 'user-1'").Scan(&last)
	})
	if !last.Valid {
		t.Error("the sign-in was not recorded on the identity")
	}
	// The code and state were in the callback's query string, which the
	// request log leaves out (docs/02 "Observability").
	cb, _ := url.Parse(w.back(authURL))
	if strings.Contains(w.log.String(), cb.Query().Get("state")) || strings.Contains(w.log.String(), "code=") {
		t.Errorf("the log carries the callback's query:\n%s", w.log.String())
	}
}

// AC1: each way the callback can be lied to is refused, and none reaches a
// session.
func TestATamperedStateIsRefused(t *testing.T) {
	w := newWorld(t)
	w.user("alice", "active", "user-1")
	flow, authURL := w.begin("")
	cb, _ := url.Parse(w.back(authURL))
	q := cb.Query()
	s := []byte(q.Get("state"))
	s[0] ^= 1
	q.Set("state", string(s))
	cb.RawQuery = q.Encode()
	w.refused(w.do("GET", cb.RequestURI(), flow), http.StatusBadRequest, "Sign-in failed")
	if w.idp.Tokens() != 0 {
		t.Error("the code was exchanged although the state did not match")
	}
}

func TestATamperedFlowCookieIsRefused(t *testing.T) {
	w := newWorld(t)
	w.user("alice", "active", "user-1")
	flow, authURL := w.begin("")
	b := []byte(flow.Value)
	b[len(b)-1] ^= 1 // the GCM tag
	if b[len(b)-1] == '-' || b[len(b)-1] == '_' {
		b[len(b)-1] = 'A'
	}
	w.refused(w.do("GET", w.back(authURL), &http.Cookie{Name: FlowCookie, Value: string(b)}), http.StatusBadRequest, "Sign-in failed")
	if w.idp.Tokens() != 0 {
		t.Error("the code was exchanged under a forged cookie")
	}
}

func TestAReplayedNonceIsRefused(t *testing.T) {
	w := newWorld(t)
	w.user("alice", "active", "user-1")
	// An earlier sign-in's nonce, as a token minted for it would carry.
	_, earlier := w.begin("")
	u, _ := url.Parse(earlier)
	w.idp.Nonce = u.Query().Get("nonce")
	w.refused(w.signIn(""), http.StatusBadRequest, "Sign-in failed")
	if w.idp.Tokens() != 1 {
		t.Errorf("%d exchanges, want the one whose token was refused", w.idp.Tokens())
	}
}

func TestAWrongAudienceIsRefused(t *testing.T) {
	w := newWorld(t)
	w.user("alice", "active", "user-1")
	w.idp.Audience = "some-other-client"
	w.refused(w.signIn(""), http.StatusBadRequest, "Sign-in failed")
}

func TestAnExpiredTokenIsRefused(t *testing.T) {
	w := newWorld(t)
	w.user("alice", "active", "user-1")
	w.idp.Expiry = -time.Minute
	w.refused(w.signIn(""), http.StatusBadRequest, "Sign-in failed")
}

// The rest of what the callback must not accept.
func TestTheCallbackRefusesTheRest(t *testing.T) {
	t.Run("no flow cookie", func(t *testing.T) {
		w := newWorld(t)
		_, authURL := w.begin("")
		w.refused(w.do("GET", w.back(authURL)), http.StatusBadRequest, "Sign-in failed")
	})
	t.Run("a flow cookie past its time", func(t *testing.T) {
		w := newWorld(t)
		w.user("alice", "active", "user-1")
		flow, authURL := w.begin("")
		cb := w.back(authURL)
		w.now = w.now.Add(FlowTTL + time.Second)
		w.refused(w.do("GET", cb, flow), http.StatusBadRequest, "Sign-in failed")
	})
	t.Run("a callback replayed with its cookie", func(t *testing.T) {
		w := newWorld(t)
		w.user("alice", "active", "user-1")
		flow, authURL := w.begin("")
		cb := w.back(authURL)
		first := w.do("GET", cb, flow)
		signedInAs(t, w, first)
		n := w.sessions()
		second := w.do("GET", cb, flow)
		if second.StatusCode != http.StatusBadRequest || w.sessions() != n {
			t.Errorf("the replay answered %d and left %d sessions, want 400 and %d", second.StatusCode, w.sessions(), n)
		}
	})
	t.Run("a callback for another provider", func(t *testing.T) {
		w := newWorld(t)
		flow, authURL := w.begin("")
		cb := strings.Replace(w.back(authURL), "/auth/fake/", "/auth/other/", 1)
		w.refused(w.do("GET", cb, flow), http.StatusBadRequest, "Sign-in failed")
	})
	t.Run("the IdP reports an error", func(t *testing.T) {
		w := newWorld(t)
		flow, authURL := w.begin("")
		u, _ := url.Parse(authURL)
		cb := "/auth/fake/callback?error=access_denied&state=" + url.QueryEscape(u.Query().Get("state"))
		w.refused(w.do("GET", cb, flow), http.StatusBadRequest, "Sign-in cancelled")
	})
	t.Run("an unknown provider", func(t *testing.T) {
		w := newWorld(t)
		w.refused(w.do("GET", "/auth/nope/start"), http.StatusNotFound, "Sign-in unavailable")
	})
	t.Run("an email the provider does not allow", func(t *testing.T) {
		w := newWorld(t, func(p *config.Provider) { p.AllowedEmailDomains = []string{"example.com"} })
		w.user("alice", "active", "user-1")
		w.idp.Claims = map[string]any{"email": "alice@elsewhere.example"}
		w.refused(w.signIn(""), http.StatusForbidden, "Sign-in not allowed")
	})
}

// With no provider configured, there is nothing to sign in with.
func TestNoProviders(t *testing.T) {
	sealer, _ := seal.New(make([]byte, seal.KeySize))
	h := New(&Handler{Sealer: sealer})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/auth/github/start", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("start with no providers answered %d", rec.Code)
	}
}

// AC2: only a path on this site is a place to return to, whole flow
// included.
func TestReturnToIsOnlyALocalPath(t *testing.T) {
	for raw, want := range map[string]string{
		"":                       "/",
		"/":                      "/",
		"/t/abc/slug?page=2":     "/t/abc/slug?page=2",
		"/u/alice#posts":         "/u/alice#posts",
		"//evil.example":         "/",
		"//evil.example/x":       "/",
		"/\\evil.example":        "/",
		"/x\\y":                  "/",
		"https://evil.example/":  "/",
		"http:/evil.example":     "/",
		"javascript:alert(1)":    "/",
		"evil.example":           "/",
		"/%2F%2Fevil.example":    "/",
		"/%2f%2fevil.example":    "/",
		"/%5Cevil.example":       "/",
		"/\t/evil.example":       "/",
		"/\n/evil.example":       "/",
		"/x\x7f":                 "/",
		"/%zz":                   "/",
		"https://forum.example/": "/",
	} {
		if got := SafeReturnPath(raw); got != want {
			t.Errorf("SafeReturnPath(%q) = %q, want %q", raw, got, want)
		}
	}

	w := newWorld(t)
	w.user("alice", "active", "user-1")
	resp := w.signIn("https://evil.example/phish")
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/" {
		t.Errorf("an absolute URL to another host sent the browser to %q", resp.Header.Get("Location"))
	}
	signedInAs(t, w, resp)
}

// AC4, with pending for TKT-01M29C7ZZR's AC3: an account that is not active
// is told so and gets no session, whatever its status.
func TestAnInactiveAccountGetsNoSession(t *testing.T) {
	for _, status := range []string{"suspended", "deactivated", "pending"} {
		t.Run(status, func(t *testing.T) {
			w := newWorld(t)
			w.user("u-"+status, status, "user-1")
			resp := w.signIn("/t/abc/x")
			w.refused(resp, http.StatusForbidden, "Account unavailable")
			if loc := resp.Header.Get("Location"); loc != "" {
				t.Errorf("an inactive account was sent on to %q", loc)
			}
		})
	}
}

// The branches whose tickets have not landed say so, and sign nobody in.
func TestTheBranchesStillToCome(t *testing.T) {
	t.Run("a new identity, signed in: linking", func(t *testing.T) {
		w := newWorld(t)
		w.user("bob", "active", "bob-sub")
		w.idp.Subject = "bob-sub"
		bob := cookie(w.signIn(""), session.CookieName)
		w.idp.Subject = "someone-new"
		flow, authURL := w.begin("")
		resp := w.do("GET", w.back(authURL), flow, bob)
		if resp.StatusCode != http.StatusNotImplemented || !strings.Contains(body(resp), "Linking is not available yet") {
			t.Errorf("answered %d", resp.StatusCode)
		}
	})
}

// count runs a count(*) query.
func (w *world) count(query string, args ...any) int {
	w.t.Helper()
	var n int
	if err := w.db.Read(w.t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		return tx.QueryRowContext(w.t.Context(), query, args...).Scan(&n)
	}); err != nil {
		w.t.Fatal(err)
	}
	return n
}

// A sign-in never keeps a session id it did not issue.
func TestSigningInEndsTheSessionItReplaces(t *testing.T) {
	w := newWorld(t)
	w.user("alice", "active", "user-1")
	w.user("bob", "active", "bob-sub")
	w.idp.Subject = "bob-sub"
	bob := cookie(w.signIn(""), session.CookieName)
	w.idp.Subject = "user-1"
	flow, authURL := w.begin("")
	resp := w.do("GET", w.back(authURL), flow, bob)
	if h := signedInAs(t, w, resp); h != "alice" {
		t.Fatalf("signed in as %s", h)
	}
	if _, err := w.store.Lookup(t.Context(), bob.Value); err == nil {
		t.Error("the session the sign-in replaced still works")
	}
}

// failingRevoke is the session store with revocation of one token broken.
type failingRevoke struct {
	*session.Store
	token string
}

func (f failingRevoke) Revoke(ctx context.Context, token string) error {
	if token == f.token {
		return errors.New("disk full")
	}
	return f.Store.Revoke(ctx, token)
}

// A sign-in that cannot end the session it replaces does not complete,
// leaves no new session behind, and is not recorded (reviews 511 and 512).
func TestASignInThatCannotEndTheOldSessionFails(t *testing.T) {
	w := newWorld(t)
	w.user("alice", "active", "user-1")
	w.user("bob", "active", "bob-sub")
	w.idp.Subject = "bob-sub"
	bob := cookie(w.signIn(""), session.CookieName)
	w.handler.Login.(*identity.Service).Sessions = failingRevoke{w.store, bob.Value}

	w.idp.Subject = "user-1"
	flow, authURL := w.begin("")
	resp := w.do("GET", w.back(authURL), flow, bob)
	if resp.StatusCode != http.StatusInternalServerError || resp.Header.Get("Location") != "" {
		t.Errorf("answered %d to %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	if c := cookie(resp, session.CookieName); c != nil && c.Value != "" {
		t.Error("a failed sign-in set a session cookie")
	}
	if n := w.sessions(); n != 1 {
		t.Errorf("%d sessions, want only bob's, which could not be ended", n)
	}
	// Nor is a sign-in that did not happen recorded (review 512).
	var last sql.NullInt64
	w.db.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		return tx.QueryRowContext(t.Context(), "SELECT last_login_at FROM identities WHERE subject = 'user-1'").Scan(&last)
	})
	if last.Valid {
		t.Error("the failed sign-in was recorded on the identity")
	}
}

func TestLogout(t *testing.T) {
	w := newWorld(t)
	w.user("alice", "active", "user-1")
	alice := cookie(w.signIn(""), session.CookieName)

	if resp := w.do("GET", "/auth/logout", alice); resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("GET /auth/logout answered %d", resp.StatusCode)
	}
	r := httptest.NewRequest("POST", site+"/auth/logout", nil)
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	r.AddCookie(alice)
	rec := httptest.NewRecorder()
	w.srv.ServeHTTP(rec, r)
	if rec.Code != http.StatusForbidden {
		t.Errorf("a cross-site sign-out answered %d", rec.Code)
	}
	if _, err := w.store.Lookup(t.Context(), alice.Value); err != nil {
		t.Fatal("a refused sign-out ended the session")
	}

	resp := w.do("POST", "/auth/logout", alice)
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/" {
		t.Errorf("sign-out answered %d to %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	if c := cookie(resp, session.CookieName); c == nil || c.MaxAge >= 0 {
		t.Errorf("sign-out did not clear the cookie: %+v", c)
	}
	if _, err := w.store.Lookup(t.Context(), alice.Value); err == nil {
		t.Error("the session outlived sign-out")
	}
	// Signed out already: still home, nothing to end.
	if resp := w.do("POST", "/auth/logout"); resp.StatusCode != http.StatusSeeOther {
		t.Errorf("a signed-out sign-out answered %d", resp.StatusCode)
	}
}
