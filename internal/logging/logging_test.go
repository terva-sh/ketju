package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/config"
)

func notATerminal(io.Writer) bool { return false }

// safeBuf is a buffer a handler may write to from any goroutine.
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

func newLogger(t *testing.T, cfg config.Log) (*slog.Logger, *safeBuf) {
	t.Helper()
	var buf safeBuf
	log, err := New(cfg, Options{Writer: &buf, IsTerminal: notATerminal})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return log, &buf
}

// --- Acceptance criterion 1: format selected by config ----------------------

func TestFormatIsSelectedByConfig(t *testing.T) {
	cases := []struct {
		format   string
		terminal bool
		wantJSON bool
	}{
		{"json", false, true},
		{"json", true, true}, // an explicit value wins over the guess
		{"text", false, false},
		{"text", true, false},
		{"auto", false, true}, // a pipe into a collector
		{"auto", true, false}, // a developer's terminal
		{"", false, true},     // the default is auto
	}
	for _, c := range cases {
		var buf safeBuf
		log, err := New(config.Log{Level: "info", Format: c.format},
			Options{Writer: &buf, IsTerminal: func(io.Writer) bool { return c.terminal }})
		if err != nil {
			t.Fatalf("New(%q): %v", c.format, err)
		}
		log.Info("hello", "n", 1)

		out := buf.String()
		isJSON := json.Valid([]byte(strings.TrimSpace(out)))
		if isJSON != c.wantJSON {
			t.Errorf("format=%q terminal=%v produced %q; json=%v want %v",
				c.format, c.terminal, out, isJSON, c.wantJSON)
		}
	}
}

func TestTheLevelIsHonoured(t *testing.T) {
	for _, c := range []struct {
		level     string
		wantDebug bool
	}{
		{"debug", true},
		{"info", false},
		{"warn", false},
		{"error", false},
	} {
		log, buf := newLogger(t, config.Log{Level: c.level, Format: "json"})
		log.Debug("a debug line")
		if got := strings.Contains(buf.String(), "a debug line"); got != c.wantDebug {
			t.Errorf("level=%q debug logged=%v, want %v", c.level, got, c.wantDebug)
		}
	}
}

func TestAnUnknownLevelOrFormatIsRefused(t *testing.T) {
	if _, err := New(config.Log{Level: "verbose", Format: "json"}, Options{Writer: &safeBuf{}}); err == nil {
		t.Error("an unknown level was accepted")
	}
	if _, err := New(config.Log{Level: "info", Format: "yaml"}, Options{Writer: &safeBuf{}}); err == nil {
		t.Error("an unknown format was accepted")
	}
}

// --- Acceptance criterion 2: the request line ------------------------------

func TestRequestLogsCarryWhatTheyMust(t *testing.T) {
	log, buf := newLogger(t, config.Log{Level: "info", Format: "json"})

	h := Middleware(log)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ContextWithViewer(r.Context(), 42)
		time.Sleep(2 * time.Millisecond)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("hello"))
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/threads/7/posts", nil)
	h.ServeHTTP(httptest.NewRecorder(), req)

	var line map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &line); err != nil {
		t.Fatalf("the request log is not JSON: %v\n%s", err, buf.String())
	}
	for k, want := range map[string]any{
		"method": "POST",
		"path":   "/api/threads/7/posts",
		"status": float64(http.StatusCreated),
		"bytes":  float64(5),
		"viewer": float64(42),
	} {
		if got := line[k]; got != want {
			t.Errorf("%s = %v, want %v", k, got, want)
		}
	}
	if _, ok := line["duration_ms"]; !ok {
		t.Error("the request log carries no duration")
	}
}

// TestAViewerSetInsideTheChainReachesTheLog is the case the pointer slot
// exists for, and the one a plain context value silently fails.
//
// Request logging is outermost so that every request produces one line.
// Session resolution runs inside it. A context value set down there travels
// inward with the derived request and never reaches the logger's deferred
// write, which still holds the original -- so the viewer is missing from every
// line while the logger looks like it is working.
func TestAViewerSetInsideTheChainReachesTheLog(t *testing.T) {
	log, buf := newLogger(t, config.Log{Level: "info", Format: "json"})

	// An inner middleware, exactly as session resolution will be written.
	session := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(ContextWithViewer(r.Context(), 99)))
		})
	}
	h := Middleware(log)(session(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if id, ok := ViewerFrom(r.Context()); !ok || id != 99 {
			t.Errorf("the handler sees viewer %d, %v", id, ok)
		}
	})))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	if !strings.Contains(buf.String(), `"viewer":99`) {
		t.Errorf("a viewer set inside the chain never reached the log:\n%s", buf.String())
	}
}

