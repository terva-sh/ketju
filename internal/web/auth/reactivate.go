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

// ReactivateCookie carries a sign-in that found its account deactivated to
// the Reactivate button: the claims the IdP verified, sealed, as the
// registration cookie carries them to the handle screen (decided 2026-09-29,
// TKT-01M29CKRFS). Only a browser that has just signed in as the account
// holds one, so only it can press the button.
const ReactivateCookie = "__Host-ketju_reactivate"

// reactivatePurpose binds a sealed reactivation to this cookie and no other:
// a registration or a sign-in's flow cannot be replayed as one.
const reactivatePurpose = "reactivate"

// ReactivateTTL is how long the Reactivate button stays good after the
// sign-in that offered it.
const ReactivateTTL = 5 * time.Minute

// ReactivatePath is where the Reactivate button posts.
const ReactivatePath = "/auth/reactivate"

// untilLayout is how the page writes a suspension's end: in UTC, which the
// page says, since it knows nothing of the reader's zone.
const untilLayout = "2 January 2006, 15:04 UTC"

// unavailable is the callback's answer for an account that is not active
// (docs/05 "Login flow"). No session is issued. A suspended account is shown
// the reason and the end, or that no end is set. A deactivated account is
// offered the Reactivate button, with the sealed cookie behind it. A pending
// account gets the plain page.
func (h *Handler) unavailable(w http.ResponseWriter, r *http.Request, res identity.Result, id *oidc.Identity, returnTo string) {
	switch res.Status {
	case domain.StatusSuspended:
		d := unavailableData{Suspended: true, Reason: res.Reason}
		if res.Until != 0 {
			d.Until = time.UnixMilli(res.Until).UTC().Format(untilLayout)
		}
		writeUnavailable(w, d)
	case domain.StatusDeactivated:
		var d unavailableData
		sealed, err := h.sealPending(reactivatePurpose, pending{Claims: h.Login.ClaimsOf(id), ReturnTo: returnTo,
			Stamp: res.Stamp})
		if err != nil {
			// The page still says what happened; signing in again offers
			// the button again.
			h.Logger.ErrorContext(r.Context(), "sealing a reactivation failed", "provider", id.Provider, "error", err)
		} else {
			http.SetCookie(w, &http.Cookie{Name: ReactivateCookie, Value: sealed, Path: "/",
				MaxAge: int(ReactivateTTL / time.Second), Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})
			d.Reactivate = true
		}
		writeUnavailable(w, d)
	default:
		writePage(w, http.StatusForbidden, pageUnavailable)
	}
}

// reactivate is the Reactivate button: it brings the deactivated account the
// cookie names back to active and signs it in, then sends the browser where
// its sign-in was going. The cookie is cleared whatever happens, and it is
// bound to the account's status_version as the sign-in found it, so a copy
// of it is no good once the status has changed: it serves one press.
//
// Only a deactivated account is reactivated; one that is suspended, pending
// or anonymized, or whose status has changed since the page was shown, is
// refused and nothing is written (identity.Service.Reactivate). A
// reactivation whose sign-in could not finish says the account is active
// and offers the way to sign in.
func (h *Handler) reactivate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var p pending
	c, err := r.Cookie(ReactivateCookie)
	if err == nil {
		err = h.Sealer.Open(reactivatePurpose, c.Value, ReactivateTTL, &p)
	}
	http.SetCookie(w, &http.Cookie{Name: ReactivateCookie, Value: "", Path: "/", MaxAge: -1,
		Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	if err != nil {
		h.Logger.InfoContext(ctx, "no reactivation to continue", "error", err)
		writePage(w, http.StatusBadRequest, pageReactivateExpired)
		return
	}
	var replaces string
	if old, err := r.Cookie(session.CookieName); err == nil {
		replaces = old.Value
	}
	res, err := h.Login.Reactivate(ctx, p.Claims, p.Stamp, session.Meta{
		IP: remoteIP(r), UserAgent: r.UserAgent(), Provider: p.Claims.Provider}, replaces)
	switch {
	case errors.Is(err, identity.ErrCannotReactivate):
		h.Logger.InfoContext(ctx, "reactivation refused", "provider", p.Claims.Provider, "error", err)
		writePage(w, http.StatusForbidden, pageUnavailable)
	case errors.Is(err, identity.ErrReactivatedNotSignedIn):
		// The account is active and the button is spent, so the way on is
		// an ordinary sign-in, which finishes what this one could not.
		h.Logger.ErrorContext(ctx, "reactivated, but the sign-in did not finish", "provider", p.Claims.Provider,
			"error", err)
		writePage(w, http.StatusInternalServerError, pageReactivatedSignInAgain)
	case err != nil:
		h.Logger.ErrorContext(ctx, "reactivation failed", "provider", p.Claims.Provider, "error", err)
		writePage(w, http.StatusInternalServerError, pageFailed)
	default:
		h.Logger.InfoContext(ctx, "a member reactivated their account", "provider", p.Claims.Provider)
		http.SetCookie(w, session.Cookie(res.Token, res.Session.ExpiresAt))
		w.Header().Set("Cache-Control", "no-store")
		http.Redirect(w, r, SafeReturnPath(p.ReturnTo), http.StatusSeeOther)
	}
}
