package api

import (
	"net/http"
	"time"

	"github.com/terva-sh/ketju/internal/auth/session"
	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/web"
)

// The account lifecycle routes (TKT-01M29CKRFS, docs/07). Each answers 204
// with no-store; account.Service holds who may do what to whom, and asks
// again whatever gate the route has.

// registerLifecycle registers the admin routes of the lifecycle. Suspension
// is a moderator action (docs/08 "Primitives"), so its routes are staff's;
// anonymizing is an admin's.
func (h *handlers) registerLifecycle(mux *http.ServeMux) {
	h.adminRoute(mux, "POST /api/v1/admin/users/{handle}/suspend", staffGate, h.adminSuspend)
	h.adminRoute(mux, "POST /api/v1/admin/users/{handle}/unsuspend", staffGate, h.adminUnsuspend)
	h.adminRoute(mux, "POST /api/v1/admin/users/{handle}/anonymize", adminGate, h.adminAnonymize)
}

// noContent answers 204 with no-store, or the error.
func (h *handlers) noContent(w http.ResponseWriter, r *http.Request, err error) {
	if err != nil {
		h.fail(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}

// deactivate is POST /api/v1/settings/deactivate: the viewer deactivates
// their own account. Every session the account had is gone, this one
// included, so the cookie is cleared too.
func (h *handlers) deactivate(w http.ResponseWriter, r *http.Request) {
	if err := h.account.Deactivate(r.Context(), domain.ViewerFrom(r.Context()), web.ClientIP(r)); err != nil {
		h.fail(w, r, err)
		return
	}
	http.SetCookie(w, session.ClearCookie())
	h.noContent(w, r, nil)
}

// adminSuspend is POST /api/v1/admin/users/{handle}/suspend with {reason,
// until?}: until is Unix ms, and null or absent sets no end.
func (h *handlers) adminSuspend(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Reason string      `json:"reason"`
		Until  *dto.Millis `json:"until"`
	}
	if err := decodeStrict(r.Body, &in); err != nil {
		h.fail(w, r, err)
		return
	}
	var until *time.Time
	if in.Until != nil {
		t := time.UnixMilli(int64(*in.Until))
		until = &t
	}
	h.noContent(w, r, h.account.Suspend(r.Context(), domain.ViewerFrom(r.Context()), web.ClientIP(r),
		r.PathValue("handle"), in.Reason, until))
}

// adminUnsuspend is POST /api/v1/admin/users/{handle}/unsuspend.
func (h *handlers) adminUnsuspend(w http.ResponseWriter, r *http.Request) {
	h.noContent(w, r, h.account.Unsuspend(r.Context(), domain.ViewerFrom(r.Context()), web.ClientIP(r),
		r.PathValue("handle")))
}

// adminAnonymize is POST /api/v1/admin/users/{handle}/anonymize. It cannot
// be undone.
func (h *handlers) adminAnonymize(w http.ResponseWriter, r *http.Request) {
	h.noContent(w, r, h.account.Anonymize(r.Context(), domain.ViewerFrom(r.Context()), web.ClientIP(r),
		r.PathValue("handle")))
}
