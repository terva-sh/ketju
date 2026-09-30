package api

import (
	"errors"
	"net/http"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/service/threads"
	"github.com/terva-sh/ketju/internal/web"
)

// The moderator queue's routes (docs/07 "Moderation and admin",
// TKT-01M29CRJ4E). Both are behind the moderates gate, which admits a member
// granted moderate on one forum through a group, and the threads service
// then scopes what they see and do by per-forum moderate. Application items
// are decided through the application routes (applications.go), which stay
// staff's.
func (h *handlers) registerQueue(mux *http.ServeMux) {
	h.adminRoute(mux, "GET /api/v1/admin/queue", moderatesGate, h.queue)
	h.adminRoute(mux, "POST /api/v1/admin/queue/posts/{id}/{ordinal}", moderatesGate, h.resolveQueuePost)
}

// queue is GET /api/v1/admin/queue?cursor=: one page of the viewer's queue.
func (h *handlers) queue(w http.ResponseWriter, r *http.Request) {
	res, err := h.threads.Queue(r.Context(), domain.ViewerFrom(r.Context()), r.URL.Query().Get("cursor"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, res)
}

// queueRequest is the body of POST /api/v1/admin/queue/posts/{id}/{ordinal}.
type queueRequest struct {
	Action *string `json:"action"`
	Note   string  `json:"note"`
}

// resolveQueuePost is POST /api/v1/admin/queue/posts/{id}/{ordinal} with
// {action, note?}: resolve ("Keep post"), dismiss or remove. The answer is
// dto.QueueResolution; another moderator's action first is conflict.
func (h *handlers) resolveQueuePost(w http.ResponseWriter, r *http.Request) {
	id, ordinal, err := postPath(r)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	var in queueRequest
	if err := decodeStrict(r.Body, &in); err != nil {
		h.fail(w, r, err)
		return
	}
	if in.Action == nil {
		h.fail(w, r, problem.Coded(problem.CodeBadRequest, errors.New("api: the request names no action")))
		return
	}
	res, err := h.threads.ResolvePost(r.Context(), domain.ViewerFrom(r.Context()), id, ordinal,
		threads.Resolution{Action: *in.Action, Note: in.Note, IP: web.ClientIP(r)})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, res)
}
