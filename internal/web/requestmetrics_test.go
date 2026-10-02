package web

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/terva-sh/ketju/internal/metrics"
	"github.com/terva-sh/ketju/internal/problem"
)

// metricsServer is New over an API that is a ServeMux of its own, as the
// real one is, with the latency histogram wired.
func metricsServer(t *testing.T) (http.Handler, *metrics.Registry, *metrics.HistogramVec) {
	t.Helper()
	log, _ := testLogger()
	api := http.NewServeMux()
	api.HandleFunc("GET /api/v1/threads/{id}", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("thread"))
	})
	api.HandleFunc("GET /api/v1/boom", func(http.ResponseWriter, *http.Request) { panic("boom") })
	api.Handle("/api/v1/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		problem.Write(w, r, problem.CodeNotFound)
	}))
	g := allGroups()
	g.API = api
	reg := metrics.NewRegistry()
	h := reg.HistogramVec("ketju_http_request_duration_seconds", "Latency.", []float64{1}, "route", "method", "code")
	return New(g, Options{Logger: log, Requests: h}), reg, h
}

func scrape(t *testing.T, reg *metrics.Registry) string {
	t.Helper()
	var b strings.Builder
	if err := reg.WriteText(&b); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// TestRequestLatencyLabelsArePatterns: a request is labelled with the most
// specific pattern that matched, never its path. A panic is labelled with
// its route and a 5xx.
func TestRequestLatencyLabelsArePatterns(t *testing.T) {
	srv, reg, _ := metricsServer(t)
	const id = "3yymw47g00001"
	for _, c := range []struct {
		method, path string
		code         int
	}{
		{http.MethodGet, "/api/v1/threads/" + id + "?page=2", 200},
		{http.MethodGet, "/api/v1/boom", 500},
		{http.MethodGet, "/api/v1/nothing-here", 404},
		{http.MethodGet, "/f/secret-forum/t/" + id, 200},
		{http.MethodGet, "/healthz", 501},
	} {
		if rec := do(srv, c.method, c.path); rec.Code != c.code {
			t.Fatalf("%s %s = %d, want %d", c.method, c.path, rec.Code, c.code)
		}
	}
	got := scrape(t, reg)
	for _, want := range []string{
		`{route="/api/v1/threads/{id}",method="GET",code="2xx"`,
		`{route="/api/v1/boom",method="GET",code="5xx"`,
		`{route="/api/v1/",method="GET",code="4xx"`,
		`{route="/",method="GET",code="2xx"`,
		`{route="/healthz",method="GET",code="5xx"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the scrape has no %s:\n%s", want, got)
		}
	}
	for _, leak := range []string{id, "secret-forum", "page=2", "nothing-here"} {
		if strings.Contains(got, leak) {
			t.Errorf("the scrape carries %q:\n%s", leak, got)
		}
	}
}

// TestRequestLatencyLabelsAreBounded (Terva review 1943): five hundred
// requests with distinct custom methods, to matched and unmatched paths,
// are all recorded as "other", and the series stay within what the fixed
// routes, methods and status classes allow.
func TestRequestLatencyLabelsAreBounded(t *testing.T) {
	srv, reg, h := metricsServer(t)
	for i := range 500 {
		path := fmt.Sprintf("/api/v1/threads/%d", i)
		if i%2 == 1 {
			path = fmt.Sprintf("/x%d/y", i)
		}
		req := httptest.NewRequest("M"+fmt.Sprint(i), path, nil)
		srv.ServeHTTP(httptest.NewRecorder(), req)
	}
	got := scrape(t, reg)
	if !strings.Contains(got, `method="other"`) {
		t.Errorf("no custom method was recorded as other:\n%s", got)
	}
	if strings.Contains(got, `method="M`) {
		t.Errorf("a custom method reached a label:\n%s", got)
	}
	// Routes: the thread pattern, the API catch-all, the root, unmatched;
	// methods: 8; classes: 5.
	if n, max := h.Series(), 4*8*5; n > max {
		t.Errorf("%d series after 500 requests, more than the %d the fixed sets allow", n, max)
	}
	if n := h.Series(); n > 10 {
		t.Errorf("%d series for two paths and one method class; a request value reached a label:\n%s", n, got)
	}
}

// TestMethodAndCodeLabels: every label value comes from a fixed set.
func TestMethodAndCodeLabels(t *testing.T) {
	for m, want := range map[string]string{"GET": "GET", "OPTIONS": "OPTIONS", "get": "other", "PROPFIND": "other",
		"CONNECT": "other", "TRACE": "other", "": "other"} {
		if got := MethodLabel(m); got != want {
			t.Errorf("MethodLabel(%q) = %q, want %q", m, got, want)
		}
	}
	for code, want := range map[int]string{200: "2xx", 304: "3xx", 404: "4xx", 503: "5xx", 101: "1xx", 0: "other", 999: "other"} {
		if got := CodeLabel(code); got != want {
			t.Errorf("CodeLabel(%d) = %q, want %q", code, got, want)
		}
	}
}

// TestMetricsGuardOnTheMainListener: the main listener answers /metrics with
// a 404 problem, whatever the method, and never the metrics; the endpoint
// is only on metrics_listen (docs/02).
func TestMetricsGuardOnTheMainListener(t *testing.T) {
	srv, reg, _ := metricsServer(t)
	do(srv, http.MethodGet, "/api/v1/threads/1") // something to export
	for _, m := range []string{http.MethodGet, http.MethodHead} {
		rec := do(srv, m, MetricsPath)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s /metrics on the main listener = %d, want 404", m, rec.Code)
		}
		if strings.Contains(rec.Body.String(), "ketju_") || strings.Contains(rec.Body.String(), "<html") {
			t.Errorf("%s /metrics on the main listener served:\n%s", m, rec.Body)
		}
		if ct := rec.Header().Get("Content-Type"); ct == metrics.ContentType {
			t.Errorf("%s /metrics answered as metrics", m)
		}
	}
	if s := scrape(t, reg); !strings.Contains(s, "ketju_http_request_duration_seconds") {
		t.Fatalf("the fixture recorded nothing:\n%s", s)
	}
}

// TestEventsStreamIsNotTimed: the live-update stream stays out of the
// histogram, since it lasts as long as the tab.
func TestEventsStreamIsNotTimed(t *testing.T) {
	srv, _, h := metricsServer(t)
	do(srv, http.MethodGet, "/events")
	if n := h.Series(); n != 0 {
		t.Errorf("/events added %d series", n)
	}
}
