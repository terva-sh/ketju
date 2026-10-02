package main

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/terva-sh/ketju/internal/auth/oidc/oidctest"
	"github.com/terva-sh/ketju/internal/domain"
)

// withMetrics is siteURL's environment with metrics_listen set to addr.
func withMetrics(addr string) func(string) string {
	return func(k string) string {
		if k == "KETJU_METRICS_LISTEN" {
			return addr
		}
		return siteURL(k)
	}
}

// startMetrics starts serve with metrics_listen on a free port and returns
// it with the metrics listener's address.
func startMetrics(t *testing.T, extra string, o serveOpts) (*running, string, string) {
	t.Helper()
	path, data := writeConfig(t, extra)
	maddr := make(chan string, 1)
	o.metricsReady = func(a string) { maddr <- a }
	if o.getenv == nil {
		o.getenv = withMetrics(freePort(t))
	}
	r := start(t, path, o)
	return r, <-maddr, data
}

// TestMetricsGuard: /metrics answers only on metrics_listen, and only
// there; the main listener answers 404 for it and never the metrics, and an
// empty metrics_listen starts no listener (docs/02, maintainer decision of
// 2026-10-02).
func TestMetricsGuard(t *testing.T) {
	r, maddr, _ := startMetrics(t, "", serveOpts{})

	code, body := get(t, "http://"+maddr+"/metrics")
	if code != http.StatusOK || !strings.Contains(body, "# TYPE ketju_http_request_duration_seconds histogram") ||
		!strings.Contains(body, "ketju_projection_lag_bytes") {
		t.Errorf("GET /metrics on metrics_listen = %d:\n%s", code, body)
	}
	for _, p := range []string{"/", "/api/v1/me", "/healthz", "/metrics/x"} {
		if code, _ := get(t, "http://"+maddr+p); code != http.StatusNotFound {
			t.Errorf("GET %s on metrics_listen = %d, want 404: it serves /metrics and nothing else", p, code)
		}
	}
	code, body = get(t, "http://"+r.addr+"/metrics")
	if code != http.StatusNotFound || strings.Contains(body, "ketju_") {
		t.Errorf("GET /metrics on the main listener = %d:\n%s", code, body)
	}
	// The request to the main listener was counted, by its pattern.
	if _, body := get(t, "http://"+maddr+"/metrics"); !strings.Contains(body, `route="/metrics",method="GET",code="4xx"`) {
		t.Errorf("the main listener's request was not observed:\n%s", body)
	}
	if err := r.shutdown(t); err != nil {
		t.Fatalf("shutdown: %v\n%s", err, r.log)
	}
	steps := r.steps()
	if i, j := slices.Index(steps, "stop metrics"), slices.Index(steps, "close threads"); i < 0 || j < 0 || i > j {
		t.Errorf("the metrics listener must stop before the stores close: %v", steps)
	}

	// Off by default: no listener, no step, and still a 404 on the main one.
	path, _ := writeConfig(t, "")
	r = start(t, path, serveOpts{})
	if code, body := get(t, "http://"+r.addr+"/metrics"); code != http.StatusNotFound || strings.Contains(body, "ketju_") {
		t.Errorf("GET /metrics with metrics off = %d:\n%s", code, body)
	}
	if err := r.shutdown(t); err != nil {
		t.Fatal(err)
	}
	if steps := r.steps(); slices.Contains(steps, "metrics") || slices.Contains(steps, "stop metrics") {
		t.Errorf("an empty metrics_listen started a listener: %v", steps)
	}
	if strings.Contains(r.log.String(), "serving metrics") {
		t.Errorf("an empty metrics_listen logged a metrics listener:\n%s", r.log)
	}
}

// TestMetricsListenRefusesTheMainAddress: metrics_listen equal to listen is
// a config error, since metrics are never served beside the forum.
func TestMetricsListenRefusesTheMainAddress(t *testing.T) {
	path, _ := writeConfig(t, "")
	err := serve(t.Context(), path, serveOpts{getenv: func(k string) string {
		if k == "KETJU_METRICS_LISTEN" {
			return "127.0.0.1:1"
		}
		if k == "KETJU_LISTEN" {
			return "127.0.0.1:1"
		}
		return ""
	}})
	if err == nil || !strings.Contains(err.Error(), "metrics_listen") {
		t.Errorf("serve with metrics_listen = listen: %v", err)
	}
}

// TestMetricsCarryNoIdentifiers: after a session that signs in, starts a
// thread in a forum, replies, reads it, opens a profile and lists the
// forum, the scrape names none of the thread's id, the handles, the forum's
// slug, the title or any path that carries them.
func TestMetricsCarryNoIdentifiers(t *testing.T) {
	const handle, subject = "zelda-quux", "zelda-quux-sub"
	idp := oidctest.New(t)
	idp.Users = []string{subject}
	path, data := writeConfig(t, providerBlock(idp.URL))
	seedPeople(t, data, idp.URL, seeded{subject: subject, handle: handle, role: domain.RoleMember})
	maddr := make(chan string, 1)
	r := start(t, path, serveOpts{idpClient: idp.Client(), getenv: withMetrics(freePort(t)),
		metricsReady: func(a string) { maddr <- a }})
	m := <-maddr

	a := newActor(r, handle)
	if res := signIn(t, a, idp, subject); res.status != http.StatusSeeOther {
		t.Fatalf("signing in answered %d:\n%s", res.status, res)
	}
	res := a.do(t, http.MethodPost, "/api/v1/forums/general/threads",
		map[string]any{"title": "Plover xyzzy", "source": "First.", "tags": []string{}})
	if res.status != http.StatusCreated {
		t.Fatalf("starting a thread answered %d:\n%s", res.status, res)
	}
	var out struct {
		Thread struct {
			ID   string `json:"id"`
			Slug string `json:"slug"`
		} `json:"thread"`
	}
	res.decode(t, &out)
	id := out.Thread.ID
	for _, c := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/threads/" + id},
		{http.MethodGet, "/api/v1/threads/" + id + "/posts/1"},
		{http.MethodGet, "/api/v1/users/" + handle},
		{http.MethodGet, "/api/v1/users/" + handle + "/posts"},
		{http.MethodGet, "/api/v1/forums/general/threads"},
		{http.MethodGet, "/api/v1/search?q=plover"},
		{http.MethodGet, "/f/general/t/" + id + "/plover-xyzzy?page=2"},
		{http.MethodGet, "/u/" + handle},
	} {
		a.do(t, c.method, c.path, nil)
	}
	if res := a.do(t, http.MethodPost, "/api/v1/threads/"+id+"/posts", map[string]any{"source": "Second."}); res.status != http.StatusCreated {
		t.Fatalf("replying answered %d:\n%s", res.status, res)
	}

	code, body := get(t, "http://"+m+"/metrics")
	if code != http.StatusOK {
		t.Fatalf("GET /metrics = %d", code)
	}
	if !strings.Contains(body, `route="/api/v1/threads/{id}"`) {
		t.Errorf("the thread read is not in the scrape by its pattern:\n%s", body)
	}
	for _, leak := range []string{id, handle, subject, "general", "plover", "Plover", "xyzzy", out.Thread.Slug, "page=2", "q="} {
		if leak != "" && strings.Contains(body, leak) {
			t.Errorf("the scrape carries %q:\n%s", leak, body)
		}
	}
}
