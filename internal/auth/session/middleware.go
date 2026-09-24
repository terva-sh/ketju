package session

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/logging"
	"github.com/terva-sh/ketju/internal/problem"
)

// Middleware resolves the request's session, for web.Options.Session (chain
// position 6: after the rate limiter, so a flood is refused before it costs
// a query, and before CSRF).
//
// A request with no cookie, a cookie that cannot be a session id, or one
// that names no live session is anonymous, and a stale cookie is cleared. A
// database that cannot answer is a 500, not an anonymous request: quietly
// signing everyone out during an outage would look like a bug in sign-in.
//
// The touch that slides last_used_at runs only when it is due, and its
// failure is logged rather than failing the request, because it is a
// freshness hint and not a gate.
func Middleware(store *Store, log *slog.Logger) func(http.Handler) http.Handler {
	if log == nil {
		log = slog.Default()
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c, err := r.Cookie(CookieName)
			if err != nil {
				next.ServeHTTP(w, r)
				return
			}
			sess, err := store.Lookup(r.Context(), c.Value)
			if errors.Is(err, ErrNoSession) {
				http.SetCookie(w, ClearCookie())
				next.ServeHTTP(w, r)
				return
			}
			if err != nil {
				log.ErrorContext(r.Context(), "session lookup failed", "error", err)
				problem.Write(w, r, problem.CodeInternal)
				return
			}
			if _, err := store.Touch(r.Context(), sess); err != nil {
				log.WarnContext(r.Context(), "session touch failed", "error", err)
			}
			next.ServeHTTP(w, r.WithContext(withSession(r.Context(), sess)))
		})
	}
}

func withSession(ctx context.Context, sess Session) context.Context {
	v := sess.Viewer()
	ctx = domain.WithViewer(ctx, v)
	if v.UserID != 0 {
		ctx = logging.ContextWithViewer(ctx, v.UserID)
	}
	return context.WithValue(ctx, sessionKey{}, sess)
}
