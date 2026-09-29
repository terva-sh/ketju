// Package auth is /auth/*: the login page, the OIDC sign-in's start and
// callback, and sign-out (docs/05 "Login flow", TKT-01M29C7ZZ9).
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
	"github.com/terva-sh/ketju/internal/web"
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
	// LinkUser is the member who started this sign-in as a link, through
	// the "link a sign-in" action (link.go), and 0 for a plain sign-in. It
	// is sealed with the rest, so only that action can set it.
	LinkUser int64 `json:"l,omitempty"`
}

// Providers is the part of *oidc.Registry the handlers use.
type Providers interface {
	Lookup(key string) (*oidc.Provider, bool)
	// Enabled is the working providers in config order, which the login
	// page lists.
	Enabled() []*oidc.Provider
}

// Login is the part of *identity.Service the callback and the registration
// screen use.
type Login interface {
	Login(ctx context.Context, id *oidc.Identity, viewer domain.Viewer, linkUser int64, meta session.Meta, replaces string) (identity.Result, error)
	// Link attaches an identity to userID, a member who started the
	// sign-in as a link, and issues no session (link.go).
	Link(ctx context.Context, userID int64, c identity.Claims, ip string) (identity.LinkRefusal, error)
	// RegistrationOpenFor is whether someone new from provider may register
	// rather than apply; AutoApproves whether they skip the handle screen.
	RegistrationOpenFor(ctx context.Context, provider string) (bool, error)
	AutoApproves(provider string) bool
	Register(ctx context.Context, c identity.Claims, handle string, meta session.Meta, replaces string) (identity.Result, error)
	AutoRegister(ctx context.Context, c identity.Claims, meta session.Meta, replaces string) (identity.Result, identity.SkipReason, error)
	// ClaimsOf is what the registration cookie carries of id.
	ClaimsOf(id *oidc.Identity) identity.Claims
	BeginApplication(ctx context.Context, c identity.Claims, meta session.Meta, replaces string) (identity.Result, error)
	Reactivate(ctx context.Context, c identity.Claims, stamp int64, meta session.Meta, replaces string) (identity.Result, error)
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
	mux := http.NewServeMux()
	for _, rt := range Routes(h) {
		mux.HandleFunc(rt.Pattern(), rt.Handler)
	}
	return mux
}

// Routes is every route New serves, as a table a test can walk
// (TKT-01M29CKRG9).
func Routes(h *Handler) []web.Route {
	if h.Logger == nil {
		h.Logger = slog.Default()
	}
	get, post := http.MethodGet, http.MethodPost
	return []web.Route{
		// The login page: the client's "Sign in" (signin.go).
		{Method: get, Path: "/auth/signin", Handler: h.signIn},
		{Method: get, Path: "/auth/{provider}/start", Handler: h.start},
		{Method: get, Path: "/auth/{provider}/callback", Handler: h.callback},
		// Linking another sign-in to a member's account: the page that
		// offers it, and the start that marks the flow as a link (link.go).
		{Method: get, Path: LinkPath, Handler: h.linkPage},
		{Method: post, Path: "/auth/{provider}/link", Handler: h.startLink},
		// The handle screen of open registration (register.go).
		{Method: get, Path: RegisterPath, Handler: h.registerForm},
		{Method: post, Path: RegisterPath, Handler: h.register},
		// A deactivated member's Reactivate button (reactivate.go).
		{Method: post, Path: ReactivatePath, Handler: h.reactivate},
		// POST, so a page elsewhere cannot sign a reader out with an image
		// tag; the CSRF check in front of every write (internal/web) holds
		// it to this site.
		{Method: post, Path: "/auth/logout", Handler: h.logout},
	}
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
	if authURL, _, ok := h.begin(w, r, r.URL.Query().Get("return_to"), 0); ok {
		http.Redirect(w, r, authURL, http.StatusFound)
	}
}

// begin is what start and startLink share: a flow for r's provider, sealed
// into the flow cookie. It returns the IdP's authorization URL to send the
// browser to, and the provider. When ok is false it has answered the
// request. linkUser marks the flow as a link that member started, 0 for
// none.
func (h *Handler) begin(w http.ResponseWriter, r *http.Request, returnTo string, linkUser int64) (authURL string, p *oidc.Provider, ok bool) {
	key := r.PathValue("provider")
	p, ok = h.provider(key)
	if !ok {
		writePage(w, http.StatusNotFound, pageNoProvider)
		return "", nil, false
	}
	f := Flow{Provider: key, State: random(), Nonce: random(), Verifier: random(),
		ReturnTo: SafeReturnPath(returnTo), LinkUser: linkUser}
	sealed, err := h.Sealer.Seal(flowPurpose, f)
	if err != nil {
		h.Logger.ErrorContext(r.Context(), "sealing a sign-in failed", "error", err)
		writePage(w, http.StatusInternalServerError, pageFailed)
		return "", nil, false
	}
	http.SetCookie(w, &http.Cookie{Name: FlowCookie, Value: sealed, Path: "/", MaxAge: int(FlowTTL / time.Second),
		Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	w.Header().Set("Cache-Control", "no-store")
	return p.AuthCodeURL(f.State, f.Nonce, f.Verifier), p, true
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
	// The gate runs before anything is looked up or written, so it holds
	// for a returning member as for someone new.
	switch g := p.Gate(id); g {
	case oidc.GateOK:
	case oidc.GateUnverified:
		h.Logger.InfoContext(ctx, "sign-in refused by allowed_email_domains", "provider", key, "reason", g.String())
		writePage(w, http.StatusForbidden, pageUnverified)
		return
	default:
		h.Logger.InfoContext(ctx, "sign-in refused by allowed_email_domains", "provider", key, "reason", g.String())
		writePage(w, http.StatusForbidden, pageNotAllowed)
		return
	}

	// The session this browser holds, if any, which a sign-in replaces.
	var replaces string
	if old, err := r.Cookie(session.CookieName); err == nil {
		replaces = old.Value
	}
	res, err := h.Login.Login(ctx, id, domain.ViewerFrom(ctx), f.LinkUser, session.Meta{
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
		h.unavailable(w, r, res, id, f.ReturnTo)
	case identity.Register:
		h.beginRegistration(w, r, id, f.ReturnTo)
	case identity.Link:
		h.link(w, r, id, f)
	case identity.LinkRefused:
		h.Logger.InfoContext(ctx, "a link was refused", "provider", key, "reason", res.Refusal.String())
		writeLinkRefused(w, res.Refusal)
	case identity.Applicant:
		// An applicant coming back sees their status page, whatever
		// return_to said: it is the one page they may reach (docs/05
		// "mode = approval" step 4).
		http.SetCookie(w, session.Cookie(res.Token, res.Session.ExpiresAt))
		w.Header().Set("Cache-Control", "no-store")
		http.Redirect(w, r, web.ApplyStatusPath, http.StatusSeeOther)
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
