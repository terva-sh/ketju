// Package logging builds the process logger and the request-logging
// middleware.
//
// # What must never be logged
//
// No post source, no session id, no token, and no secret. A forum's logs leave
// the machine: they go to an aggregator, into a support ticket, onto a terminal
// somebody screenshots. A post body that was soft-deleted in the thread log is
// not deleted if a copy is sitting in a log line, and an aggregator has no
// concept of a moderator's delete.
//
// That rule is not left to whoever writes the next log call. Three mechanisms
// hold it up, and each one covers a gap the others do not:
//
//   - [Redact] drops the value of any attribute whose key names something
//     dangerous, wherever it appears, including inside groups and inside
//     attributes attached with With. A handler cannot inspect meaning, but it
//     can refuse to print a value called "token".
//   - Types that carry secrets implement [slog.LogValuer] and redact
//     themselves, which is what config.Secret already does. This is the only
//     one of the three that survives being renamed to an innocuous key.
//   - [Middleware] logs a request's path and never its query string, because
//     the OIDC callback arrives as ?code=...&state=..., and an authorization
//     code in a log file is a login somebody else can complete.
//
// The first is a denylist and denylists are never complete. It is here because
// it costs nothing and catches the common mistake; the second is the one to
// reach for when adding a type that must not be printed.
package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/terva-sh/ketju/internal/config"
)

// Redacted replaces the value of an attribute that must not be logged. It is
// the same marker internal/config uses, so a redacted value reads the same
// wherever it came from.
const Redacted = "[redacted]"

// Options configures New. The zero value writes to stderr.
type Options struct {
	// Writer receives the log output. Defaults to os.Stderr.
	Writer io.Writer

	// IsTerminal decides what "auto" means. Defaults to checking whether
	// Writer is a character device, which is what distinguishes a developer's
	// terminal from a pipe into a log collector.
	IsTerminal func(io.Writer) bool

	// AddSource attaches the calling file and line. Off by default: it costs a
	// stack walk per record and says nothing an operator needs.
	AddSource bool
}

// New builds the process logger.
func New(cfg config.Log, opts Options) (*slog.Logger, error) {
	w := opts.Writer
	if w == nil {
		w = os.Stderr
	}
	level, err := ParseLevel(cfg.Level)
	if err != nil {
		return nil, err
	}

	format := cfg.Format
	if format == "" {
		format = config.DefaultLogFormat
	}
	if format == "auto" {
		isTerm := opts.IsTerminal
		if isTerm == nil {
			isTerm = isCharDevice
		}
		if isTerm(w) {
			format = "text"
		} else {
			format = "json"
		}
	}

	hopts := &slog.HandlerOptions{Level: level, AddSource: opts.AddSource}
	var h slog.Handler
	switch format {
	case "json":
		h = slog.NewJSONHandler(w, hopts)
	case "text":
		h = slog.NewTextHandler(w, hopts)
	default:
		return nil, fmt.Errorf("logging: %q is not a format; use json, text or auto", cfg.Format)
	}
	return slog.New(Redact(h)), nil
}

// ParseLevel turns a configured level name into a slog level.
func ParseLevel(name string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "info":
		return slog.LevelInfo, nil
	case "debug":
		return slog.LevelDebug, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	}
	return 0, fmt.Errorf("logging: %q is not a level; use debug, info, warn or error", name)
}

// isCharDevice reports whether w is a terminal.
//
// Checking the file mode avoids a dependency on x/term for the one bit of it
// this package needs. A pipe, a file and a socket are all not character
// devices, which is exactly the set that should get JSON.
func isCharDevice(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// sensitiveKeys are attribute names whose values never reach the output.
//
// Two notes on what is in here and what is not.
//
// "code", "state" and "nonce" are in, and they are generic enough that they
// will occasionally redact something harmless. That is the right trade: in this
// system those three names belong to the OIDC flow, where "code" is an
// authorization code somebody could redeem. A status code should be logged as
// "status" and an error code as "error_code", both of which pass through.
//
// A bare "key" is deliberately NOT in, because an OIDC provider key is the
// thing an operator most needs to see in a log line naming a broken provider.
// Only the compound forms -- api_key, private_key, secret_key -- are redacted.
// A bare "auth" is out for the same reason: it is a natural name for a *group*
// holding a provider, a subject and a token, and redacting it wholesale would
// throw away the two useful fields to hide the one that is caught anyway.
// "authorization", the header, is in.
//
// A group whose own key is sensitive is redacted whole rather than descended
// into, because the alternative leaks: slog.Group("token", "value", secret)
// would otherwise be walked, and "value" is not a name any denylist contains.
// The cost is that a group deliberately named "session" loses fields that might
// have been safe, which is the right direction to be wrong in.
var sensitiveKeys = map[string]bool{
	// Content. A post body in an aggregator outlives the moderator's delete.
	"source": true, "post_source": true, "body": true, "content": true,
	"markdown": true, "html": true, "draft": true, "excerpt": true,

	// Session and transport credentials.
	"session": true, "session_id": true, "sid": true,
	"cookie": true, "set-cookie": true, "set_cookie": true,
	"authorization": true,

	// OIDC and OAuth.
	"token": true, "access_token": true, "id_token": true, "refresh_token": true,
	"code": true, "code_verifier": true, "state": true, "nonce": true,
	"client_secret": true, "assertion": true,

	// Anything that says it is a secret.
	"secret": true, "password": true, "passphrase": true, "credential": true,
	"api_key": true, "private_key": true, "secret_key": true, "signing_key": true,
}

// Redact wraps a handler so that dangerous attribute values never reach it.
func Redact(h slog.Handler) slog.Handler { return &redactor{inner: h} }

type redactor struct{ inner slog.Handler }

func (r *redactor) Enabled(ctx context.Context, l slog.Level) bool {
	return r.inner.Enabled(ctx, l)
}

func (r *redactor) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &redactor{inner: r.inner.WithAttrs(redactAttrs(attrs))}
}

