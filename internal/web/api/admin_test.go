package api

import (
	"database/sql"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/auth/session"
	"github.com/terva-sh/ketju/internal/auth/session/sessiontest"
	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/perm"
	"github.com/terva-sh/ketju/internal/service/forums"
	"github.com/terva-sh/ketju/internal/service/threads"
	"github.com/terva-sh/ketju/internal/service/users"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/web"
)

// adminWorld is the API behind the real session middleware, with one viewer
// of each class the gates tell apart.
type adminWorld struct {
	t       *testing.T
	db      *db.DB
	perm    *perm.Evaluator
	h       *handlers
	mux     *http.ServeMux
	handler http.Handler
	now     time.Time
	tokens  map[string]string
	forum   int64
}

// The admin world's users: id, handle, status, role.
var adminUsers = []struct {
	id                   int64
	handle, status, role string
}{
	{1, "member", "active", "member"},
	{2, "scoped", "active", "member"}, // in the moderators group, granted moderate on one forum
	{3, "moderator", "active", "moderator"},
	{4, "admin", "active", "admin"},
	{5, "suspended", "suspended", "admin"},
}

func newAdminWorld(t *testing.T) *adminWorld {
	t.Helper()
	d, err := db.Open(t.Context(), db.Options{Path: filepath.Join(t.TempDir(), "ketju.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if err := d.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	w := &adminWorld{t: t, db: d, perm: perm.New(d), now: time.UnixMilli(1_757_500_000_000), tokens: map[string]string{}}
	if err := d.Write(t.Context(), func(q *dbq.Queries, tx *sql.Tx) error {
		for _, u := range adminUsers {
			if _, err := tx.ExecContext(t.Context(), `INSERT INTO users (id, handle, display_name, status, role, kind,
				prefs_json, created_at, updated_at) VALUES (?, ?, ?, ?, ?, 'human', '{}', 1, 1)`,
				u.id, u.handle, u.handle, u.status, u.role); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(t.Context(), `INSERT INTO identities (id, provider_key, subject, issuer, created_at,
			updated_at) VALUES (1, 'github', 'applicant', 'https://github.com', 1, 1)`); err != nil {
			return err
		}
		w.forum, err = q.CreateForum(t.Context(), dbq.CreateForumParams{Slug: "general", Name: "General",
			Visibility: domain.VisibilityPublic, CreatedAt: 1, UpdatedAt: 1})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	mods := w.group(domain.GroupModerators)
	if err := w.perm.AddMember(t.Context(), mods, 2, 0); err != nil {
		t.Fatal(err)
	}
	if err := w.perm.Grant(t.Context(), w.forum, mods, domain.PermModerate); err != nil {
		t.Fatal(err)
	}
	for _, u := range adminUsers {
		w.tokens[u.handle] = sessiontest.SignIn(t, d, u.id, w.now)
	}
	w.tokens["pre-user"] = sessiontest.PreUser(t, d, 1, w.now)
	w.tokens["anonymous"] = ""

	w.h, w.mux = build(Deps{Users: &users.Service{DB: d, Perm: w.perm}, Threads: &threads.Service{DB: d, Perm: w.perm},
		Forums: &forums.Service{DB: d, Perm: w.perm}, Perm: w.perm})
	store := &session.Store{DB: d, Now: func() time.Time { return w.now }}
	pass := func(h http.Handler) http.Handler { return h }
	w.handler = web.New(web.Groups{API: w.mux}, web.Options{Session: session.Middleware(store, nil), RateLimit: pass, CSRF: pass})
	return w
}

func (w *adminWorld) group(slug string) int64 {
	w.t.Helper()
	var id int64
	if err := w.db.Read(w.t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
		g, err := q.GetGroupBySlug(w.t.Context(), slug)
		id = g.ID
		return err
	}); err != nil {
		w.t.Fatal(err)
	}
	return id
}

// fixtureRoutes registers one route behind each gate, through adminRoute,
// so the walk below exercises every gate whatever routes the sections have
// registered so far. Each answers 204 once admitted.
func (w *adminWorld) fixtureRoutes() {
	for _, g := range []gate{adminGate, staffGate, moderatesGate} {
		w.h.adminRoute(w.mux, "POST /api/v1/admin/_test/"+g.name+"/{id}", g, func(rw http.ResponseWriter, r *http.Request) {
			rw.WriteHeader(http.StatusNoContent)
		})
	}
}

var pathParam = regexp.MustCompile(`\{[^}]+\}`)

// request sends the route's method to its path, with every path parameter
// filled in, as viewer.
func (w *adminWorld) request(pattern, viewer string) int {
	w.t.Helper()
	method, path, _ := strings.Cut(pattern, " ")
	path = pathParam.ReplaceAllString(strings.TrimSuffix(path, "{$}"), "1")
	req := httptest.NewRequest(method, path, strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	if tok := w.tokens[viewer]; tok != "" {
		req.AddCookie(sessiontest.Cookie(tok))
	}
	rec := httptest.NewRecorder()
	w.handler.ServeHTTP(rec, req)
	return rec.Code
}

// admits is which viewers each gate lets through.
var admits = map[string]map[string]bool{
	"admin":     {"admin": true},
	"staff":     {"admin": true, "moderator": true},
	"moderates": {"admin": true, "moderator": true, "scoped": true},
}

// TestAdminRoutesAreGated is AC1: every route registered under
// /api/v1/admin/ answers 401 to a viewer with no member session, and 403 to
// a pre-user and to any member its gate does not admit. It walks the list
// adminRoute keeps, so a route a section adds later is covered without an
// edit here; the fixture routes make sure every gate is walked today.
func TestAdminRoutesAreGated(t *testing.T) {
	w := newAdminWorld(t)
	w.fixtureRoutes()
	seen := map[string]bool{}
	for _, route := range w.h.adminRoutes {
		seen[route.gate.name] = true
		want, ok := admits[route.gate.name]
		if !ok {
			t.Fatalf("%s has the gate %q, which this test does not know", route.pattern, route.gate.name)
		}
		fixture := strings.Contains(route.pattern, "/_test/")
		for _, viewer := range []string{"anonymous", "suspended", "pre-user", "member", "scoped", "moderator", "admin"} {
			got := w.request(route.pattern, viewer)
			switch {
			case viewer == "anonymous" || viewer == "suspended":
				if got != http.StatusUnauthorized {
					t.Errorf("%s as %s: %d, want 401", route.pattern, viewer, got)
				}
			case !want[viewer]:
				if got != http.StatusForbidden {
					t.Errorf("%s as %s: %d, want 403", route.pattern, viewer, got)
				}
			case fixture:
				// A real route's handler may refuse for reasons of its own
				// once admitted; the fixture's never does.
				if got != http.StatusNoContent {
					t.Errorf("%s as %s: %d, want the handler's 204", route.pattern, viewer, got)
				}
			}
		}
	}
	for g := range admits {
		if !seen[g] {
			t.Errorf("no route has the %s gate; the walk did not exercise it", g)
		}
	}
}

// TestModeratesGate is AC5's server half: a member granted moderate on one
// forum through the moderators group passes moderates and neither staff nor
// admin, and once the grant is revoked fails moderates on the next request.
func TestModeratesGate(t *testing.T) {
	w := newAdminWorld(t)
	w.fixtureRoutes()
	for g, want := range map[string]int{"moderates": http.StatusNoContent, "staff": http.StatusForbidden,
		"admin": http.StatusForbidden} {
		if got := w.request("POST /api/v1/admin/_test/"+g+"/{id}", "scoped"); got != want {
			t.Errorf("the scoped moderator at the %s gate: %d, want %d", g, got, want)
		}
	}
	if err := w.perm.Revoke(t.Context(), w.forum, w.group(domain.GroupModerators), domain.PermModerate); err != nil {
		t.Fatal(err)
	}
	if got := w.request("POST /api/v1/admin/_test/moderates/{id}", "scoped"); got != http.StatusForbidden {
		t.Errorf("after the revoke, the moderates gate answered %d, want 403", got)
	}
}

// TestAdminRouteRefusesAPathOutsideAdmin: adminRoute is for admin routes
// only, so the list the walk reads cannot hold anything else, and a route
// with no method or gate is a wiring mistake that stops the server starting.
func TestAdminRouteRefusesAPathOutsideAdmin(t *testing.T) {
	w := newAdminWorld(t)
	ok := func(http.ResponseWriter, *http.Request) {}
	for _, c := range []struct {
		pattern string
		g       gate
	}{
		{"GET /api/v1/users", adminGate},
		{"/api/v1/admin/x", adminGate},
		{"GET /api/v1/admin/x", gate{name: "none"}},
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("adminRoute accepted %q with gate %q", c.pattern, c.g.name)
				}
			}()
			w.h.adminRoute(http.NewServeMux(), c.pattern, c.g, ok)
		}()
	}
}

// TestAdminPathsGoThroughAdminRoute: nothing in this package registers a
// path under /api/v1/admin/ on the mux directly, where the walk above would
// not see it and no gate would run.
func TestAdminPathsGoThroughAdminRoute(t *testing.T) {
	fset := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, e.Name(), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		checked++
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || (sel.Sel.Name != "HandleFunc" && sel.Sel.Name != "Handle") {
				return true
			}
			if lit, ok := call.Args[0].(*ast.BasicLit); ok && strings.Contains(lit.Value, "/api/v1/admin") {
				t.Errorf("%s registers %s directly; admin routes go through adminRoute", fset.Position(call.Pos()), lit.Value)
			}
			return true
		})
	}
	if checked < 5 {
		t.Fatalf("checked only %d files", checked)
	}
}
