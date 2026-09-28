package api

import (
	"errors"
	"net/http"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/service/threads"
	"github.com/terva-sh/ketju/internal/store/threadid"
)

// readRequest is the body of PUT /api/v1/threads/{id}/read, and the optional
// body of its DELETE (docs/07).
type readRequest struct {
	Ordinal *uint32 `json:"ordinal"`
}

// draftRequest is the body of PUT /api/v1/drafts/{thread_id} (docs/07).
type draftRequest struct {
	Source  *string `json:"source"`
	ReplyTo *uint32 `json:"reply_to"`
}

// maxReadBody bounds DELETE /read's optional {ordinal} body, as
// maxReasonBody bounds a reason: a number needs a few bytes.
const maxReadBody = 1 << 10

// markRead and markUnread are PUT and DELETE /api/v1/threads/{id}/read, the
// read position (TKT-01M29CA9CE). The rules are the threads service's
// (MarkRead, MarkUnread); these ask whether the viewer has a position to
// keep first, as the write handlers ask MayWrite, then parse and answer 200
// with the position as it now stands. Like every answer about the viewer
// alone, it is not cached.
func (h *handlers) markRead(w http.ResponseWriter, r *http.Request) {
	v := domain.ViewerFrom(r.Context())
	if err := h.threads.MayKeepState(v); err != nil {
		h.fail(w, r, err)
		return
	}
	id, err := threadid.Parse(r.PathValue("id"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	var in readRequest
	if err := decodeStrict(r.Body, &in); err != nil {
		h.fail(w, r, err)
		return
	}
	if in.Ordinal == nil {
		h.fail(w, r, problem.Coded(problem.CodeBadRequest, errors.New("api: the read position has no ordinal")))
		return
	}
	res, err := h.threads.MarkRead(r.Context(), v, id, *in.Ordinal)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, res)
}

func (h *handlers) markUnread(w http.ResponseWriter, r *http.Request) {
	v := domain.ViewerFrom(r.Context())
	if err := h.threads.MayKeepState(v); err != nil {
		h.fail(w, r, err)
		return
	}
	id, err := threadid.Parse(r.PathValue("id"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	var in readRequest
	if err := optionalJSON(r.Body, maxReadBody, &in); err != nil {
		h.fail(w, r, err)
		return
	}
	res, err := h.threads.MarkUnread(r.Context(), v, id, in.Ordinal)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, res)
}

// getDraft, putDraft and deleteDraft are /api/v1/drafts/{thread_id}, the
// composer's autosave (TKT-01M29CA9CE). A viewer with no draft for the
// thread gets 204 rather than not-found, which the API keeps for a thread
// that is not there: having no draft is the usual state, not an error.
// DELETE answers 204 whether or not there was one.
func (h *handlers) getDraft(w http.ResponseWriter, r *http.Request) {
	v := domain.ViewerFrom(r.Context())
	if err := h.threads.MayKeepState(v); err != nil {
		h.fail(w, r, err)
		return
	}
	id, err := threadid.Parse(r.PathValue("thread_id"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	d, ok, err := h.threads.Draft(r.Context(), v, id)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if !ok {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (h *handlers) putDraft(w http.ResponseWriter, r *http.Request) {
	v := domain.ViewerFrom(r.Context())
	if err := h.threads.MayKeepState(v); err != nil {
		h.fail(w, r, err)
		return
	}
	id, err := threadid.Parse(r.PathValue("thread_id"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	var in draftRequest
	if err := decodeStrict(r.Body, &in); err != nil {
		h.fail(w, r, err)
		return
	}
	if in.Source == nil {
		h.fail(w, r, problem.Coded(problem.CodeBadRequest, errors.New("api: the draft has no source")))
		return
	}
	d, err := h.threads.SaveDraft(r.Context(), v, id, threads.DraftRequest{Source: *in.Source, ReplyTo: in.ReplyTo})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, d)
}

func (h *handlers) deleteDraft(w http.ResponseWriter, r *http.Request) {
	v := domain.ViewerFrom(r.Context())
	if err := h.threads.MayKeepState(v); err != nil {
		h.fail(w, r, err)
		return
	}
	id, err := threadid.Parse(r.PathValue("thread_id"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	if err := h.threads.DeleteDraft(r.Context(), v, id); err != nil {
		h.fail(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}