func TestAnAnonymousRequestHasNoViewer(t *testing.T) {
	log, buf := newLogger(t, config.Log{Level: "info", Format: "json"})
	h := Middleware(log)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	var line map[string]any
	_ = json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &line)
	if _, ok := line["viewer"]; ok {
		// A viewer of 0 would read as "user zero" rather than "nobody", and
		// the difference matters when somebody greps for who did something.
		t.Errorf("an anonymous request logged a viewer: %v", line["viewer"])
	}
}

func TestTheStatusPicksTheLevel(t *testing.T) {
	for _, c := range []struct {
		status int
		want   string
	}{
		{200, "INFO"}, {304, "INFO"}, {404, "WARN"}, {429, "WARN"}, {500, "ERROR"},
	} {
		log, buf := newLogger(t, config.Log{Level: "debug", Format: "json"})
		h := Middleware(log)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(c.status)
		}))
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil))

		var line map[string]any
		_ = json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &line)
		if line["level"] != c.want {
			// A wall of 404s in the error stream trains people to ignore it,
			// and the 500 in the middle is what they then miss.
			t.Errorf("status %d logged at %v, want %s", c.status, line["level"], c.want)
		}
	}
}

// --- Acceptance criterion 3: nothing sensitive reaches the output -----------

// TestTheQueryStringNeverReachesTheLog is the one that would be a live
// vulnerability rather than a policy breach.
//
// GET /auth/{provider}/callback?code=...&state=... is how every login
// completes. Logging the request URI puts an authorization code into every
// aggregator the deployment feeds, and one of those is enough to finish
// somebody else's login.
func TestTheQueryStringNeverReachesTheLog(t *testing.T) {
	log, buf := newLogger(t, config.Log{Level: "info", Format: "json"})
	h := Middleware(log)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	const code = "4/0AY0e-authorization-code-that-must-not-be-logged"
	req := httptest.NewRequest(http.MethodGet,
		"/auth/google/callback?code="+code+"&state=abc123&session_state=xyz", nil)
	h.ServeHTTP(httptest.NewRecorder(), req)

	out := buf.String()
	for _, secret := range []string{code, "state=abc123", "xyz", "?"} {
		if strings.Contains(out, secret) {
			t.Errorf("the log carries %q:\n%s", secret, out)
		}
	}
	if !strings.Contains(out, "/auth/google/callback") {
		t.Errorf("the path was lost along with the query:\n%s", out)
	}
}

// TestSensitiveAttributesAreRedacted covers the second half of the criterion.
func TestSensitiveAttributesAreRedacted(t *testing.T) {
	const (
		body    = "the full text of a post that was later deleted"
		session = "s_01J9ZQ8X4C7K2N0P"
		token   = "eyJhbGciOiJSUzI1NiJ9.payload.signature"
	)
	log, buf := newLogger(t, config.Log{Level: "info", Format: "json"})

	log.Info("saved",
		"source", body,
		"session_id", session,
		"access_token", token,
		"client_secret", "hunter2",
		"code", "authcode",
		"nonce", "n1",
		"cookie", "ketju_session="+session,
		// Things that must survive, or the log is useless.
		"thread", 7, "ordinal", 12, "status", 201, "provider", "google",
	)

	out := buf.String()
	for _, secret := range []string{body, session, token, "hunter2", "authcode"} {
		if strings.Contains(out, secret) {
			t.Errorf("the log carries %q:\n%s", secret, out)
		}
	}
	for _, keep := range []string{`"thread":7`, `"ordinal":12`, `"status":201`, `"provider":"google"`} {
		if !strings.Contains(out, keep) {
			t.Errorf("redaction ate %s:\n%s", keep, out)
		}
	}
	if n := strings.Count(out, Redacted); n != 7 {
		t.Errorf("%d values redacted, want 7:\n%s", n, out)
	}
}