func (r *redactor) WithGroup(name string) slog.Handler {
	return &redactor{inner: r.inner.WithGroup(name)}
}

func (r *redactor) Handle(ctx context.Context, rec slog.Record) error {
	// Record.Attrs cannot be edited in place, so build a replacement with the
	// same message, level and time.
	out := slog.NewRecord(rec.Time, rec.Level, rec.Message, rec.PC)
	rec.Attrs(func(a slog.Attr) bool {
		out.AddAttrs(redactAttr(a))
		return true
	})
	return r.inner.Handle(ctx, out)
}

func redactAttrs(attrs []slog.Attr) []slog.Attr {
	out := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		out[i] = redactAttr(a)
	}
	return out
}

func redactAttr(a slog.Attr) slog.Attr {
	// Resolve first, and it is worth saying what this does and does not buy,
	// because the obvious answer is wrong. It is NOT what protects a type that
	// redacts itself: slog's own handlers resolve a LogValuer before formatting,
	// so config.Secret stays redacted whether or not this line exists --
	// removing it does not make that test fail.
	//
	// What it does is let a group *returned* by a LogValuer be walked like any
	// other. Unresolved, such a value has kind LogValuer rather than Group, so
	// the descent below skips it and the inner handler expands it afterwards,
	// printing whatever sensitive keys it contains.
	a.Value = a.Value.Resolve()

	if sensitiveKeys[strings.ToLower(a.Key)] {
		return slog.String(a.Key, Redacted)
	}
	if a.Value.Kind() == slog.KindGroup {
		return slog.Attr{Key: a.Key, Value: slog.GroupValue(redactAttrs(a.Value.Group())...)}
	}
	return a
}

// viewerKey types the context value so nothing else can collide with it.
type viewerKey struct{}

// ContextWithViewer records who is making this request, for the request log.
//
// The session middleware sets it once authentication has happened; everything
// downstream reads it. It is an id rather than a user because a log line wants
// a number and because holding a user here would invite handlers to read their
// viewer from the logging package.
func ContextWithViewer(ctx context.Context, userID int64) context.Context {
	return context.WithValue(ctx, viewerKey{}, userID)
}

// ViewerFrom returns the viewer id recorded by ContextWithViewer.
func ViewerFrom(ctx context.Context) (int64, bool) {
	id, ok := ctx.Value(viewerKey{}).(int64)
	return id, ok
}

// Middleware logs one line per request.
//
// The line carries method, path, status, duration and, when the request is
// authenticated, the viewer id. It does not carry the query string, and that
// is the point rather than an omission: /auth/{provider}/callback arrives as
// ?code=...&state=..., so logging a raw request URI would write an
// authorization code into every log aggregator the deployment feeds. One is
// enough to complete somebody else's login.
func Middleware(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &recorder{ResponseWriter: w, status: http.StatusOK}

			defer func() {
				attrs := []any{
					"method", r.Method,
					// Path only. See the comment above.
					"path", r.URL.Path,
					"status", rec.status,
					"bytes", rec.written,
					"duration_ms", time.Since(start).Milliseconds(),
				}
				if id, ok := ViewerFrom(r.Context()); ok {
					attrs = append(attrs, "viewer", id)
				}
				log.LogAttrs(r.Context(), levelFor(rec.status), "request", toAttrs(attrs)...)
			}()

			next.ServeHTTP(rec, r)
		})
	}
}

// levelFor keeps a wall of 404s out of the error stream. A 5xx is the server's
// fault and is worth waking up for; a 4xx is a client's and is not.
func levelFor(status int) slog.Level {
	switch {
	case status >= 500:
		return slog.LevelError
	case status >= 400:
		return slog.LevelWarn
	}
	return slog.LevelInfo
}

func toAttrs(kv []any) []slog.Attr {
	out := make([]slog.Attr, 0, len(kv)/2)
	for i := 0; i+1 < len(kv); i += 2 {
		out = append(out, slog.Any(kv[i].(string), kv[i+1]))
	}
	return out
}

// recorder captures the status and byte count without hiding the writer
// underneath it.
//
// Unwrap is what makes http.ResponseController work through this, so a handler
// can still flush an SSE stream or set a write deadline. Flush is implemented
// directly as well, because handlers written before ResponseController existed
// assert on http.Flusher and a wrapper that does not implement it turns a live
// event stream into a response that arrives all at once at the end, or never.
type recorder struct {
	http.ResponseWriter
	status  int
	written int64
	wrote   bool
}

func (r *recorder) WriteHeader(status int) {
	if r.wrote {
		return
	}
	r.wrote = true
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *recorder) Write(b []byte) (int, error) {
	if !r.wrote {
		r.wrote = true
	}
	n, err := r.ResponseWriter.Write(b)
	r.written += int64(n)
	return n, err
}

func (r *recorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

func (r *recorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
