package web

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/terva-sh/ketju/internal/logging"
)

type safeBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *safeBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *safeBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func testLogger() (*slog.Logger, *safeBuf) {
	var buf safeBuf
	return slog.New(logging.Redact(slog.NewJSONHandler(&buf, nil))), &buf
}

// marker returns a handler that identifies itself, so a routing test can assert
// which group answered rather than only that something did.
func marker(name string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Group", name)
		_, _ = io.WriteString(w, name+" "+r.URL.Path)
	})
}

func allGroups() Groups {
	return Groups{
		API:    marker("api"),
		Auth:   marker("auth"),
		Blobs:  marker("blobs"),
		Events: marker("events"),
		Assets: marker("assets"),
		SPA:    marker("spa"),
	}
}

func do(h http.Handler, method, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

// --- Acceptance criterion 1: every prefix routes ----------------------------

func TestEveryPrefixRoutesToItsGroup(t *testing.T) {
	log, _ := testLogger()
	h := New(allGroups(), Options{Logger: log})

	cases := []struct{ path, group string }{
		{"/api/v1/me", "api"},
		{"/api/v1/threads/3yymw47g00001?page=2", "api"},
		{"/auth/google/start", "auth"},
		{"/auth/google/callback", "auth"},
		{"/auth/logout", "auth"},
		{"/blobs/deadbeef", "blobs"},
		{"/events", "events"},
		{"/assets/app.js", "assets"},

		// Application routes from docs/07. None of these exist on the server;
		// all of them are the client's, and all must reach the shell.
		{"/", "spa"},
		{"/f/general", "spa"},
		{"/t/3yymw47g00001/why-we-left-slack", "spa"},
		{"/t/3yymw47g00001/why-we-left-slack/41", "spa"},
		{"/u/alice", "spa"},
		{"/settings/sessions", "spa"},
		{"/admin/queue", "spa"},
		{"/unread", "spa"},
	}
	for _, c := range cases {
		rec := do(h, http.MethodGet, c.path)
		if got := rec.Header().Get("X-Group"); got != c.group {
			t.Errorf("%s routed to %q, want %q (status %d)", c.path, got, c.group, rec.Code)
		}
	}
}

func TestTheAPIHandlerSeesTheFullPath(t *testing.T) {
	// Not a stripped one. Handlers register the routes docs/07 documents, and
	// a stripped prefix would mean every handler's pattern disagreeing with
	// the document that defines it.
	log, _ := testLogger()
	h := New(allGroups(), Options{Logger: log})
	rec := do(h, http.MethodGet, "/api/v1/forums")
	if !strings.Contains(rec.Body.String(), "/api/v1/forums") {
		t.Errorf("the API group saw %q", rec.Body.String())
	}
}

func TestAMissingGroupSays501NotFound(t *testing.T) {
	log, _ := testLogger()
	h := New(Groups{SPA: marker("spa")}, Options{Logger: log})

	for _, path := range []string{"/api/v1/me", "/auth/x/start", "/blobs/x", "/events", "/assets/x"} {
		rec := do(h, http.MethodGet, path)
		// 501 and not 404: "this build does not implement that" is a different
		// fact from "there is no such endpoint", to a developer and to a
		// monitoring check alike.
		if rec.Code != http.StatusNotImplemented {
			t.Errorf("%s = %d, want 501", path, rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); ct != ProblemContentType {
			t.Errorf("%s content type = %q, want %q", path, ct, ProblemContentType)
		}
	}
}

// --- Acceptance criterion 2: the catch-all rule -----------------------------

// TestNoAPIPathIsAnsweredWithARedirect. ServeMux answers the bare path of a
// subtree pattern with a 307 to the trailing-slash form. That is right for a
// browser and wrong for an API: a client that POSTs JSON to /api/v1 would get a
// redirect, and a redirect is the one response where the body and the method
// depend on what the client decides to do next.
func TestNoAPIPathIsAnsweredWithARedirect(t *testing.T) {
	log, _ := testLogger()
	h := New(allGroups(), Options{Logger: log})

	for _, p := range []string{"/api", "/api/v1"} {
		rec := do(h, http.MethodPost, p)
		if rec.Code >= 300 && rec.Code < 400 {
			t.Errorf("%s = %d (%s); an API client must not be redirected",
				p, rec.Code, rec.Header().Get("Location"))
		}
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s = %d, want 404", p, rec.Code)
		}
	}
}

// TestTheCatchAllTellsAPIPathsFromApplicationPaths is the criterion with the
// two classic failures behind it.
//
// Serve the shell for an unknown API path and a mistyped endpoint returns 200
// with a page of HTML, which a JSON client reports as a parse error rather than
// as the 404 it is. Serve a 404 for an unknown application path and every deep
// link breaks on refresh.
func TestTheCatchAllTellsAPIPathsFromApplicationPaths(t *testing.T) {
	log, _ := testLogger()
	h := New(Groups{SPA: marker("spa")}, Options{Logger: log})

	// Paths under /api/v1/ belong to the API group, which owns its own 404s
	// because only it knows its routes; with no API group mounted they are 501.
	// What the catch-all decides is everything else under /api.
	apiPaths := []string{
		"/api/v1/nope",
		"/api/v1/threads/1/nonsense",
		"/api/v1",
		"/api",
		// A version this build does not serve is still not an application
		// route, and must not be handed the shell.
		"/api/v2/threads",
		"/api/nonsense",
	}
	for _, p := range apiPaths {
		rec := do(h, http.MethodGet, p)
		if rec.Header().Get("X-Group") == "spa" {
			t.Errorf("%s was answered with the SPA shell", p)
		}
		if ct := rec.Header().Get("Content-Type"); ct != ProblemContentType {
			t.Errorf("%s content type = %q, want a problem response", p, ct)
		}
		if rec.Code != http.StatusNotFound && rec.Code != http.StatusNotImplemented {
			t.Errorf("%s = %d, want 404 or 501", p, rec.Code)
		}
	}

	appPaths := []string{"/", "/anything", "/deep/link/the/client/knows", "/t/abc/slug/9"}
	for _, p := range appPaths {
		rec := do(h, http.MethodGet, p)
		if rec.Header().Get("X-Group") != "spa" {
			t.Errorf("%s = %d, did not reach the shell; deep links would 404 on refresh",
				p, rec.Code)
		}
	}
}

func TestAProblemBodyIsRFC9457Shaped(t *testing.T) {
	log, _ := testLogger()
	h := New(allGroups(), Options{Logger: log})
	// A path under /api that this build does not serve, so the catch-all
	// answers rather than the API group.
	rec := do(h, http.MethodGet, "/api/v2/threads")

	var p Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatalf("the problem body is not JSON: %v\n%s", err, rec.Body.String())
	}
	if p.Type == "" || p.Title == "" {
		t.Errorf("problem = %+v, want a type and a title", p)
	}
	if p.Status != http.StatusNotFound {
		t.Errorf("problem status = %d, want it to repeat the HTTP status", p.Status)
	}
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "no-store") {
		// A 404 cached by an intermediary outlives the deploy that adds the
		// route it was complaining about.
		t.Errorf("Cache-Control = %q, want no-store", cc)
	}
}

