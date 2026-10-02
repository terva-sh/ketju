package web

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/terva-sh/ketju/internal/metrics"
)

// MetricsPath is where a Prometheus scraper looks. The main listener never
// serves it: it answers 404 here, and the metrics are served only on
// metrics_listen (docs/02 "Observability", decided by the maintainer on
// 2026-10-02).
const MetricsPath = "/metrics"

// Unmatched is the route label of a request answered before any router
// matched it: a rate-limit refusal, a body over the limit, a CSRF refusal.
const Unmatched = "unmatched"

// methodLabels is every method recorded under its own name. A method is
// client input and can be any token, so anything else is "other", and no
// client can grow the series (Terva review 1943).
var methodLabels = map[string]bool{
	http.MethodGet: true, http.MethodHead: true, http.MethodPost: true, http.MethodPut: true,
	http.MethodPatch: true, http.MethodDelete: true, http.MethodOptions: true,
}

// MethodLabel is a request method as the latency histogram records it.
func MethodLabel(m string) string {
	if methodLabels[m] {
		return m
	}
	return "other"
}

// CodeLabel is a status as its class, "1xx" to "5xx".
func CodeLabel(status int) string {
	if status < 100 || status > 599 {
		return "other"
	}
	return string(rune('0'+status/100)) + "xx"
}

// routeSlot is where the innermost router to match a request writes the
// pattern it matched, for the latency middleware to read once the response
// is done.
type routeSlot struct{ pattern string }

type routeSlotKey struct{}

// routed wraps a ServeMux so the pattern it matched reaches the latency
// middleware. ServeMux sets r.Pattern on the request it was handed, so the
// pattern is read back after it returns; the innermost mux returns first,
// and a slot already written is kept, so the most specific pattern wins.
// It is deferred, so a handler that panics is still labelled.
func routed(mux *http.ServeMux) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if s, ok := r.Context().Value(routeSlotKey{}).(*routeSlot); ok && s.pattern == "" && r.Pattern != "" {
				s.pattern = routeLabel(r.Pattern)
			}
		}()
		mux.ServeHTTP(w, r)
	})
}

// routeLabel is a pattern without its method, which has a label of its own.
// A pattern is always one the code registered, never the request's path:
// /api/v1/threads/{id}, not the id.
func routeLabel(pattern string) string {
	if _, path, ok := strings.Cut(pattern, " "); ok {
		return path
	}
	return pattern
}

// requestMetrics observes each request's latency in h, labelled by route
// pattern, method and status class. The live-update stream is left out: it
// lasts as long as the tab does, and the hub counts it.
func requestMetrics(h *metrics.HistogramVec) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if h == nil {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/events" {
				next.ServeHTTP(w, r)
				return
			}
			start := time.Now()
			slot := &routeSlot{}
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			defer func() {
				route := slot.pattern
				if route == "" {
					route = Unmatched
				}
				h.With(route, MethodLabel(r.Method), CodeLabel(rec.status)).Observe(time.Since(start).Seconds())
			}()
			next.ServeHTTP(rec, r.WithContext(context.WithValue(r.Context(), routeSlotKey{}, slot)))
		})
	}
}

// statusRecorder captures the status without hiding the writer underneath:
// Unwrap keeps http.ResponseController working through it, as
// internal/logging's recorder does.
type statusRecorder struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (r *statusRecorder) WriteHeader(status int) {
	// An informational 1xx is not the response's status; the one after it
	// is.
	if !r.wrote && status >= 200 {
		r.wrote = true
		r.status = status
	}
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	r.wrote = true
	return r.ResponseWriter.Write(b)
}

func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
