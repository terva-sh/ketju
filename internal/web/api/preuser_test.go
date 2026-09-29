package api

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/auth/session"
	"github.com/terva-sh/ketju/internal/auth/session/sessiontest"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/web"
)

// TestAPreUserNeverReachesTheResponseCache is AC7 of TKT-01M29CKRG9. An
// approval-mode applicant asking for a thread, through the server's chain
// and the real session middleware, is refused before the thread endpoints
// run, so the response cache is neither looked in nor filled: a pre-user
// is no viewer class the cache key has to tell apart (docs/07 "The response
// cache"). A member's request, the control, is a lookup and an insert, so
// the counters are live.
func TestAPreUserNeverReachesTheResponseCache(t *testing.T) {
	w := newWorld(t)
	id, _ := w.seed()
	var applicant int64
	w.write(func(q *dbq.Queries) error {
		var err error
		applicant, err = q.CreateIdentity(t.Context(), dbq.CreateIdentityParams{ProviderKey: "github",
			Subject: "applicant", Issuer: "https://github.com", CreatedAt: 1, UpdatedAt: 1})
		return err
	})
	now := time.UnixMilli(w.clock)
	store := &session.Store{DB: w.db, Now: func() time.Time { return now }}
	pass := func(h http.Handler) http.Handler { return h }
	h := web.New(web.Groups{API: w.handler}, web.Options{Session: session.Middleware(store, nil), RateLimit: pass})
	get := func(path, token string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		r.AddCookie(sessiontest.Cookie(token))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		return rec
	}
	pre := sessiontest.PreUser(t, w.db, applicant, now)
	member := sessiontest.SignIn(t, w.db, w.users["alice"], now)

	thread := "/api/v1/threads/" + id.String()
	before := w.svc.Responses.Stats()
	for _, path := range []string{thread, thread + "?page=1", postPathN(id, 1),
		postPathN(id, 1) + "/replies", postPathN(id, 1) + "/history"} {
		rec := get(path, pre)
		var pr problem.Problem
		if rec.Code != http.StatusForbidden || json.Unmarshal(rec.Body.Bytes(), &pr) != nil || pr.Type != problem.CodeForbidden {
			t.Errorf("GET %s as a pre-user: %d %s, want 403 forbidden", path, rec.Code, rec.Body)
		}
		if rec.Header().Get("ETag") != "" {
			t.Errorf("GET %s as a pre-user carried an ETag", path)
		}
	}
	after := w.svc.Responses.Stats()
	if after.Hits+after.Misses != before.Hits+before.Misses || after.Inserts != before.Inserts || after.Entries != before.Entries {
		t.Fatalf("the response cache moved for a pre-user: before %+v, after %+v", before, after)
	}

	if rec := get(thread, member); rec.Code != http.StatusOK {
		t.Fatalf("GET %s as a member: %d %s", thread, rec.Code, rec.Body)
	}
	control := w.svc.Responses.Stats()
	if control.Hits+control.Misses == after.Hits+after.Misses || control.Inserts == after.Inserts {
		t.Fatalf("a member's page did not touch the cache (%+v then %+v): the counters above prove nothing",
			after, control)
	}
}

// TestEveryRouteIsInTheTable: this package registers a route only from
// routes() or through adminRoute, so Routes lists everything New serves
// and the pre-user walk in internal/web misses nothing. A literal pattern
// handed to a mux anywhere else is a route the walk cannot see; the one
// allowed is the /api/v1/ catch-all, which answers not-found.
func TestEveryRouteIsInTheTable(t *testing.T) {
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
			if lit, ok := call.Args[0].(*ast.BasicLit); ok && lit.Value != `"/api/v1/"` {
				t.Errorf("%s registers %s outside routes() and adminRoute", fset.Position(call.Pos()), lit.Value)
			}
			return true
		})
	}
	if checked < 5 {
		t.Fatalf("checked only %d files", checked)
	}
}
