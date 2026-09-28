// Package auth is /auth/*: the OIDC sign-in's start and callback, and
// sign-out (docs/05 "Login flow", TKT-01M29C7ZZ9).
//
// Everything a sign-in needs between start and callback -- which provider,
// the state, the nonce, the PKCE verifier, where to go afterwards -- rides
// in one sealed, short-lived cookie rather than a table (internal/auth/seal),
// so an abandoned sign-in writes nothing. The callback clears that cookie
// whatever happens, which makes it single use: a second callback with the
// same state finds nothing to match.
//
// A stateless cookie cannot make the nonce strictly one-time. Replay is
// bounded instead by the cookie's ten minutes, by its being cleared on
// first use, and by the IdP's codes being single use.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/terva-sh/ketju/internal/auth/oidc"
	"github.com/terva-sh/ketju/internal/auth/seal"
	"github.com/terva-sh/ketju/internal/auth/session"
	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/service/identity"
)

// FlowCookie holds a sign-in's state between start and callback. The
// __Host- prefix keeps it to this host, over TLS, at Path=/.
const FlowCookie = "__Host-ketju_login"

// flowPurpose binds a sealed flow to this cookie and no other.
const flowPurpose = "oidc-login"

// FlowTTL is how long a sign-in may take at the IdP.
const FlowTTL = 10 * time.Minute

// Flow is what a sign-in's start hands its callback.
type Flow struct {
	Provider string `json:"p"`
	State    string `json:"s"`
	Nonce    string `json:"n"`
	Verifier string `json:"v"`
	ReturnTo string `json:"r"`
}

// Providers is the part of *oidc.Registry the handlers use.
type Providers interface {
	Lookup(key string) (*oidc.Provider, bool)
}

// Login is the part of *identity.Service the callback and the registration
// screen use.
type Login interface {
	Login(ctx context.Context, id *oidc.Identity, viewer domain.Viewer, meta session.Meta, replaces string) (identity.Result, error)
	RegistrationOpen(ctx context.Context) (bool, error)
	Register(ctx context.Context, c identity.Claims, handle string, meta session.Meta, replaces string) (identity.Result, error)
}

// Sessions is the part of *session.Store the handlers use.
type Sessions interface {
	Revoke(ctx context.Context, token string) error
}

// Handler serves /auth/*.
type Handler struct {
	// Providers is nil when no provider is configured: every sign-in is then
	// refused as unavailable.
	Providers Providers
	Sealer    *seal.Sealer
	Login     Login
	Sessions  Sessions
	// HTTPClient reaches the IdP's token endpoint; nil is the one the
	// registry was built with.
	HTTPClient *http.Client
	Logger     *slog.Logger
}

