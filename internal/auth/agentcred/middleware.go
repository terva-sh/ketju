package agentcred

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/logging"
	"github.com/terva-sh/ketju/internal/problem"
)

// Middleware resolves a bearer credential, and hands every request without
// one to sessions, the session middleware (session.Middleware). serve passes
// the composition as web.Options.Session, so it runs at chain position 6,
// after the per-address limiter and before the pre-user gate and CSRF.
//
// An Authorization header decides the request. When one is present the
// session middleware does not run, so the cookie is not read, no cookie is
// set or cleared, and no session row is read, touched or written. The
// header must be "Bearer <token>" naming a live credential of an active
// agent. Anything else, a malformed header, another scheme, an unknown or
// revoked token, or an agent that is not active, is 401 unauthenticated,
// not an anonymous request: an agent with a bad token learns so at once,
// rather than reading as a stranger.
//
// A database that cannot answer is a 500, as it is for a session. The
// touch that records the credential's last use runs only when due, and its
// failure is logged rather than failing the request.
//
// The token is never logged. The request log's redactor drops the
// Authorization header (internal/logging), and nothing here names it.
func Middleware(store *Store, sessions func(http.Handler) http.Handler, log *slog.Logger) func(http.Handler) http.Handler {
	if log == nil {
		log = slog.Default()
	}
	return func(next http.Handler) http.Handler {
		viaSession := next
		if sessions != nil {
			viaSession = sessions(next)
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			values, present := r.Header["Authorization"]
			if !present {
				viaSession.ServeHTTP(w, r)
				return
			}
			token, ok := bearer(values)
			if !ok {
				refuse(w, r)
				return
			}
			c, err := store.Lookup(r.Context(), token)
			if errors.Is(err, ErrNoCredential) {
				refuse(w, r)
				return
			}
			if err != nil {
				log.ErrorContext(r.Context(), "agent credential lookup failed", "error", err)
				problem.Write(w, r, problem.CodeInternal)
				return
			}
			if _, err := store.Touch(r.Context(), c); err != nil {
				log.WarnContext(r.Context(), "agent credential touch failed", "error", err)
			}
			ctx := domain.WithViewer(r.Context(), c.Viewer())
			ctx = logging.ContextWithViewer(ctx, c.UserID)
			next.ServeHTTP(w, r.WithContext(With(ctx, c)))
		})
	}
}

// bearer reads the token from the Authorization header's values: exactly
// one value, of the Bearer scheme (case-insensitive, RFC 9110 §11.1), with
// a token after one space.
func bearer(values []string) (string, bool) {
	if len(values) != 1 {
		return "", false
	}
	scheme, token, ok := strings.Cut(values[0], " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") || token == "" || strings.ContainsAny(token, " \t") {
		return "", false
	}
	return token, true
}

// refuse answers a request whose Authorization header authenticates nobody.
func refuse(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
	problem.Write(w, r, problem.CodeUnauthenticated)
}
