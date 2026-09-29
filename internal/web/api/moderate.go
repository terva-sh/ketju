package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/store/threadid"
)

// The thread moderation routes (docs/07 "Moderation and admin",
// TKT-01M29CKRGS). Each is staff's by role, as docs/08's "Primitives" names
// the moderator for locking and pinning, and the threads service then checks
// moderate in the thread's own forum. Each appends one record, answers 200
// with the thread's header as its page now serves it, and is no-store.
func (h *handlers) registerModeration(mux *http.ServeMux) {
	h.adminRoute(mux, "POST /api/v1/admin/threads/{id}/lock", staffGate, h.lockThread)
	h.adminRoute(mux, "POST /api/v1/admin/threads/{id}/unlock", staffGate, h.unlockThread)
	h.adminRoute(mux, "POST /api/v1/admin/threads/{id}/pin", staffGate, h.pinThread)
	h.adminRoute(mux, "POST /api/v1/admin/threads/{id}/unpin", staffGate, h.unpinThread)
}

// pinRequest is the body of POST …/pin. Until is Unix ms, null for no end.
type pinRequest struct {
	Scope *string `json:"scope"`
	Until *int64  `json:"until"`
}

// unpinRequest is the body of POST …/unpin.
type unpinRequest struct {
	Scope *string `json:"scope"`
}

func (h *handlers) lockThread(w http.ResponseWriter, r *http.Request) {
	h.setLocked(w, r, h.threads.Lock)
}

func (h *handlers) unlockThread(w http.ResponseWriter, r *http.Request) {
	h.setLocked(w, r, h.threads.Unlock)
}

// setLocked is lock and unlock: an optional {reason} body, as a delete
// takes.
func (h *handlers) setLocked(w http.ResponseWriter, r *http.Request,
	do func(context.Context, domain.Viewer, threadid.ID, string) (dto.ThreadHeader, error)) {
	id, err := threadid.Parse(r.PathValue("id"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	in, err := optionalReason(r.Body)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	res, err := do(r.Context(), domain.ViewerFrom(r.Context()), id, in.Reason)
	h.answerHeader(w, r, res, err)
}

func (h *handlers) pinThread(w http.ResponseWriter, r *http.Request) {
	id, err := threadid.Parse(r.PathValue("id"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	var in pinRequest
	if err := optionalJSON(r.Body, maxReasonBody, &in); err != nil {
		h.fail(w, r, err)
		return
	}
	if in.Scope == nil {
		h.fail(w, r, problem.Coded(problem.CodeBadRequest, errors.New("api: the pin has no scope")))
		return
	}
	var until *time.Time
	if in.Until != nil {
		t := time.UnixMilli(*in.Until)
		until = &t
	}
	res, err := h.threads.Pin(r.Context(), domain.ViewerFrom(r.Context()), id, *in.Scope, until)
	h.answerHeader(w, r, res, err)
}

func (h *handlers) unpinThread(w http.ResponseWriter, r *http.Request) {
	id, err := threadid.Parse(r.PathValue("id"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	var in unpinRequest
	if err := optionalJSON(r.Body, maxReasonBody, &in); err != nil {
		h.fail(w, r, err)
		return
	}
	if in.Scope == nil {
		h.fail(w, r, problem.Coded(problem.CodeBadRequest, errors.New("api: the unpin has no scope")))
		return
	}
	res, err := h.threads.Unpin(r.Context(), domain.ViewerFrom(r.Context()), id, *in.Scope)
	h.answerHeader(w, r, res, err)
}

func (h *handlers) answerHeader(w http.ResponseWriter, r *http.Request, res dto.ThreadHeader, err error) {
	if err != nil {
		h.fail(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, res)
}
