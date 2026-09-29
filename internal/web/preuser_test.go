package web_test

import (
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/auth/session"
	"github.com/terva-sh/ketju/internal/auth/session/sessiontest"
	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/perm"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/service/applications"
	formsvc "github.com/terva-sh/ketju/internal/service/forms"
	"github.com/terva-sh/ketju/internal/service/forums"
	"github.com/terva-sh/ketju/internal/service/threads"
	"github.com/terva-sh/ketju/internal/service/users"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/web"
	"github.com/terva-sh/ketju/internal/web/api"
	"github.com/terva-sh/ketju/internal/web/auth"
	"github.com/terva-sh/ketju/internal/web/spa"
)

// The pre-user gate (TKT-01M29CKRG9): an approval-mode applicant's session
// reaches web.PreUserRoutes and nothing else. These tests build the server
// the way serve does, with the real session middleware and the real API and
// auth route tables, and walk every route there is.

// theAllowlist is what a pre-user may reach, written out so that a change
// to web.PreUserRoutes is a change to this test too, in the same diff: "GET
// /api/v1/me, the sign-in routes, POST /auth/logout, /assets, and the /apply
// and /apply/status pages" (AC2), and the application API TKT-01M29CP2KZ
// added.
var theAllowlist = []string{
	"GET /api/v1/me",
	"GET /auth/signin",
	"GET /auth/{provider}/start",
	"GET /auth/{provider}/callback",
	"POST /auth/logout",
	"GET /assets/",
	"GET /apply",
	"GET /apply/status",
	"GET /api/v1/apply/form",
	"POST /api/v1/apply",
	"GET /api/v1/apply/status",
	"POST /api/v1/apply/withdraw",
}

// gateWorld is a database with an applicant's pre-user session and a
// member's session, and the services the API needs.
type gateWorld struct {
	t       *testing.T
	db      *db.DB
	deps    api.Deps
	auth    *auth.Handler
	session func(http.Handler) http.Handler
	// preUser and member are cookie values.
	preUser, member string
}