func TestAProblemNeverEchoesTheRequest(t *testing.T) {
	log, _ := testLogger()
	h := New(allGroups(), Options{Logger: log})

	const probe = "<script>alert(1)</script>"
	rec := do(h, http.MethodGet, "/api/v2/"+probe)

	// An error body goes straight back to whoever sent it. Echoing the input is
	// how a helpful message becomes a way to probe the server, and with the
	// wrong content type, worse than that.
	if strings.Contains(rec.Body.String(), "script") {
		t.Errorf("the problem body echoed the request path:\n%s", rec.Body.String())
	}
}

// --- Acceptance criterion 3: a panic is survivable --------------------------

func TestAPanicBecomes500AndIsLogged(t *testing.T) {
	log, buf := testLogger()
	h := New(Groups{
		API: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			panic("handler exploded")
		}),
		SPA: marker("spa"),
	}, Options{Logger: log})

	rec := do(h, http.MethodGet, "/api/v1/boom")
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != ProblemContentType {
		t.Errorf("content type = %q, want a problem response", ct)
	}

	out := buf.String()
	if !strings.Contains(out, "panic in handler") {
		t.Errorf("the panic was not logged:\n%s", out)
	}
	if !strings.Contains(out, "handler exploded") {
		t.Errorf("the log does not say what panicked:\n%s", out)
	}
	if !strings.Contains(out, "stack") {
		t.Errorf("the log carries no stack, which is the part that locates it:\n%s", out)
	}

	// And the server still serves.
	if got := do(h, http.MethodGet, "/anything").Header().Get("X-Group"); got != "spa" {
		t.Errorf("a later request routed to %q; the panic took something down", got)
	}
}

