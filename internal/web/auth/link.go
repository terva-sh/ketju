package auth

import (
	"errors"
	"net/http"
	"net/url"

	"github.com/terva-sh/ketju/internal/auth/agentcred"
	"github.com/terva-sh/ketju/internal/auth/oidc"
	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/service/identity"
)

// Account linking (docs/05 "Login flow" step 3, TKT-01M29CKRES). A member
// adds another sign-in to their account only through the explicit "link a
// sign-in" action: the page at LinkPath, whose forms post to
// /auth/{provider}/link. That start seals the member's user id into the
// flow cookie as Flow.LinkUser, and the callback links an identity nobody
// has only when the flow carries it and the browser is still signed in as
// that member. Without it, an identity nobody has from a signed-in browser
// is refused, so neither a member switching accounts nor a forged sign-in
// at a weak provider attaches an identity to an account unasked.
//
// A link issues no session and changes no cookie but the flow's. The
// identity is used later to sign in through the callback like any other,
// and so through session.IssueUser's status check.

// LinkPath is the "link a sign-in" page.
const LinkPath = "/auth/link"

// linker is who may link a sign-in: an active member signed in with a
// session. An agent authenticated by its credential never uses OIDC
// (docs/05), so it is refused here as well as by identity.Link's check of
// the account's kind.
func linker(r *http.Request) (domain.Viewer, bool) {
	v := domain.ViewerFrom(r.Context())
	if _, agent := agentcred.From(r.Context()); agent {
		return v, false
	}
	return v, v.Member()
}

// linkPage offers each enabled provider as a form posting to its link
// start. A viewer who may not link is sent to sign in, and brought back
// here afterwards.
func (h *Handler) linkPage(w http.ResponseWriter, r *http.Request) {
	returnTo := SafeReturnPath(r.URL.Query().Get("return_to"))
	w.Header().Set("Cache-Control", "no-store")
	if _, ok := linker(r); !ok {
		back := LinkPath
		if returnTo != "/" {
			back += "?" + url.Values{"return_to": {returnTo}}.Encode()
		}
		http.Redirect(w, r, "/auth/signin?"+url.Values{"return_to": {back}}.Encode(), http.StatusSeeOther)
		return
	}
	var choices []linkChoice
	if h.Providers != nil {
		for _, p := range h.Providers.Enabled() {
			choices = append(choices, linkChoice{Name: p.Name(), Action: "/auth/" + url.PathEscape(p.Key()) + "/link"})
		}
	}
	if len(choices) == 0 {
		writePage(w, http.StatusOK, pageLinkUnavailable)
		return
	}
	writeLinkPage(w, linkData{Choices: choices, ReturnTo: returnTo})
}

// startLink is POST /auth/{provider}/link: a sign-in marked as a link by
// the member making it. The cross-origin check in front of every write
// holds it to this site, so another page cannot start a link for a
// member. It answers with a page that sends the browser on to the IdP
// rather than a redirect (continueTmpl).
func (h *Handler) startLink(w http.ResponseWriter, r *http.Request) {
	v, ok := linker(r)
	if !ok {
		writePage(w, http.StatusForbidden, pageLinkNeedsMember)
		return
	}
	authURL, p, ok := h.begin(w, r, r.PostFormValue("return_to"), v.UserID)
	if !ok {
		return
	}
	writeContinuePage(w, continueData{Name: p.Name(), URL: authURL})
}

// link is the callback's answer when Login found a link: the identity is
// attached to the member who started it, who is the member signed in, and
// the browser goes back where the link began. The session is left as it
// is.
func (h *Handler) link(w http.ResponseWriter, r *http.Request, id *oidc.Identity, f Flow) {
	ctx := r.Context()
	why, err := h.Login.Link(ctx, f.LinkUser, h.Login.ClaimsOf(id), remoteIP(r))
	switch {
	case errors.Is(err, identity.ErrCannotLink):
		h.Logger.InfoContext(ctx, "a link was refused: the account cannot link", "provider", id.Provider, "error", err)
		writePage(w, http.StatusForbidden, pageLinkNotAllowed)
	case errors.Is(err, identity.ErrLinkTaken):
		h.Logger.InfoContext(ctx, "a link was refused: the identity was taken meanwhile", "provider", id.Provider)
		writePage(w, http.StatusConflict, pageLinkOtherUser)
	case err != nil:
		h.Logger.ErrorContext(ctx, "linking failed", "provider", id.Provider, "error", err)
		writePage(w, http.StatusInternalServerError, pageFailed)
	case why != identity.LinkRefusalNone:
		h.Logger.InfoContext(ctx, "a link was refused", "provider", id.Provider, "reason", why.String())
		writeLinkRefused(w, why)
	default:
		h.Logger.InfoContext(ctx, "linked a sign-in to an account", "provider", id.Provider, "user", f.LinkUser)
		w.Header().Set("Cache-Control", "no-store")
		http.Redirect(w, r, f.ReturnTo, http.StatusSeeOther)
	}
}