// New returns the /auth/* routes.
func New(h *Handler) http.Handler {
	if h.Logger == nil {
		h.Logger = slog.Default()
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /auth/{provider}/start", h.start)
	mux.HandleFunc("GET /auth/{provider}/callback", h.callback)
	// The handle screen of open registration (register.go).
	mux.HandleFunc("GET /auth/register", h.registerForm)
	mux.HandleFunc("POST /auth/register", h.register)
	// POST, so a page elsewhere cannot sign a reader out with an image tag;
	// the CSRF check in front of every write (internal/web) holds it to
	// this site.
	mux.HandleFunc("POST /auth/logout", h.logout)
	return mux
}

func (h *Handler) provider(key string) (*oidc.Provider, bool) {
	if h.Providers == nil {
		return nil, false
	}
	return h.Providers.Lookup(key)
}

// start begins a sign-in: fresh state, nonce and PKCE verifier, sealed into
// the flow cookie with where to go afterwards, and off to the IdP.
func (h *Handler) start(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("provider")
	p, ok := h.provider(key)
	if !ok {
		writePage(w, http.StatusNotFound, pageNoProvider)
		return
	}
	f := Flow{Provider: key, State: random(), Nonce: random(), Verifier: random(),
		ReturnTo: SafeReturnPath(r.URL.Query().Get("return_to"))}
	sealed, err := h.Sealer.Seal(flowPurpose, f)
	if err != nil {
		h.Logger.ErrorContext(r.Context(), "sealing a sign-in failed", "error", err)
		writePage(w, http.StatusInternalServerError, pageFailed)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: FlowCookie, Value: sealed, Path: "/", MaxAge: int(FlowTTL / time.Second),
		Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, p.AuthCodeURL(f.State, f.Nonce, f.Verifier), http.StatusFound)
}

// callback finishes a sign-in. Every refusal before the IdP's answer is
// trusted says only "sign-in failed" to the reader and logs why.
func (h *Handler) callback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	key := r.PathValue("provider")
	// Single use, whatever follows.
	http.SetCookie(w, &http.Cookie{Name: FlowCookie, Value: "", Path: "/", MaxAge: -1,
		Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	fail := func(reason string, err error) {
		h.Logger.WarnContext(ctx, "sign-in refused", "provider", key, "reason", reason, "error", err)
		writePage(w, http.StatusBadRequest, pageFailed)
	}

	var f Flow
	c, err := r.Cookie(FlowCookie)
	if err != nil {
		fail("no sign-in in progress", err)
		return
	}
	if err := h.Sealer.Open(flowPurpose, c.Value, FlowTTL, &f); err != nil {
		fail("the sign-in state did not open", err)
		return
	}
	if f.Provider != key {
		fail("the callback is for another provider than the sign-in started with", nil)
		return
	}
	q := r.URL.Query()
	if subtle.ConstantTimeCompare([]byte(q.Get("state")), []byte(f.State)) != 1 {
		fail("the state does not match", nil)
		return
	}
	if e := q.Get("error"); e != "" {
		h.Logger.InfoContext(ctx, "sign-in cancelled at the provider", "provider", key, "error", e)
		writePage(w, http.StatusBadRequest, pageCancelled)
		return
	}
	p, ok := h.provider(key)
	if !ok {
		writePage(w, http.StatusNotFound, pageNoProvider)
		return
	}
	id, err := p.Exchange(ctx, h.HTTPClient, q.Get("code"), f.Nonce, f.Verifier)
	if err != nil {
		fail("the provider's answer did not verify", err)
		return
	}
	if !p.EmailAllowed(id.Email) {
		h.Logger.InfoContext(ctx, "sign-in refused by allowed_email_domains", "provider", key)
		writePage(w, http.StatusForbidden, pageNotAllowed)
		return
	}

	// The session this browser holds, if any, which a sign-in replaces.
	var replaces string
	if old, err := r.Cookie(session.CookieName); err == nil {
		replaces = old.Value
	}
	res, err := h.Login.Login(ctx, id, domain.ViewerFrom(ctx), session.Meta{
		IP: remoteIP(r), UserAgent: r.UserAgent(), Provider: key}, replaces)
	if err != nil {
		h.Logger.ErrorContext(ctx, "sign-in failed", "provider", key, "error", err)
		writePage(w, http.StatusInternalServerError, pageFailed)
		return
	}
	switch res.Outcome {
	case identity.SignedIn:
		http.SetCookie(w, session.Cookie(res.Token, res.Session.ExpiresAt))
		w.Header().Set("Cache-Control", "no-store")
		http.Redirect(w, r, f.ReturnTo, http.StatusSeeOther)
	case identity.Unavailable:
		h.Logger.InfoContext(ctx, "sign-in refused: the account is not active", "provider", key, "status", res.Status)
		writePage(w, http.StatusForbidden, pageUnavailable)
	case identity.Register:
		h.beginRegistration(w, r, id, f.ReturnTo)
	case identity.Link:
		writePage(w, http.StatusNotImplemented, pageLinking)
	case identity.Applicant:
		writePage(w, http.StatusNotImplemented, pageApplicant)
	default:
		h.Logger.ErrorContext(ctx, "sign-in ended in an unknown outcome", "outcome", res.Outcome)
		writePage(w, http.StatusInternalServerError, pageFailed)
	}
}

// logout ends this browser's session and sends it home.
func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(session.CookieName); err == nil {
		if err := h.Sessions.Revoke(r.Context(), c.Value); err != nil {
			h.Logger.ErrorContext(r.Context(), "signing out failed", "error", err)
			writePage(w, http.StatusInternalServerError, pageData{"Sign-out failed",
				"The session could not be ended. Please try again.", "/", "Go to the forum"})
			return
		}
	}
	http.SetCookie(w, session.ClearCookie())
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// random is 256 bits for a state, nonce or PKCE verifier. A verifier must be
// 43 to 128 characters (RFC 7636), and this is 43.
func random() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand does not fail on a supported platform
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func remoteIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