func newGateWorld(t *testing.T) *gateWorld {
	t.Helper()
	d, err := db.Open(t.Context(), db.Options{Path: filepath.Join(t.TempDir(), "ketju.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if err := d.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	var applicant, member int64
	if err := d.Write(t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
		var err error
		if member, err = q.CreateUser(t.Context(), dbq.CreateUserParams{Handle: "alice", DisplayName: "Alice",
			Status: "active", Role: domain.RoleAdmin, Kind: "human", CreatedAt: 1, UpdatedAt: 1}); err != nil {
			return err
		}
		applicant, err = q.CreateIdentity(t.Context(), dbq.CreateIdentityParams{ProviderKey: "github",
			Subject: "applicant", Issuer: "https://github.com", CreatedAt: 1, UpdatedAt: 1})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	p := perm.New(d)
	store := &session.Store{DB: d}
	return &gateWorld{t: t, db: d,
		deps: api.Deps{Users: &users.Service{DB: d, Perm: p}, Threads: &threads.Service{DB: d, Perm: p},
			Forums: &forums.Service{DB: d, Perm: p}, Forms: &formsvc.Service{DB: d}, Applications: &applications.Service{DB: d}, Perm: p, Logger: log},
		auth:    &auth.Handler{Sessions: store, Logger: log},
		session: session.Middleware(store, log),
		preUser: sessiontest.PreUser(t, d, applicant, now),
		member:  sessiontest.SignIn(t, d, member, now),
	}
}

// probe is one request the walk sends, for the route named by pattern.
type probe struct {
	pattern      string
	method, path string
	// unserved is a path no group serves, which the router answers itself.
	unserved bool
}

var pathParam = regexp.MustCompile(`\{[^}]+\}`)

// concrete is a request path for a ServeMux path pattern: each wildcard
// filled in, a subtree given a file, {$} dropped.
func concrete(path string) string {
	path = strings.TrimSuffix(path, "{$}")
	path = pathParam.ReplaceAllString(path, "1")
	if strings.HasSuffix(path, "/") {
		path += "index.js"
	}
	return path
}

// walk is every route of the server: each route in api.Routes and
// auth.Routes, each group mounted outside them, pages of the client, and
// paths nothing serves. A GET route is also probed with HEAD, which the
// router gives it too.
func (w *gateWorld) walk() []probe {
	var out []probe
	add := func(pattern, method, path string) {
		unserved := strings.HasPrefix(path, "/api/v2/")
		out = append(out, probe{pattern: pattern, method: method, path: path, unserved: unserved})
		if method == http.MethodGet {
			out = append(out, probe{pattern: pattern, method: http.MethodHead, path: path, unserved: unserved})
		}
	}
	for _, rt := range append(api.Routes(w.deps), auth.Routes(w.auth)...) {
		add(rt.Pattern(), rt.Method, concrete(rt.Path))
	}
	for _, g := range []struct{ pattern, path string }{
		{"GET /assets/", "/assets/index.js"},
		{"GET /blobs/", "/blobs/abc"},
		{"POST /blobs/", "/blobs/abc"},
		{"GET /events", "/events"},
		{"GET /apply", "/apply"},
		{"GET /apply/status", "/apply/status"},
		{"POST /apply", "/apply"},
		{"GET /", "/"},
		{"GET /f/{forum}", "/f/general"},
		{"GET /t/{id}/{slug}", "/t/abc/x"},
		{"GET /admin/applications", "/admin/applications"},
		{"GET /apply/", "/apply/"},
		{"GET /apply/status/", "/apply/status/x"},
		{"POST /", "/"},
		{"GET /api/v1/{unknown}", "/api/v1/nope"},
		{"GET /api/v2/", "/api/v2/threads"},
		{"POST /api/v1/{unknown}", "/api/v1/nope"},
	} {
		method, _, _ := strings.Cut(g.pattern, " ")
		add(g.pattern, method, g.path)
	}
	return out
}

// allowed reports whether a probe's route is on the allowlist. HEAD is
// allowed where GET is.
func allowed(p probe) bool {
	pattern := strings.Replace(p.pattern, http.MethodHead+" ", http.MethodGet+" ", 1)
	return slices.Contains(theAllowlist, pattern)
}

// send sends a probe through h with the session cookie token.
func send(h http.Handler, p probe, token string) *httptest.ResponseRecorder {
	var body io.Reader
	req := httptest.NewRequest(p.method, "https://forum.example"+p.path, body)
	if p.method != http.MethodGet && p.method != http.MethodHead {
		req = httptest.NewRequest(p.method, "https://forum.example"+p.path, strings.NewReader("{}"))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Sec-Fetch-Site", "same-origin")
	}
	if token != "" {
		req.AddCookie(sessiontest.Cookie(token))
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// refused checks rec is the gate's refusal of p: 403 forbidden as a problem
// for an API path, 303 to the status page for a page, 403 for anything else.
func refused(t *testing.T, p probe, rec *httptest.ResponseRecorder) {
	t.Helper()
	page := !strings.HasPrefix(p.path, "/api/") && (p.method == http.MethodGet || p.method == http.MethodHead)
	if page {
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != web.ApplyStatusPath {
			t.Errorf("%s %s as a pre-user: %d to %q, want 303 to %s", p.method, p.path, rec.Code,
				rec.Header().Get("Location"), web.ApplyStatusPath)
		}
		return
	}
	if rec.Code != http.StatusForbidden {
		t.Errorf("%s %s as a pre-user: %d, want 403", p.method, p.path, rec.Code)
		return
	}
	if p.method == http.MethodHead {
		return // no body
	}
	var pr problem.Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &pr); err != nil || pr.Type != problem.CodeForbidden {
		t.Errorf("%s %s as a pre-user: body %q, want the forbidden problem", p.method, p.path, rec.Body)
	}
}

// recorder notes which handler each request reached.
type recorder struct {
	mu  sync.Mutex
	hit []string
}

// handler records "name path" for every request it answers.
func (rc *recorder) handler(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rc.mu.Lock()
		rc.hit = append(rc.hit, name+" "+r.URL.Path)
		rc.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}
}

func (rc *recorder) take() []string {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	h := rc.hit
	rc.hit = nil
	return h
}

// recorded is the server built from the same route tables as serve's, with
// each handler replaced by one that records it was reached. It is how the
// walk knows a refusal came from the gate and not from a handler that
// happens to refuse a pre-user too.
func (w *gateWorld) recorded(rc *recorder) http.Handler {
	mount := func(routes []web.Route, rest string) http.Handler {
		mux := http.NewServeMux()
		for _, rt := range routes {
			mux.HandleFunc(rt.Pattern(), rc.handler(rt.Pattern()))
		}
		if rest != "" {
			mux.HandleFunc(rest, rc.handler(rest))
		}
		return mux
	}
	pass := func(h http.Handler) http.Handler { return h }
	return web.New(web.Groups{
		API:    mount(api.Routes(w.deps), "/api/v1/"),
		Auth:   mount(auth.Routes(w.auth), ""),
		Blobs:  rc.handler("blobs"),
		Events: rc.handler("events"),
		Assets: rc.handler("assets"),
		SPA:    rc.handler("spa"),
	}, web.Options{Session: w.session, RateLimit: pass, SiteURL: "https://forum.example"})
}

// TestPreUserRoutesAreExactlyTheAllowlist is AC1 and AC2 of TKT-01M29CKRG9.
// It walks every route in api.Routes and auth.Routes, the groups mounted
// beside them, client pages and paths nothing serves, and shows three
// things. A member reaches each one's handler, so the walk is reaching
// real routes. A pre-user reaches the handler of exactly the routes on the
// allowlist, and is refused on every other before any handler runs: 403
// forbidden for an API path, 303 to /apply/status for a page. And on the
// real server, built as serve builds it, the same holds with the real
// handlers behind the gate.
//
// /healthz is not walked: it leaves the chain before session resolution
// (web.New), so no viewer, pre-user or otherwise, reaches it as anyone.
func TestPreUserRoutesAreExactlyTheAllowlist(t *testing.T) {
	var listed []string
	for _, r := range web.PreUserRoutes {
		listed = append(listed, r.Pattern())
	}
	if !slices.Equal(listed, theAllowlist) {
		t.Fatalf("web.PreUserRoutes is\n  %s\nwant\n  %s\nA route on the list is open to an applicant: "+
			"change this test in the same diff, and say why in the ticket.",
			strings.Join(listed, "\n  "), strings.Join(theAllowlist, "\n  "))
	}

	w := newGateWorld(t)
	t.Run("each entry names a route", func(t *testing.T) { eachEntryNamesARoute(t, w) })
	probes := w.walk()
	if len(probes) < 60 {
		t.Fatalf("the walk has %d requests; the route tables look empty", len(probes))
	}

	t.Run("recorded", func(t *testing.T) {
		rc := &recorder{}
		h := w.recorded(rc)
		for _, p := range probes {
			if rec := send(h, p, w.member); len(rc.take()) != 1 && !p.unserved {
				t.Errorf("%s %s as a member answered %d and reached no handler: the walk is not reaching the route",
					p.method, p.path, rec.Code)
			}
			rec := send(h, p, w.preUser)
			hit := rc.take()
			switch {
			case allowed(p) && len(hit) != 1:
				t.Errorf("%s %s is on the allowlist, and a pre-user answered %d without reaching its handler",
					p.method, p.path, rec.Code)
			case !allowed(p) && len(hit) != 0:
				t.Errorf("%s %s reached %v as a pre-user; only the allowlist may", p.method, p.path, hit)
			case !allowed(p):
				refused(t, p, rec)
			}
		}
	})

	t.Run("real", func(t *testing.T) {
		tmpl, err := spa.Shell()
		if err != nil {
			t.Fatal(err)
		}
		shell, err := web.NewShell(tmpl, nil)
		if err != nil {
			t.Fatal(err)
		}
		shell.SetPage(api.ShellPage(w.deps.Users))
		pass := func(h http.Handler) http.Handler { return h }
		h := web.New(web.Groups{API: api.New(w.deps), Auth: auth.New(w.auth), Assets: spa.Assets(), SPA: shell},
			web.Options{Session: w.session, RateLimit: pass, SiteURL: "https://forum.example"})
		for _, p := range probes {
			if p.pattern == "POST /auth/logout" {
				// Last, below: it ends the session the walk uses.
				continue
			}
			rec := send(h, p, w.preUser)
			if !allowed(p) {
				refused(t, p, rec)
				continue
			}
			if rec.Code == http.StatusForbidden || rec.Header().Get("Location") == web.ApplyStatusPath {
				t.Errorf("%s %s is on the allowlist, and a pre-user was refused: %d to %q", p.method, p.path,
					rec.Code, rec.Header().Get("Location"))
			}
		}
		// The pages the applicant may reach are the shell, telling the
		// client who they are.
		for _, path := range []string{"/apply", web.ApplyStatusPath} {
			rec := send(h, probe{method: http.MethodGet, path: path}, w.preUser)
			if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"pre_user":{"provider":"github"}`) {
				t.Errorf("GET %s as a pre-user: %d, and the bootstrap does not name the applicant", path, rec.Code)
			}
		}
		rec := send(h, probe{method: http.MethodPost, path: "/auth/logout"}, w.preUser)
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/" {
			t.Errorf("POST /auth/logout as a pre-user: %d to %q, want the sign-out's 303 to /", rec.Code,
				rec.Header().Get("Location"))
		}
	})
}

// clientPages are the paths of the client's route table
// (web/client/src/routes.tsx), as ServeMux patterns.
func clientPages(t *testing.T) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "web", "client", "src", "routes.tsx"))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, m := range regexp.MustCompile(`\{ path: '([^']+)'`).FindAllStringSubmatch(string(b), -1) {
		out = append(out, regexp.MustCompile(`:(\w+)`).ReplaceAllString(m[1], "{$1}"))
	}
	if len(out) < 10 {
		t.Fatalf("found %d client routes in routes.tsx; the pattern no longer matches it", len(out))
	}
	return out
}

// eachEntryNamesARoute checks each allowlist entry names something the
// server serves, so a typo cannot open a path nobody meant or close one
// somebody needs: a route in api.Routes or auth.Routes, the /assets/ group,
// or a page of the client's route table, which the server answers with the
// shell.
func eachEntryNamesARoute(t *testing.T, w *gateWorld) {
	registered := map[string]bool{"GET /assets/": true}
	for _, rt := range append(api.Routes(w.deps), auth.Routes(w.auth)...) {
		registered[rt.Pattern()] = true
	}
	pages := clientPages(t)
	for _, r := range web.PreUserRoutes {
		page := r.Method == http.MethodGet && slices.Contains(pages, r.Path)
		if !registered[r.Pattern()] && !page {
			t.Errorf("web.PreUserRoutes lists %q, which is neither a registered route nor a client page", r.Pattern())
		}
	}
}

// TestANewRouteIsClosedToAPreUserByDefault: a route nobody added to the
// allowlist refuses a pre-user without any change to the gate, and serves
// a member. Here it is a throwaway table registered for this test alone.
func TestANewRouteIsClosedToAPreUserByDefault(t *testing.T) {
	w := newGateWorld(t)
	rc := &recorder{}
	brandNew := []web.Route{
		{Method: http.MethodGet, Path: "/api/v1/brand-new/{id}", Handler: rc.handler("get")},
		{Method: http.MethodPost, Path: "/api/v1/brand-new/{id}", Handler: rc.handler("post")},
	}
	mux := http.NewServeMux()
	for _, rt := range brandNew {
		mux.HandleFunc(rt.Pattern(), rt.Handler)
	}
	pass := func(h http.Handler) http.Handler { return h }
	h := web.New(web.Groups{API: mux, SPA: rc.handler("spa")},
		web.Options{Session: w.session, RateLimit: pass, SiteURL: "https://forum.example"})
	for _, p := range []probe{
		{pattern: "GET /api/v1/brand-new/{id}", method: http.MethodGet, path: "/api/v1/brand-new/7"},
		{pattern: "POST /api/v1/brand-new/{id}", method: http.MethodPost, path: "/api/v1/brand-new/7"},
		{pattern: "GET /brand-new-page", method: http.MethodGet, path: "/brand-new-page"},
	} {
		if rec := send(h, p, w.member); rec.Code != http.StatusNoContent || len(rc.take()) != 1 {
			t.Errorf("%s %s as a member: %d", p.method, p.path, rec.Code)
		}
		rec := send(h, p, w.preUser)
		if hit := rc.take(); len(hit) != 0 {
			t.Errorf("%s %s reached %v as a pre-user", p.method, p.path, hit)
		}
		refused(t, p, rec)
	}
}

// The gate matches methods as the router does: a GET route admits HEAD, and
// a method the route does not name is refused. Anyone but a pre-user passes
// untouched. An entry with no method or path stops the server starting.
func TestThePreUserGateMatchesMethodsAsTheRouterDoes(t *testing.T) {
	gate := web.PreUserGate([]web.Route{
		{Method: http.MethodGet, Path: "/apply"},
		{Method: http.MethodPost, Path: "/auth/logout"},
	})
	reached := false
	h := gate(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true }))
	for _, c := range []struct {
		method, path string
		pass         bool
	}{
		{http.MethodGet, "/apply", true},
		{http.MethodHead, "/apply", true},
		{http.MethodPost, "/apply", false},
		{http.MethodOptions, "/apply", false},
		{http.MethodGet, "/apply/", false},
		{http.MethodGet, "/applyx", false},
		{http.MethodGet, "/APPLY", false},
		{http.MethodPost, "/auth/logout", true},
		{http.MethodGet, "/auth/logout", false},
		{http.MethodDelete, "/auth/logout", false},
	} {
		for _, v := range []domain.Viewer{{PreUser: true, IdentityID: 1}, {UserID: 1, Active: true}, {}} {
			reached = false
			r := httptest.NewRequest(c.method, c.path, nil)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, r.WithContext(domain.WithViewer(r.Context(), v)))
			if want := c.pass || !v.PreUser; reached != want {
				t.Errorf("%s %s as %+v: passed=%v, want %v (answered %d)", c.method, c.path, v, reached, want, rec.Code)
			}
		}
	}
	for _, bad := range []web.Route{{Path: "/apply"}, {Method: http.MethodGet}} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("PreUserGate accepted %+v", bad)
				}
			}()
			web.PreUserGate([]web.Route{bad})
		}()
	}
}

