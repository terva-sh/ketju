package metrics

import (
	"bufio"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
)

var update = flag.Bool("update", false, "rewrite testdata/exposition.golden")

// TestTextExposition: a registry holding each metric type writes the text
// format as the golden file has it, sorted and escaped, and every line
// parses back.
func TestTextExposition(t *testing.T) {
	r := NewRegistry()
	c := r.Counter("ketju_things_total", "Things counted.")
	c.Add(3)
	g := r.Gauge("ketju_level_bytes", "A level, with a backslash \\ and a\nnewline in its help.")
	g.Set(1.5)
	r.CounterFunc("ketju_labelled_total", "Counted per kind.", []string{"kind"}, func(emit func(Sample)) {
		emit(Sample{Labels: []string{"zeta"}, Value: 2})
		emit(Sample{Labels: []string{`quo"te\back`}, Value: 1})
	})
	r.GaugeFunc("ketju_read_at_scrape", "Read at scrape time.", nil, func(emit func(Sample)) {
		emit(Sample{Value: 7})
	})
	h := r.HistogramVec("ketju_wait_seconds", "Waits.", []float64{0.01, 0.1, 1}, "file")
	h.With("log").Observe(0.005)
	h.With("log").Observe(0.05)
	h.With("log").Observe(5)
	h.With("index").Observe(0.1)

	var b strings.Builder
	if err := r.WriteText(&b); err != nil {
		t.Fatal(err)
	}
	got := b.String()
	path := filepath.Join("testdata", "exposition.golden")
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("exposition differs from %s (run with -update to rewrite):\n%s", path, got)
	}
	parse(t, got)
}

// line is one sample line of the text format.
var line = regexp.MustCompile(`^([a-z_][a-z0-9_]*)(\{(?:[a-z_][a-z0-9_]*="(?:[^"\\\n]|\\["\\n])*",?)*\})? (\S+)$`)

// parse checks every line of an exposition: a comment is HELP or TYPE, and
// a sample is a name, optional labels, and a number. It returns the samples
// by their name and label text.
func parse(t *testing.T, text string) map[string]float64 {
	t.Helper()
	out := map[string]float64{}
	sc := bufio.NewScanner(strings.NewReader(text))
	for sc.Scan() {
		l := sc.Text()
		if strings.HasPrefix(l, "# HELP ") || strings.HasPrefix(l, "# TYPE ") {
			continue
		}
		m := line.FindStringSubmatch(l)
		if m == nil {
			t.Errorf("line does not parse: %q", l)
			continue
		}
		v, err := strconv.ParseFloat(m[3], 64)
		if err != nil {
			t.Errorf("value does not parse: %q", l)
		}
		out[m[1]+m[2]] = v
	}
	return out
}

// TestHistogramConcurrent: observations from many goroutines are all
// counted, in the right buckets, and sum exactly.
func TestHistogramConcurrent(t *testing.T) {
	r := NewRegistry()
	h := r.HistogramVec("ketju_x_seconds", "x", []float64{1, 2}, "k")
	const workers, each = 16, 1000
	var wg sync.WaitGroup
	for w := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range each {
				// Two label sets, so new series are made under contention.
				h.With(fmt.Sprint(w % 2)).Observe(float64(i % 3))
			}
		}()
	}
	// Scrape while they observe, which the race detector checks.
	for range 20 {
		var b strings.Builder
		_ = r.WriteText(&b)
	}
	wg.Wait()
	var b strings.Builder
	if err := r.WriteText(&b); err != nil {
		t.Fatal(err)
	}
	s := parse(t, b.String())
	var count, sum, le1 float64
	for _, k := range []string{"0", "1"} {
		count += s[`ketju_x_seconds_count{k="`+k+`"}`]
		sum += s[`ketju_x_seconds_sum{k="`+k+`"}`]
		le1 += s[`ketju_x_seconds_bucket{k="`+k+`",le="1"}`]
	}
	if count != workers*each {
		t.Errorf("count = %v, want %d", count, workers*each)
	}
	// Values cycle 0, 1, 2: each worker observes 334 zeros, 333 ones and
	// 333 twos.
	if want := float64(workers * (333 + 2*333)); sum != want {
		t.Errorf("sum = %v, want %v", sum, want)
	}
	if want := float64(workers * (334 + 333)); le1 != want {
		t.Errorf("le=1 = %v, want %v", le1, want)
	}
}

// TestRegisterRefusesBadNames: a name or label outside the rules panics at
// registration.
func TestRegisterRefusesBadNames(t *testing.T) {
	for _, c := range []struct {
		name string
		reg  func(r *Registry)
	}{
		{"no prefix", func(r *Registry) { r.Counter("requests_total", "h") }},
		{"uppercase", func(r *Registry) { r.Gauge("ketju_Open", "h") }},
		{"counter without _total", func(r *Registry) { r.Counter("ketju_requests", "h") }},
		{"gauge with _total", func(r *Registry) { r.Gauge("ketju_open_total", "h") }},
		{"histogram suffix", func(r *Registry) { r.Gauge("ketju_open_count", "h") }},
		{"no help", func(r *Registry) { r.Gauge("ketju_open", " ") }},
		{"le label", func(r *Registry) { r.HistogramVec("ketju_x_seconds", "h", []float64{1}, "le") }},
		{"bad label", func(r *Registry) {
			r.GaugeFunc("ketju_open", "h", []string{"Bad"}, func(func(Sample)) {})
		}},
		{"repeated label", func(r *Registry) {
			r.GaugeFunc("ketju_open", "h", []string{"a", "a"}, func(func(Sample)) {})
		}},
		{"unsorted buckets", func(r *Registry) { r.HistogramVec("ketju_x_seconds", "h", []float64{2, 1}) }},
		{"twice", func(r *Registry) { r.Gauge("ketju_open", "h"); r.Gauge("ketju_open", "h") }},
	} {
		t.Run(c.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Error("registration did not panic")
				}
			}()
			c.reg(NewRegistry())
		})
	}
}

// TestHandlerServesOnlyMetrics: the handler answers /metrics for GET and
// HEAD, uncached, and nothing else.
func TestHandlerServesOnlyMetrics(t *testing.T) {
	r := NewRegistry()
	r.Counter("ketju_things_total", "Things.").Inc()
	h := r.Handler()
	for _, c := range []struct {
		method, path string
		code         int
	}{
		{http.MethodGet, "/metrics", 200},
		{http.MethodHead, "/metrics", 200},
		{http.MethodPost, "/metrics", 405},
		{http.MethodGet, "/", 404},
		{http.MethodGet, "/metrics/x", 404},
		{http.MethodGet, "/api/v1/me", 404},
	} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(c.method, c.path, nil))
		if w.Code != c.code {
			t.Errorf("%s %s = %d, want %d", c.method, c.path, w.Code, c.code)
		}
		if c.code != 200 {
			continue
		}
		if got := w.Header().Get("Cache-Control"); got != "no-store" {
			t.Errorf("%s: Cache-Control = %q", c.method, got)
		}
		if got := w.Header().Get("Content-Type"); got != ContentType {
			t.Errorf("%s: Content-Type = %q", c.method, got)
		}
		if c.method == http.MethodGet && !strings.Contains(w.Body.String(), "ketju_things_total 1\n") {
			t.Errorf("GET body:\n%s", w.Body)
		}
		if c.method == http.MethodHead && w.Body.Len() != 0 {
			t.Errorf("HEAD wrote a body")
		}
	}
}
