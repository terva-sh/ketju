package web

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"runtime"
	"strings"

	"github.com/terva-sh/ketju/internal/config"
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

// MaxPostBody is the largest JSON request body the API accepts.
//
// 64 KB, fixed by docs/07. It is a constant rather than a setting because it
// is part of what the API promises a client: an installation that quietly
// raised it would accept posts another installation could not take.
//
// It is far below render.MaxSource (256 KiB), which is the renderer's own
// backstop, and further below the storage format's MaxFrameSize (1 MiB). Three
// limits at three layers, each guarding a different failure: this one bounds
// what a stranger can make the server read.
const MaxPostBody = 64 << 10

// UploadPath is the one route measured against the attachment cap rather than
// MaxPostBody. docs/07 puts attachment upload at POST /api/v1/blobs.
const UploadPath = APIPrefix + "blobs"

// Limits are the request body bounds.
type Limits struct {
	// Body caps every request that is not an upload. Defaults to MaxPostBody.
	Body int64

	// Upload caps an upload. Defaults to config's DefaultAttachmentMB.
	Upload int64
}

func (l Limits) withDefaults() Limits {
	if l.Body <= 0 {
		l.Body = MaxPostBody
	}
	if l.Upload <= 0 {
		l.Upload = int64(config.DefaultAttachmentMB) << 20
	}
	return l
}

// forPath returns the limit that applies to a path.
//
// Route-dependent rather than one global number, because a single limit has to
// be the larger of the two and that would let any endpoint accept an
// attachment-sized body. The choice is made here rather than in each handler
// for the reason the chain exists at all: a limit applied by each handler is a
// limit the next handler forgets.
func (l Limits) forPath(p string) int64 {
	if p == UploadPath || strings.HasPrefix(p, UploadPath+"/") {
		return l.Upload
	}
	return l.Body
}

// limitBody caps the request body before any handler reads it.
//
// Two mechanisms, because there are two ways to send too much:
//
//  1. An honest Content-Length over the limit is refused here, before a byte of
//     the body is read. This is what AC4 of TKT-01M29C8020 asks for -- a
//     problem response rather than a truncated read -- and it costs the server
//     nothing to answer.
//
//  2. A chunked body, or one whose Content-Length lies, cannot be judged up
//     front. MaxBytesReader is what makes the cap real there: the handler's
//     read fails with *http.MaxBytesError, which internal/problem maps to
//     payload-too-large, so the client still gets a problem response and not a
//     half-parsed body.
func limitBody(l Limits) func(http.Handler) http.Handler {
	l = l.withDefaults()
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			max := l.forPath(r.URL.Path)

			// ContentLength is -1 when unknown, which is not a violation.
			if r.ContentLength > max {
				problem.Write(w, r, problem.CodePayloadTooLarge)
				return
			}
			if r.Body != nil {
				r.Body = http.MaxBytesReader(w, r.Body, max)
			}
			next.ServeHTTP(w, r)
		})
	}
}
