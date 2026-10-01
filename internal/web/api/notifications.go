package api

import (
	"net/http"

	"github.com/terva-sh/ketju/internal/domain"
)

// notificationsReadRequest is the body of POST /api/v1/notifications/read
// (docs/07): {ids} or {up_to}, exactly one. ids is a pointer so that an
// empty list, which marks nothing, is told from one not given; null is not
// given.
type notificationsReadRequest struct {
	IDs  *[]int64 `json:"ids"`
	UpTo *int64   `json:"up_to"`
}

// notifications is GET /api/v1/notifications?cursor= (TKT-01M29CRJ2T): a
// page of the viewer's notifications. The rules are the threads service's
// (Notifications). The viewer is asked whether they keep state before
// anything is read, as every viewer-state route asks, and the answer is the
// viewer's own, so no cache keeps it.
func (h *handlers) notifications(w http.ResponseWriter, r *http.Request) {
	v := domain.ViewerFrom(r.Context())
	if err := h.threads.MayKeepState(v); err != nil {
		h.fail(w, r, err)
		return
	}
	res, err := h.threads.Notifications(r.Context(), v, r.URL.Query().Get("cursor"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, res)
}

// markNotificationsRead is POST /api/v1/notifications/read: it marks the
// viewer's own unread notifications read, by ids or up to an id
// (MarkNotificationsRead), and answers how many it marked.
func (h *handlers) markNotificationsRead(w http.ResponseWriter, r *http.Request) {
	v := domain.ViewerFrom(r.Context())
	if err := h.threads.MayKeepState(v); err != nil {
		h.fail(w, r, err)
		return
	}
	var in notificationsReadRequest
	if err := decodeStrict(r.Body, &in); err != nil {
		h.fail(w, r, err)
		return
	}
	var ids []int64
	if in.IDs != nil {
		ids = append([]int64{}, *in.IDs...) // non-nil: an empty list marks nothing
	}
	res, err := h.threads.MarkNotificationsRead(r.Context(), v, ids, in.UpTo)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, res)
}
