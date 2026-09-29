package auth

import (
	"errors"
	"fmt"
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

// pending is what the registration cookie holds, and the reactivation
// cookie, which adds the account's status_version as the sign-in found it
// (reactivate.go).
type pending struct {
	Claims   identity.Claims `json:"c"`
	ReturnTo string          `json:"r"`
	Stamp    int64           `json:"t,omitempty"`
	// Why is set when an auto_approve provider's preferred_username could
	// not be the handle, and the handle screen says why. It is sealed with
	// the rest, so a browser cannot choose it.
	Why identity.SkipReason `json:"w,omitempty"`
}

// beginRegistration is the callback's answer for an identity nobody has,
// from a browser nobody is signed in on (docs/05 "Registration path").
//
// When registration is not open for this provider, the mode is approval and
// the provider does not auto-approve, and someone new applies
// (beginApplication). When it is open and the provider auto-approves, the
// account is made here, under the handle the IdP's preferred_username
// makes, if it makes a free one. Otherwise, or for any other provider, the
// claims go into the registration cookie and the browser to the handle
// screen, which says why the preferred_username was not used when it was
// tried.
func (h *Handler) beginRegistration(w http.ResponseWriter, r *http.Request, id *oidc.Identity, returnTo string) {
	ctx := r.Context()
	open, err := h.Login.RegistrationOpenFor(ctx, id.Provider)
	if err != nil {
		h.Logger.ErrorContext(ctx, "reading the registration mode failed", "error", err)
		writePage(w, http.StatusInternalServerError, pageFailed)
		return
	}
	if !open {
		h.beginApplication(w, r, id)
		return
	}
	p := pending{Claims: h.Login.ClaimsOf(id), ReturnTo: returnTo}
	if h.Login.AutoApproves(id.Provider) {
		var done bool
		if p.Why, done = h.autoRegister(w, r, p); done {
			return
		}
	}
	sealed, err := h.sealRegistration(p)
	if errors.Is(err, errRegistrationTooBig) {
		h.Logger.WarnContext(ctx, "registration refused: the provider's claims do not fit in a cookie",
			"provider", id.Provider, "error", err)
		writePage(w, http.StatusBadRequest, pageFailed)
		return
	}
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

// autoRegister is the callback registering someone new from an auto_approve
// provider with no handle screen (docs/05 "mode = approval"). done is true
// when it has answered the request: signed in and sent on, or refused.
// Otherwise nothing was written, and why says what the handle screen,
// shown instead, tells the reader.
//
// The refusals are the handle screen's, for the same races: the identity
// registered in another tab, or registration closed since the callback
// read the mode.
func (h *Handler) autoRegister(w http.ResponseWriter, r *http.Request, p pending) (why identity.SkipReason, done bool) {
	ctx := r.Context()
	var replaces string
	if old, err := r.Cookie(session.CookieName); err == nil {
		replaces = old.Value
	}
	res, why, err := h.Login.AutoRegister(ctx, p.Claims, session.Meta{
		IP: remoteIP(r), UserAgent: r.UserAgent(), Provider: p.Claims.Provider}, replaces)
	switch {
	case errors.Is(err, identity.ErrAlreadyRegistered):
		h.Logger.InfoContext(ctx, "an auto-approved registration found the identity registered", "provider", p.Claims.Provider)
		writePage(w, http.StatusConflict, pageAlreadyRegistered)
	case errors.Is(err, identity.ErrRegistrationClosed):
		h.Logger.InfoContext(ctx, "an auto-approved registration found registration closed", "provider", p.Claims.Provider)
		writePage(w, http.StatusForbidden, pageRegistration)
	case err != nil:
		h.Logger.ErrorContext(ctx, "an auto-approved registration failed", "provider", p.Claims.Provider, "error", err)
		writePage(w, http.StatusInternalServerError, pageFailed)
	case why != identity.SkipNone:
		h.Logger.InfoContext(ctx, "an auto-approved registration needs a handle", "provider", p.Claims.Provider,
			"reason", skipWords[why])
		return why, false
	default:
		h.Logger.InfoContext(ctx, "registered a new member without a handle screen", "provider", p.Claims.Provider)
		http.SetCookie(w, session.Cookie(res.Token, res.Session.ExpiresAt))
		w.Header().Set("Cache-Control", "no-store")
		http.Redirect(w, r, SafeReturnPath(p.ReturnTo), http.StatusSeeOther)
	}
	return identity.SkipNone, true
}

// skipWords end the handle screen's line saying why the sign-in's username
// was not used.
var skipWords = map[identity.SkipReason]string{
	identity.SkipMissing:  "the sign-in did not include one",
	identity.SkipInvalid:  "it is not a valid handle",
	identity.SkipReserved: "it is reserved",
	identity.SkipTaken:    "it is taken",
}

// beginApplication is approval mode's answer for someone new (docs/05 "mode
// = approval" step 1): an identity with no user, a pre-user session, and
// 303 to the application form. The session reaches the application flow
// and sign-out, and nothing else (web.PreUserRoutes).
//
// The two refusals are races with something else the browser or an admin
// did, and signing in again settles each: another tab wrote the identity,
// and this sign-in now finds an applicant; or registration opened, and it
// now reaches the handle screen.
func (h *Handler) beginApplication(w http.ResponseWriter, r *http.Request, id *oidc.Identity) {
	ctx := r.Context()
	var replaces string
	if old, err := r.Cookie(session.CookieName); err == nil {
		replaces = old.Value
	}
	res, err := h.Login.BeginApplication(ctx, h.Login.ClaimsOf(id), session.Meta{
		IP: remoteIP(r), UserAgent: r.UserAgent(), Provider: id.Provider}, replaces)
	switch {
	case errors.Is(err, identity.ErrAlreadyRegistered), errors.Is(err, identity.ErrRegistrationClosed):
		h.Logger.InfoContext(ctx, "an application could not begin", "provider", id.Provider, "error", err)
		writePage(w, http.StatusConflict, pageFailed)
	case err != nil:
		h.Logger.ErrorContext(ctx, "beginning an application failed", "provider", id.Provider, "error", err)
		writePage(w, http.StatusInternalServerError, pageFailed)
	default:
		h.Logger.InfoContext(ctx, "someone new is applying", "provider", id.Provider)
		http.SetCookie(w, session.Cookie(res.Token, res.Session.ExpiresAt))
		w.Header().Set("Cache-Control", "no-store")
		http.Redirect(w, r, ApplyPath, http.StatusSeeOther)
	}
}

// ApplyPath is the application form, where someone new goes under approval
// mode.
const ApplyPath = "/apply"

// MaxRegistrationCookie bounds the sealed registration, in bytes. Browsers
// keep a cookie of up to 4096 bytes counting its name and attributes, and
// silently drop a larger one, which would leave the handle screen with no
// registration to continue (review 1146). This leaves room for both.
const MaxRegistrationCookie = 3800

var errRegistrationTooBig = errors.New("the sealed registration is larger than a cookie may be")

// sealRegistration seals p for the registration cookie, shedding what the
// registration can do without until it fits: first the name and
// preferred_username, which only supply the display name and the prefill
// (the display name falls back to the handle), then the return_to, which
// falls back to "/". What is left is what the account needs: the
// provider, issuer, subject and email. The IdP bounds those, and if they
// still do not fit the registration is refused rather than sent to a
// browser that would drop it.
func (h *Handler) sealRegistration(p pending) (string, error) {
	return h.sealPending(registrationPurpose, p)
}

// sealPending is sealRegistration for purpose: the reactivation cookie
// carries the same claims and return_to, and sheds them the same way.
func (h *Handler) sealPending(purpose string, p pending) (string, error) {
	shed := []func(*pending){
		func(p *pending) { p.Claims.Name, p.Claims.PreferredUsername = "", "" },
		func(p *pending) { p.ReturnTo = "/" },
	}
	for i := 0; ; i++ {
		sealed, err := h.Sealer.Seal(purpose, p)
		if err != nil || len(sealed) <= MaxRegistrationCookie {
			return sealed, err
		}
		if i == len(shed) {
			return "", fmt.Errorf("%w: %d bytes", errRegistrationTooBig, len(sealed))
		}
		shed[i](&p)
	}
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
	d := registerData{Handle: identity.Prefill(p.Claims.PreferredUsername, p.Claims.Email)}
	if words, ok := skipWords[p.Why]; ok {
		d.Handle = identity.FallbackPrefill(p.Claims.PreferredUsername, p.Claims.Email)
		d.Why = "Your sign-in's username could not be used as your handle: " + words + "."
	}
	writeRegisterPage(w, http.StatusOK, d)
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
