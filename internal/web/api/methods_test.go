package api

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/terva-sh/ketju/internal/perm"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/service/applications"
	formsvc "github.com/terva-sh/ketju/internal/service/forms"
	"github.com/terva-sh/ketju/internal/service/forums"
	"github.com/terva-sh/ketju/internal/service/users"
)

// A wrong verb on a real route is method-not-allowed with Allow, and
// anything else under /api/v1/ is not-found (TKT-01M3YME9KG, docs/07
// "Error catalogue").

// methodsWorld is the API as New builds it, with no middleware in front:
// the answer under test is the API's own, before any gate.
func methodsWorld(t *testing.T) (http.Handler, []string) {
	t.Helper()
	w := newWorld(t)
	pm := perm.New(w.db)
	d := Deps{Threads: w.svc, Users: &users.Service{DB: w.db, Perm: pm}, Forums: &forums.Service{DB: w.db, Perm: pm},
		Forms: &formsvc.Service{DB: w.db}, Applications: &applications.Service{DB: w.db}, Perm: pm}
	var patterns []string
	for _, rt := range Routes(d) {
		patterns = append(patterns, rt.Method+" "+rt.Path)
	}
	return New(d), patterns
}

// sendBare is one request through h, with no body, as nobody.
func sendBare(h http.Handler, method, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

// allowOrder is the order Allow lists methods in (the ticket's settled
// rule, as internal/web/health writes it). It is written out here rather
// than read from probeMethods, so a method dropped from the probe fails
// this test instead of changing what it expects.
var allowOrder = []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE"}

// TestEveryRouteAnswersAWrongVerbWithMethodNotAllowed walks the route
// table, so a route added later is covered with no edit here. For each
// path it sends every method the path does not accept, OPTIONS included,
// with the path parameters filled in, and wants 405 method-not-allowed
// with Allow naming exactly the path's methods, HEAD among them when GET
// is.
func TestEveryRouteAnswersAWrongVerbWithMethodNotAllowed(t *testing.T) {
	h, patterns := methodsWorld(t)
	accepts := map[string][]string{} // path pattern -> methods
	var paths []string
	for _, p := range patterns {
		method, path, _ := strings.Cut(p, " ")
		if _, seen := accepts[path]; !seen {
			paths = append(paths, path)
		}
		accepts[path] = append(accepts[path], method)
		if method == http.MethodGet {
			accepts[path] = append(accepts[path], http.MethodHead)
		}
	}
	if len(paths) < 40 {
		t.Fatalf("the table has %d paths; the walk is not seeing the API", len(paths))
	}
	tried := 0
	for _, path := range paths {
		var allow []string
		for _, m := range allowOrder {
			if slices.Contains(accepts[path], m) {
				allow = append(allow, m)
			}
		}
		want := strings.Join(allow, ", ")
		url := pathParam.ReplaceAllString(strings.TrimSuffix(path, "{$}"), "1")
		for _, m := range append(slices.Clone(allowOrder), http.MethodOptions) {
			if slices.Contains(accepts[path], m) {
				continue
			}
			tried++
			rec := sendBare(h, m, url)
			// A HEAD answer has headers and no body (problem.Write).
			if rec.Code != http.StatusMethodNotAllowed ||
				(m != http.MethodHead && problemCode(t, rec) != string(problem.CodeMethodNotAllowed)) {
				t.Errorf("%s %s (route %s): %d %s", m, url, path, rec.Code, rec.Body)
				continue
			}
			if got := rec.Header().Get("Allow"); got != want {
				t.Errorf("%s %s (route %s): Allow %q, want %q", m, url, path, got, want)
			}
			if ct := rec.Header().Get("Content-Type"); ct != problem.ContentType {
				t.Errorf("%s %s: Content-Type %q", m, url, ct)
			}
		}
	}
	if tried == 0 {
		t.Fatal("no wrong verb was sent")
	}
}

// TestAnUnknownPathIsStillNotFound: a path no route has, under any
// method, is not-found with no Allow.
func TestAnUnknownPathIsStillNotFound(t *testing.T) {
	h, _ := methodsWorld(t)
	for _, c := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/nope"},
		{http.MethodDelete, "/api/v1/threads"},
		{http.MethodGet, "/api/v1/threads"},
		{http.MethodPost, "/api/v1/admin/nope"},
		{http.MethodGet, "/api/v1/"},
	} {
		rec := sendBare(h, c.method, c.path)
		if rec.Code != http.StatusNotFound || problemCode(t, rec) != string(problem.CodeNotFound) {
			t.Errorf("%s %s: %d %s", c.method, c.path, rec.Code, rec.Body)
		}
		if a, ok := rec.Header()["Allow"]; ok {
			t.Errorf("%s %s: Allow %q on a not-found", c.method, c.path, a)
		}
	}
}

// TestHeadOnAGetRouteIsServed: HEAD reaches a GET route's handler as GET
// does, rather than the 405.
func TestHeadOnAGetRouteIsServed(t *testing.T) {
	h, _ := methodsWorld(t)
	get, head := sendBare(h, http.MethodGet, "/api/v1/forums"), sendBare(h, http.MethodHead, "/api/v1/forums")
	if get.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/forums: %d %s", get.Code, get.Body)
	}
	if head.Code != get.Code || head.Header().Get("Content-Type") != get.Header().Get("Content-Type") {
		t.Errorf("HEAD /api/v1/forums: %d %q, GET %d %q", head.Code, head.Header().Get("Content-Type"),
			get.Code, get.Header().Get("Content-Type"))
	}
	if _, ok := head.Header()["Allow"]; ok {
		t.Error("HEAD on a GET route carries Allow")
	}
}