// TestRedactionReachesThroughWithAndGroups is where a naive implementation
// leaks.
//
// Attributes attached with With are handled once, when the child logger is
// built, not on every record; a redactor that only walks Record.Attrs misses
// them entirely. Groups nest, so a secret one level down is still a secret.
func TestRedactionReachesThroughWithAndGroups(t *testing.T) {
	const secret = "s_do-not-print-me"
	log, buf := newLogger(t, config.Log{Level: "info", Format: "json"})

	child := log.With("session_id", secret, "request", "r1")
	child.Info("one")

	child.Info("two", slog.Group("auth",
		"token", secret,
		"provider", "google",
		slog.Group("inner", "secret", secret, "ok", 1),
	))

	log.WithGroup("outer").Info("three", "password", secret)

	out := buf.String()
	if strings.Contains(out, secret) {
		t.Errorf("a secret survived With or a group:\n%s", out)
	}
	// The group is named "auth", which is a natural name for a container and is
	// deliberately not on the denylist: its useful fields survive and the
	// dangerous one inside it is caught by its own key.
	if !strings.Contains(out, `"provider":"google"`) || !strings.Contains(out, `"ok":1`) {
		t.Errorf("redaction ate the surrounding group:\n%s", out)
	}
	if !strings.Contains(out, `"request":"r1"`) {
		t.Errorf("redaction ate a With attribute that was fine:\n%s", out)
	}
}

// TestASelfRedactingTypeIsStillRedacted checks the mechanism that survives
// being given an innocuous key, which the denylist cannot. Note that slog
// itself is what makes this work; see TestAGroupReturnedByALogValuerIsWalked
// for the part this package adds.
// TestAGroupNamedForASecretIsRedactedWhole is the other half of that rule.
//
// Descending into every group would leak: nothing in the denylist matches
// "value", so slog.Group("token", "value", secret) would print it. A group
// whose own key is dangerous is therefore replaced entirely.
func TestAGroupNamedForASecretIsRedactedWhole(t *testing.T) {
	const secret = "t_do-not-print-me"
	log, buf := newLogger(t, config.Log{Level: "info", Format: "json"})
	log.Info("x", slog.Group("token", "value", secret, "kind", "bearer"))

	out := buf.String()
	if strings.Contains(out, secret) {
		t.Errorf("a secret under an innocuous key inside a dangerous group leaked:\n%s", out)
	}
}

// logValuerGroup returns a group from LogValue, which is the shape that gets
// past a redactor that does not resolve.
type logValuerGroup struct{ token string }

func (g logValuerGroup) LogValue() slog.Value {
	return slog.GroupValue(slog.String("token", g.token), slog.String("kind", "bearer"))
}

// TestAGroupReturnedByALogValuerIsWalked is what the Resolve call actually
// buys, as opposed to what it looks like it buys.
//
// slog resolves a LogValuer itself before formatting, so a self-redacting type
// is safe with or without it. A type that returns a *group*, though, is only
// walked if it is resolved first: unresolved its kind is LogValuer, the descent
// skips it, and the inner handler then expands it with the token intact.
func TestAGroupReturnedByALogValuerIsWalked(t *testing.T) {
	const secret = "t_returned-inside-a-group"
	log, buf := newLogger(t, config.Log{Level: "info", Format: "json"})

	// Under a key that is itself harmless, so only the descent can catch it.
	log.Info("x", "thing", logValuerGroup{secret})

	out := buf.String()
	if strings.Contains(out, secret) {
		t.Errorf("a token inside a group returned by LogValue leaked:\n%s", out)
	}
	if !strings.Contains(out, `"kind":"bearer"`) {
		t.Errorf("the rest of the group was lost:\n%s", out)
	}
}

func TestASelfRedactingTypeIsStillRedacted(t *testing.T) {
	log, buf := newLogger(t, config.Log{Level: "info", Format: "json"})
	s := config.NewSecret("hunter2")

	// A key no denylist would ever guess.
	log.Info("configured", "provider_thing", s)

	out := buf.String()
	if strings.Contains(out, "hunter2") {
		t.Errorf("a config.Secret printed its value:\n%s", out)
	}
}

