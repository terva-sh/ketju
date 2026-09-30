package api

import (
	"errors"
	"net/http"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/web"
)

// registerSlowMode is the thread's slow mode route (docs/07 "Moderation and
// admin", TKT-01M29CRJ50). Like lock and pin it is staff's by role, and the
// threads service then checks moderate in the thread's own forum. It writes
// no record: the value is kept in the database (docs/08 "Primitives").
func (h *handlers) registerSlowMode(mux *http.ServeMux) {
	h.adminRoute(mux, "PUT /api/v1/admin/threads/{id}/slow-mode", staffGate, h.setSlowMode)
}

// slowModeRequest is the body of PUT …/slow-mode: seconds, 0 for off.
type slowModeRequest struct {
	Seconds *int `json:"seconds"`
}

// setSlowMode is PUT /api/v1/admin/threads/{id}/slow-mode with {seconds},
// answered 200 dto.SlowMode and no-store.
func (h *handlers) setSlowMode(w http.ResponseWriter, r *http.Request) {
	id, err := threadid.Parse(r.PathValue("id"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	var in slowModeRequest
	if err := decodeStrict(r.Body, &in); err != nil {
		h.fail(w, r, err)
		return
	}
	if in.Seconds == nil {
		h.fail(w, r, problem.Coded(problem.CodeBadRequest, errors.New("api: the request names no seconds")))
		return
	}
	res, err := h.threads.SetSlowMode(r.Context(), domain.ViewerFrom(r.Context()), web.ClientIP(r), id, *in.Seconds)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, res)
}
