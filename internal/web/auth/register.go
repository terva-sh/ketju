package auth

import (
	"errors"
	"net/http"
	"time"

	"github.com/terva-sh/ketju/internal/auth/oidc"
	"github.com/terva-sh/ketju/internal/auth/session"
	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/service/identity"
)

// RegistrationCookie carries a registration from the callback to the handle
// screen: the claims the IdP verified, sealed, so an abandoned registration
// writes nothing (docs/05 "Registration path", settled in grooming for
// TKT-01M29C8005).
const RegistrationCookie = "__Host-ketju_reg"

// registrationPurpose binds a sealed registration to this cookie and no
// other: a sign-in's flow cookie cannot be replayed as one.
const registrationPurpose = "registration"

// RegistrationTTL is how long someone has to choose a handle.
const RegistrationTTL = 15 * time.Minute

// RegisterPath is the handle screen, GET to show it and POST to submit it.
const RegisterPath = "/auth/register"

// pending is what the registration cookie holds.
type pending struct {
	Claims   identity.Claims `json:"c"`
	ReturnTo string          `json:"r"`
}

// beginRegistration is the callback's answer for an identity nobody has,
// from a browser nobody is signed in on. With registration open, the claims
// go into the registration cookie and the browser to the handle screen.
func (h *Handler) beginRegistration(w http.ResponseWriter, r *http.Request, id *oidc.Identity, returnTo string) {
	ctx := r.Context()
	open, err := h.Login.RegistrationOpen(ctx)
	if err != nil {
		h.Logger.ErrorContext(ctx, "reading the registration mode failed", "error", err)
		writePage(w, http.StatusInternalServerError, pageFailed)
		return
	}
	if !open {
		// Approval mode, whose applicant path is TKT-01M29CKRG9's.
		writePage(w, http.StatusNotImplemented, pageRegistration)
		return
	}
	sealed, err := h.Sealer.Seal(registrationPurpose, pending{Claims: identity.ClaimsOf(id), ReturnTo: returnTo})
	if err != nil {
		h.Logger.ErrorContext(ctx, "sealing a registration failed", "error", err)
		writePage(w, http.StatusInternalServerError, pageFailed)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: RegistrationCookie, Value: sealed, Path: "/",
		MaxAge: int(RegistrationTTL / time.Second), Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, RegisterPath, http.StatusSeeOther)
}

// pendingFrom opens the registration cookie. When it cannot, it has
// answered the request, and cleared the cookie.
func (h *Handler) pendingFrom(w http.ResponseWriter, r *http.Request) (pending, bool) {
	var p pending
	c, err := r.Cookie(RegistrationCookie)
	if err == nil {
		err = h.Sealer.Open(registrationPurpose, c.Value, RegistrationTTL, &p)
	}
	if err != nil {
		h.Logger.InfoContext(r.Context(), "no registration to continue", "error", err)
		clearRegistration(w)
		writePage(w, http.StatusBadRequest, pageRegistrationExpired)
		return p, false
	}
	return p, true
}

// registerForm shows the handle screen, prefilled.
func (h *Handler) registerForm(w http.ResponseWriter, r *http.Request) {
	p, ok := h.pendingFrom(w, r)
	if !ok {
		return
	}
	writeRegisterPage(w, http.StatusOK, registerData{
		Handle: identity.Prefill(p.Claims.PreferredUsername, p.Claims.Email)})
}

// register takes the handle and, if registration gives it out, signs the new
// member in and sends them where their sign-in was going.
func (h *Handler) register(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p, ok := h.pendingFrom(w, r)
	if !ok {
		return
	}
	// Someone signed in since the callback, in another tab. A signed-in
	// member signing in with a new identity is linking it, never making a
	// second account, as at the callback.
	if domain.ViewerFrom(ctx).Member() {
		clearRegistration(w)
		writePage(w, http.StatusConflict, pageLinking)
		return
	}
	var replaces string
	if old, err := r.Cookie(session.CookieName); err == nil {
		replaces = old.Value
	}
	typed := r.PostFormValue("handle")
	res, err := h.Login.Register(ctx, p.Claims, typed, session.Meta{
		IP: remoteIP(r), UserAgent: r.UserAgent(), Provider: p.Claims.Provider}, replaces)
	again := func(status int, msg string) {
		writeRegisterPage(w, status, registerData{Handle: typed, Error: msg})
	}
	switch {
	case errors.Is(err, domain.ErrInvalidHandle):
		again(http.StatusUnprocessableEntity, "That handle is not allowed. "+handleRule)
	case errors.Is(err, identity.ErrHandleReserved):
		again(http.StatusUnprocessableEntity, "That handle is reserved. Please choose another.")
	case errors.Is(err, identity.ErrHandleTaken):
		again(http.StatusConflict, "That handle is taken. Please choose another.")
	case errors.Is(err, identity.ErrAlreadyRegistered):
		clearRegistration(w)
		writePage(w, http.StatusConflict, pageAlreadyRegistered)
	case errors.Is(err, identity.ErrRegistrationClosed):
		clearRegistration(w)
		writePage(w, http.StatusForbidden, pageRegistration)
	case err != nil:
		h.Logger.ErrorContext(ctx, "registration failed", "provider", p.Claims.Provider, "error", err)
		writePage(w, http.StatusInternalServerError, pageFailed)
	default:
		h.Logger.InfoContext(ctx, "registered a new member", "provider", p.Claims.Provider)
		clearRegistration(w)
		http.SetCookie(w, session.Cookie(res.Token, res.Session.ExpiresAt))
		w.Header().Set("Cache-Control", "no-store")
		http.Redirect(w, r, SafeReturnPath(p.ReturnTo), http.StatusSeeOther)
	}
}

func clearRegistration(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: RegistrationCookie, Value: "", Path: "/", MaxAge: -1,
		Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})
}
