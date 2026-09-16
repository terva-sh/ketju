package web

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"runtime"
	"strings"

	"github.com/terva-sh/ketju/internal/problem"
)

// recoverPanics turns a panic into a 500 and keeps the server up.
//
// A panic in one handler is a bug in one request. Letting it reach net/http
// kills the connection and prints a stack to stderr with no request context
// attached, which is the least useful place for it; letting it reach main
// would take the process down and every other reader with it.
func recoverPanics(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				v := recover()
				if v == nil {
					return
				}
				// net/http panics with this value when a handler aborts a
				// response on purpose. Swallowing it would turn a deliberate
				// abort into a 500 and log a crash that did not happen, so it
				// goes back the way it came.
				if v == http.ErrAbortHandler {
					panic(v)
				}

				log.ErrorContext(r.Context(), "panic in handler",
					"panic", sanitizePanic(v),
					"method", r.Method,
					// Path only, never the query. See internal/logging.
					"path", r.URL.Path,
					"stack", stack(),
				)

				// If the handler already wrote a header this is too late and
				// the client gets a truncated body. Nothing can be done about
				// that here; what matters is that the process survives.
				problem.Write(w, r, problem.CodeInternal)
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// sanitizePanic renders a panic value for a log line.
//
// A panic value is frequently an error carrying whatever the handler was
// working on, which on this server can be a post body or a token. It is
// truncated hard: enough to identify the bug, not enough to be a copy of the
// data. The stack is the part that actually locates it.
func sanitizePanic(v any) string {
	const max = 256
	s := strings.TrimSpace(sprint(v))
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}

func sprint(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case error:
		return t.Error()
	default:
		return jsonish(v)
	}
}

func jsonish(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "unprintable panic value"
	}
	return string(b)
}

func stack() string {
	buf := make([]byte, 8<<10)
	return string(buf[:runtime.Stack(buf, false)])
}

// limitBody caps the request body before any handler reads it.
//
// MaxBytesReader is what makes the cap real: a handler that ignores
// Content-Length, or a client that lies about it, still cannot stream more
// than this into memory. A body at the limit fails on read rather than here,
// so the handler's own decoding error is what the client sees.
func limitBody(max int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Body != nil {
				r.Body = http.MaxBytesReader(w, r.Body, max)
			}
			next.ServeHTTP(w, r)
		})
	}
}
