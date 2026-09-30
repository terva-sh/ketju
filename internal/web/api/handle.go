package api

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/service/member"
	"github.com/terva-sh/ketju/internal/service/users"
	"github.com/terva-sh/ketju/internal/web"
)

// Handle changes (docs/05 "Changing a handle", docs/07, TKT-01M3NRP3W9).
// identity.Service holds the rules; these routes read the body, ask, and
// answer dto.HandleChange with no-store.

// handleBody is what both rename routes take.
type handleBody struct {
	Handle string `json:"handle"`
}

// changeHandle is PUT /api/v1/settings/handle with {handle}: the viewer
// changes their own handle, at most once every 30 days. The viewer is asked
// for before the body is read, so a stranger is refused as one whatever
// they sent.
func (h *handlers) changeHandle(w http.ResponseWriter, r *http.Request) {
	v := domain.ViewerFrom(r.Context())
	if err := member.Require(v, "change a handle"); err != nil {
		h.fail(w, r, err)
		return
	}
	var in handleBody
	if err := decodeStrict(r.Body, &in); err != nil {
		h.fail(w, r, err)
		return
	}
	res, err := h.identity.ChangeHandle(r.Context(), v, web.ClientIP(r), in.Handle)
	h.handleChanged(w, r, res, err)
}

// adminRename is POST /api/v1/admin/users/{handle}/rename with {handle}: an
// admin renames the account, without the cooldown.
func (h *handlers) adminRename(w http.ResponseWriter, r *http.Request) {
	var in handleBody
	if err := decodeStrict(r.Body, &in); err != nil {
		h.fail(w, r, err)
		return
	}
	res, err := h.identity.RenameMember(r.Context(), domain.ViewerFrom(r.Context()), web.ClientIP(r),
		r.PathValue("handle"), in.Handle)
	h.handleChanged(w, r, res, err)
}

func (h *handlers) handleChanged(w http.ResponseWriter, r *http.Request, res any, err error) {
	if err != nil {
		h.fail(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, res)
}

// moved answers a profile route's *member.Moved with the redirect docs/05
// asks for, and reports whether it did: 302 to the same path under the
// account's handle now, the query kept (a cursor names no account), with
// no-store, because the old handle is someone else's to take once its
// reservation ends. prefix is the path before the handle, and rest what
// follows it.
func moved(w http.ResponseWriter, r *http.Request, err error, prefix, rest string) bool {
	var m *member.Moved
	if !errors.As(err, &m) {
		return false
	}
	loc := prefix + url.PathEscape(m.Handle) + rest
	if r.URL.RawQuery != "" {
		loc += "?" + r.URL.RawQuery
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Location", loc)
	w.WriteHeader(http.StatusFound)
	return true
}

// ProfileRedirects wraps the client shell so a member who opens /u/{old} or
// /u/{old}/posts for a handle a rename released within 90 days is sent to
// /u/{new} with a temporary, uncached redirect, as GET /api/v1/users/{old}
// is (docs/05 "Changing a handle"). Anyone else, and every other path, gets
// next: the shell, which routes the profile as it would any other, so a
// stranger learns nothing about who renamed to what. The reservation is read
// at the service's clock.
//
// A nil service panics here, at wiring, rather than on the first page.
func ProfileRedirects(u *users.Service, next http.Handler) http.Handler {
	if u == nil || next == nil {
		panic("api.ProfileRedirects: the users service and the shell are required")
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handle, rest, ok := profilePath(r.URL.Path)
		v := domain.ViewerFrom(r.Context())
		if !ok || (r.Method != http.MethodGet && r.Method != http.MethodHead) || !v.Member() {
			next.ServeHTTP(w, r)
			return
		}
		now := time.Now()
		if u.Now != nil {
			now = u.Now()
		}
		if _, err := member.Account(r.Context(), u.DB, v, handle, now); !moved(w, r, err, "/u/", rest) {
			next.ServeHTTP(w, r)
		}
	})
}

// profilePath splits /u/{handle} and /u/{handle}/posts into the handle and
// what follows it.
func profilePath(p string) (handle, rest string, ok bool) {
	tail, found := strings.CutPrefix(p, "/u/")
	if !found {
		return "", "", false
	}
	handle, rest, _ = strings.Cut(tail, "/")
	if rest != "" {
		rest = "/" + rest
	}
	if handle == "" || (rest != "" && rest != "/posts") {
		return "", "", false
	}
	return handle, rest, true
}