// The gate and the router agree on paths spelled to slip between them: dot
// segments, escaped dots and letters, doubled and missing slashes, case. For
// each, whatever the gate lets a pre-user through to, the router sends
// only to an allowlisted handler, or answers with a redirect or a 404 of
// its own. The gate judges a path the router would clean by its clean form,
// and the router answers such a path only with a redirect, which comes back
// through the gate.
func TestTrickyPathsReachOnlyTheAllowlist(t *testing.T) {
	w := newGateWorld(t)
	rc := &recorder{}
	h := w.recorded(rc)
	allowedHit := func(hit string) bool {
		// A route's name is its pattern, which has a space in it.
		i := strings.LastIndex(hit, " ")
		name, path := hit[:i], hit[i+1:]
		switch name {
		case "assets":
			return strings.HasPrefix(path, "/assets/")
		case "spa":
			return path == "/apply" || path == web.ApplyStatusPath
		}
		return slices.Contains(theAllowlist, name)
	}
	reached := 0
	for _, path := range []string{
		"/assets", "/x/../assets/a.js", "/assets/../api/v1/forums", "/assets/%2e%2e/api/v1/forums",
		"/assets/..%2fapi/v1/forums", "/%61ssets/a.js", "/assets//../../api/v1/forums",
		"/api/v1/me/", "/api/v1//me", "/api/v1/me/../forums", "/api/v1/%6de", "/api/v1/me%2f..%2fforums",
		"/apply/../f/general", "//apply", "/apply/.", "/apply/status/..", "/%61pply", "/apply%2fstatus",
		"/auth/x/start/../../register", "/auth/%2e%2e/start", "/auth/register%2fx/start", "/auth/signin/",
		"/API/v1/forums", "/Apply", "/assets/%2e%2e%2fapi/v1/forums",
	} {
		for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPost} {
			req := httptest.NewRequest(method, "https://forum.example"+path, strings.NewReader("{}"))
			req.Header.Set("Content-Type", "application/json")
			req.AddCookie(sessiontest.Cookie(w.preUser))
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			for _, hit := range rc.take() {
				reached++
				if !allowedHit(hit) {
					t.Errorf("%s %s as a pre-user reached %q (answered %d)", method, path, hit, rec.Code)
				}
			}
		}
	}
	// Some of these do reach an allowlisted handler, so the check above
	// was asked something.
	if reached < 5 {
		t.Errorf("only %d requests reached a handler; the recorder is not seeing them", reached)
	}
}