func TestABareKeyIsNotRedacted(t *testing.T) {
	// An OIDC provider key is the thing an operator most needs to see in a line
	// naming a broken provider, and it is not a credential. Only the compound
	// forms are dangerous.
	log, buf := newLogger(t, config.Log{Level: "info", Format: "json"})
	log.Info("provider disabled", "key", "google", "api_key", "sk-live-xyz")

	out := buf.String()
	if !strings.Contains(out, `"key":"google"`) {
		t.Errorf("the provider key was redacted, making the line useless:\n%s", out)
	}
	if strings.Contains(out, "sk-live-xyz") {
		t.Errorf("an api_key survived:\n%s", out)
	}
}

func TestRedactionIsCaseInsensitive(t *testing.T) {
	log, buf := newLogger(t, config.Log{Level: "info", Format: "json"})
	log.Info("x", "Session_ID", "s_secret", "TOKEN", "t_secret", "Authorization", "Bearer abc")
	out := buf.String()
	for _, secret := range []string{"s_secret", "t_secret", "Bearer abc"} {
		if strings.Contains(out, secret) {
			t.Errorf("%q survived a differently-cased key:\n%s", secret, out)
		}
	}
}

// --- The response writer wrapper ---------------------------------------------

// TestTheWrapperDoesNotBreakStreaming. docs/02 serves live updates over SSE at
// /events. A ResponseWriter wrapper that does not implement Flusher turns a
// live stream into a response that arrives all at once when the handler
// returns, which looks like "live updates are broken" and not like a logging
// change.
func TestTheWrapperDoesNotBreakStreaming(t *testing.T) {
	log, _ := newLogger(t, config.Log{Level: "info", Format: "json"})

	var flushedDirectly, flushedByController bool
	h := Middleware(log)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
			flushedDirectly = true
		}
		if err := http.NewResponseController(w).Flush(); err == nil {
			flushedByController = true
		}
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/events", nil))

	if !flushedDirectly {
		t.Error("the wrapper does not implement http.Flusher; SSE would buffer")
	}
	if !flushedByController {
		t.Error("http.ResponseController cannot reach through the wrapper")
	}
}

func TestAHandlerThatNeverWritesLogs200(t *testing.T) {
	log, buf := newLogger(t, config.Log{Level: "info", Format: "json"})
	h := Middleware(log)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	if !strings.Contains(buf.String(), `"status":200`) {
		t.Errorf("a handler that wrote nothing did not log 200:\n%s", buf.String())
	}
}

func TestASecondWriteHeaderDoesNotChangeTheStatus(t *testing.T) {
	log, buf := newLogger(t, config.Log{Level: "debug", Format: "json"})
	h := Middleware(log)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.WriteHeader(http.StatusOK) // net/http ignores this; so must the log
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	if !strings.Contains(buf.String(), `"status":404`) {
		t.Errorf("the logged status followed the second WriteHeader:\n%s", buf.String())
	}
}

func TestMiddlewareIsSafeForConcurrentUse(t *testing.T) {
	log, buf := newLogger(t, config.Log{Level: "info", Format: "json"})
	h := Middleware(log)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))

	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 25 {
				h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil))
			}
		}()
	}
	wg.Wait()

	if n := strings.Count(buf.String(), `"msg":"request"`); n != 400 {
		t.Errorf("%d request lines, want 400", n)
	}
}

func TestViewerRoundTrips(t *testing.T) {
	ctx := ContextWithViewer(context.Background(), 7)
	if id, ok := ViewerFrom(ctx); !ok || id != 7 {
		t.Errorf("ViewerFrom = %d, %v", id, ok)
	}
	if _, ok := ViewerFrom(context.Background()); ok {
		t.Error("an empty context reported a viewer")
	}
}

func TestAutoDetectsARealTerminal(t *testing.T) {
	// The default IsTerminal is a file-mode check rather than a guess, so it
	// can be exercised against real handles instead of mocked.
	if isCharDevice(&safeBuf{}) {
		t.Error("a buffer reported as a terminal")
	}
	f, err := os.CreateTemp(t.TempDir(), "log")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if isCharDevice(f) {
		t.Error("a regular file reported as a terminal; a redirected log would get text")
	}
	if dev, err := os.Open(os.DevNull); err == nil {
		defer dev.Close()
		if !isCharDevice(dev) {
			t.Error("/dev/null is a character device and was not detected as one")
		}
	}
}