// TestAPanicIsLoggedWithTheStatusTheClientGot is why recovery sits inside the
// request log rather than outside it.
//
// Outside, the log line is never written at all. Inside but after the logger
// reads the status, every crash is recorded as the 200 the recorder still
// held -- a log that says the server is healthy while it is returning 500s.
func TestAPanicIsLoggedWithTheStatusTheClientGot(t *testing.T) {
	log, buf := testLogger()
	h := New(Groups{
		API: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { panic("boom") }),
	}, Options{Logger: log})

	do(h, http.MethodGet, "/api/v1/boom")

	out := buf.String()
	if !strings.Contains(out, `"msg":"request"`) {
		t.Fatalf("a panicking request produced no request log line:\n%s", out)
	}
	if !strings.Contains(out, `"status":500`) {
		t.Errorf("the request line does not record the 500 the client got:\n%s", out)
	}
}

// TestAbortHandlerIsNotSwallowed. net/http panics with this value when a
// handler deliberately abandons a response. Turning it into a 500 would log a
// crash that did not happen and write a body to a connection the handler had
// given up on.
func TestAbortHandlerIsNotSwallowed(t *testing.T) {
	log, buf := testLogger()
	h := New(Groups{
		API: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			panic(http.ErrAbortHandler)
		}),
	}, Options{Logger: log})

	defer func() {
		if v := recover(); v != http.ErrAbortHandler {
			t.Errorf("recovered %v, want ErrAbortHandler to pass through", v)
		}
		if strings.Contains(buf.String(), "panic in handler") {
			t.Errorf("a deliberate abort was logged as a crash:\n%s", buf.String())
		}
	}()
	do(h, http.MethodGet, "/api/v1/abort")
}

func TestAPanicValueIsTruncated(t *testing.T) {
	log, buf := testLogger()
	huge := strings.Repeat("post-source-", 200)
	h := New(Groups{
		API: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { panic(huge) }),
	}, Options{Logger: log})

	do(h, http.MethodGet, "/api/v1/boom")

	// A panic value is often an error carrying whatever the handler was working
	// on, which here can be a post body. Enough to identify the bug, not a copy
	// of the data.
	if strings.Contains(buf.String(), huge) {
		t.Error("a panic value was logged in full")
	}
	if !strings.Contains(buf.String(), "post-source-") {
		t.Error("the panic value was dropped entirely, leaving nothing to identify it")
	}
}

func TestThePanicPathIsLoggedWithoutTheQuery(t *testing.T) {
	log, buf := testLogger()
	h := New(Groups{
		Auth: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { panic("boom") }),
	}, Options{Logger: log})

	do(h, http.MethodGet, "/auth/google/callback?code=SECRET-CODE&state=s")

	if strings.Contains(buf.String(), "SECRET-CODE") {
		t.Errorf("the panic log carries the authorization code:\n%s", buf.String())
	}
}

// --- Acceptance criterion 4: the chain order --------------------------------

