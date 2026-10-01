package api

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/terva-sh/ketju/internal/auth/agentcred"
	"github.com/terva-sh/ketju/internal/auth/session"
	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/service/member"
	"github.com/terva-sh/ketju/internal/service/users"
	"github.com/terva-sh/ketju/internal/web"
)

// A member's own settings, sessions and sign-ins (TKT-01M29CCC0X, docs/07
// "Resources (v1)", docs/05 "Sessions and CSRF"). Every answer is no-store:
// each is the viewer's own.
//
// Anonymous is unauthenticated and a pre-user forbidden on every route,
// before a body is read. GET and PATCH /settings are open to an agent by
// its credential, because page_size changes the thread pages it reads. The
// session and identity routes are not: an agent never uses OIDC and has no
// session (docs/05 "Agent accounts"), so a request its credential
// authenticated is forbidden there, as POST /auth/{provider}/link refuses
// one.

// settings is GET /api/v1/settings.
func (h *handlers) settings(w http.ResponseWriter, r *http.Request) {
	res, err := h.users.Settings(r.Context(), domain.ViewerFrom(r.Context()))
	answer(h, w, r, res, err)
}

// patchSettings is PATCH /api/v1/settings with any of {timezone, page_size,
// theme}. A key it does not name is bad-request; a value it refuses is
// validation-failed with a field error for each.
func (h *handlers) patchSettings(w http.ResponseWriter, r *http.Request) {
	v := domain.ViewerFrom(r.Context())
	if err := member.Require(v, "change settings"); err != nil {
		h.fail(w, r, err)
		return
	}
	var in users.SettingsPatch
	if err := decodeStrict(r.Body, &in); err != nil {
		h.fail(w, r, err)
		return
	}
	res, err := h.users.UpdateSettings(r.Context(), v, in)
	answer(h, w, r, res, err)
}

// ownSession is the request's member session, for the session and identity
// routes: a member signed in with a cookie, and nobody else. An agent by its
// credential is forbidden.
func ownSession(r *http.Request, what string) (domain.Viewer, session.Session, error) {
	v := domain.ViewerFrom(r.Context())
	if err := member.Require(v, what); err != nil {
		return v, session.Session{}, err
	}
	if _, agent := agentcred.From(r.Context()); agent {
		return v, session.Session{}, problem.Coded(problem.CodeForbidden,
			fmt.Errorf("api: an agent has no sessions or sign-ins, so it cannot %s", what))
	}
	sess, ok := session.From(r.Context())
	if !ok || sess.User == nil || sess.User.ID != v.UserID {
		return v, session.Session{}, problem.Coded(problem.CodeUnauthenticated,
			fmt.Errorf("api: %s needs a member session", what))
	}
	return v, sess, nil
}

// sessionList is GET /api/v1/settings/sessions: the viewer's live sessions,
// most recently used first, the request's own marked current.
func (h *handlers) sessionList(w http.ResponseWriter, r *http.Request) {
	v, sess, err := ownSession(r, "list sessions")
	if err != nil {
		h.fail(w, r, err)
		return
	}
	infos, err := h.sessions.ListForUser(r.Context(), v.UserID)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	out := dto.SessionList{Sessions: []dto.SessionInfo{}}
	current := sess.ID()
	for _, i := range infos {
		if !i.Live {
			continue
		}
		out.Sessions = append(out.Sessions, dto.SessionInfo{ID: i.ID, Current: i.ID == current,
			Device: session.DeviceLabel(i.UserAgent), UserAgent: i.UserAgent, IP: i.IP, Provider: i.Provider,
			CreatedAt: dto.Millis(i.CreatedAt.UnixMilli()), LastUsedAt: dto.Millis(i.LastUsedAt.UnixMilli())})
	}
	answer(h, w, r, out, nil)
}

// endSession is DELETE /api/v1/settings/sessions/{id}: sign out the device
// the list names id. An id that is not one of the viewer's live sessions is
// not-found, whoever's it is. Ending the request's own session clears its
// cookie.
func (h *handlers) endSession(w http.ResponseWriter, r *http.Request) {
	v, sess, err := ownSession(r, "end a session")
	if err != nil {
		h.fail(w, r, err)
		return
	}
	id := r.PathValue("id")
	ended, err := h.sessions.RevokeOwn(r.Context(), v.UserID, id)
	switch {
	case err != nil:
		h.fail(w, r, err)
		return
	case !ended:
		h.fail(w, r, problem.Coded(problem.CodeNotFound, errors.New("api: the viewer has no such session")))
		return
	}
	if id == sess.ID() {
		http.SetCookie(w, session.ClearCookie())
	}
	h.noContent(w, r, nil)
}

// endEverySession is DELETE /api/v1/settings/sessions: sign out everywhere,
// this browser included (docs/05 "Sessions and CSRF", decided by the run
// 2026-10-01), so the cookie is cleared.
func (h *handlers) endEverySession(w http.ResponseWriter, r *http.Request) {
	v, _, err := ownSession(r, "sign out everywhere")
	if err != nil {
		h.fail(w, r, err)
		return
	}
	if err := h.sessions.RevokeUser(r.Context(), v.UserID); err != nil {
		h.fail(w, r, err)
		return
	}
	http.SetCookie(w, session.ClearCookie())
	h.noContent(w, r, nil)
}

// identityList is GET /api/v1/settings/identities: the sign-ins linked to
// the viewer's account, the one this session came through marked current.
func (h *handlers) identityList(w http.ResponseWriter, r *http.Request) {
	v, sess, err := ownSession(r, "list sign-ins")
	if err != nil {
		h.fail(w, r, err)
		return
	}
	res, err := h.identity.ListIdentities(r.Context(), v, sess.IdentityID)
	answer(h, w, r, res, err)
}

// removeIdentity is DELETE /api/v1/settings/identities/{id}: remove one of
// the viewer's sign-ins (identity.Service.Unlink). When the request's own
// session came through it, that session has ended and the cookie is
// cleared.
func (h *handlers) removeIdentity(w http.ResponseWriter, r *http.Request) {
	v, _, err := ownSession(r, "remove a sign-in")
	if err != nil {
		h.fail(w, r, err)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		h.fail(w, r, problem.Coded(problem.CodeNotFound, fmt.Errorf("api: %q names no identity", r.PathValue("id"))))
		return
	}
	var token string
	if c, err := r.Cookie(session.CookieName); err == nil {
		token = c.Value
	}
	signedOut, err := h.identity.Unlink(r.Context(), v, token, id, web.ClientIP(r))
	if err == nil && signedOut {
		http.SetCookie(w, session.ClearCookie())
	}
	answer(h, w, r, dto.IdentityRemoved{SignedOut: signedOut}, err)
}