// TestTheMiddlewareRunsInTheDocumentedOrder turns the comment on chain into
// something that fails when the order changes.
func TestTheMiddlewareRunsInTheDocumentedOrder(t *testing.T) {
	log, _ := testLogger()
	var seen []string
	var mu sync.Mutex
	record := func(name string) func(http.Handler) http.Handler {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				seen = append(seen, name)
				mu.Unlock()
				next.ServeHTTP(w, r)
			})
		}
	}

	h := New(Groups{
		API: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			seen = append(seen, "handler")
			mu.Unlock()
		}),
	}, Options{
		Logger:    log,
		RateLimit: record("ratelimit"),
		Session:   record("session"),
		CSRF:      record("csrf"),
	})

	do(h, http.MethodGet, "/api/v1/me")

	want := []string{"ratelimit", "session", "csrf", "handler"}
	if len(seen) != len(want) {
		t.Fatalf("ran %v, want %v", seen, want)
	}
	for i := range want {
		if seen[i] != want[i] {
			// Rate limiting after session resolution means an unauthenticated
			// flood costs a database query each, and the limiter is protecting
			// the thing it stands behind.
			t.Fatalf("ran %v, want %v", seen, want)
		}
	}
}

func TestTheRateLimiterCanRejectAndStillBeLogged(t *testing.T) {
	log, buf := testLogger()
	h := New(Groups{API: marker("api")}, Options{
		Logger: log,
		RateLimit: func(http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				WriteProblem(w, r, http.StatusTooManyRequests, "rate-limited", "Slow down.")
			})
		},
	})

	rec := do(h, http.MethodGet, "/api/v1/me")
	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want 429", rec.Code)
	}
	// Logging is outermost precisely so that requests refused before any
	// handler runs are still visible. A limiter whose rejections do not appear
	// in the log is a limiter nobody can tune.
	if !strings.Contains(buf.String(), `"status":429`) {
		t.Errorf("a rejected request produced no log line:\n%s", buf.String())
	}
}

// TestTheSessionViewerReachesTheRequestLog joins the two halves: session
// resolution runs deep in the chain, the log line is written by the outermost
// middleware, and the viewer still gets there.
func TestTheSessionViewerReachesTheRequestLog(t *testing.T) {
	log, buf := testLogger()
	h := New(Groups{API: marker("api")}, Options{
		Logger: log,
		Session: func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				next.ServeHTTP(w, r.WithContext(logging.ContextWithViewer(r.Context(), 7)))
			})
		},
	})

	do(h, http.MethodGet, "/api/v1/me")
	if !strings.Contains(buf.String(), `"viewer":7`) {
		t.Errorf("the viewer did not reach the request log:\n%s", buf.String())
	}
}

// --- The body limit ----------------------------------------------------------

func TestTheBodyLimitApplies(t *testing.T) {
	log, _ := testLogger()
	var readErr error
	h := New(Groups{
		API: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, readErr = io.ReadAll(r.Body)
		}),
	}, Options{Logger: log, MaxBody: 64})

	body := strings.NewReader(strings.Repeat("x", 4096))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/threads", body)
	h.ServeHTTP(httptest.NewRecorder(), req)

	if readErr == nil {
		t.Error("a body past the limit was read in full")
	}
	var maxErr *http.MaxBytesError
	if !errors.As(readErr, &maxErr) {
		t.Errorf("read error = %v, want MaxBytesError so a handler can map it to 413", readErr)
	}
}

func TestABodyUnderTheLimitIsUntouched(t *testing.T) {
	log, _ := testLogger()
	var got string
	h := New(Groups{
		API: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			b, _ := io.ReadAll(r.Body)
			got = string(b)
		}),
	}, Options{Logger: log, MaxBody: 64})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/threads", strings.NewReader("hello"))
	h.ServeHTTP(httptest.NewRecorder(), req)
	if got != "hello" {
		t.Errorf("body = %q, want hello", got)
	}
}

func TestHeadOnAProblemHasNoBody(t *testing.T) {
	log, _ := testLogger()
	h := New(allGroups(), Options{Logger: log})
	rec := do(h, http.MethodHead, "/api/v2/nope")
	if rec.Body.Len() != 0 {
		t.Errorf("HEAD returned a body: %q", rec.Body.String())
	}
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}
