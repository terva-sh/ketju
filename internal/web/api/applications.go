package api

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/service/applications"
	"github.com/terva-sh/ketju/internal/web"
)

// The application review routes (TKT-01M29CP2MG, docs/07 "Moderation and
// admin"): staff list applications, read one, note it, approve it, deny it
// with or without a block, and lift a block. Reviewing is a moderator
// action (docs/08 "Primitives"), so every route is behind the staff gate,
// and internal/service/applications asks again. Every answer is no-store
// and none goes near the response cache: it is staff's view of rows any
// moderator can change.
func (h *handlers) registerApplications(mux *http.ServeMux) {
	h.adminRoute(mux, "GET /api/v1/admin/applications", staffGate, h.listApplications)
	h.adminRoute(mux, "GET /api/v1/admin/applications/{id}", staffGate, h.getApplication)
	h.adminRoute(mux, "POST /api/v1/admin/applications/{id}/approve", staffGate, h.approveApplication)
	h.adminRoute(mux, "POST /api/v1/admin/applications/{id}/deny", staffGate, h.denyApplication)
	h.adminRoute(mux, "POST /api/v1/admin/applications/{id}/notes", staffGate, h.noteApplication)
	h.adminRoute(mux, "POST /api/v1/admin/applications/{id}/unblock", staffGate, h.unblockApplication)
}

// applicationID parses {id}.
func applicationID(r *http.Request) (int64, error) {
	s := r.PathValue("id")
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n < 1 {
		return 0, problem.Coded(problem.CodeBadRequest, fmt.Errorf("api: application id %q is not a positive integer", s))
	}
	return n, nil
}

// listApplications is GET /api/v1/admin/applications?status=&cursor=.
func (h *handlers) listApplications(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	out, err := h.applications.List(r.Context(), domain.ViewerFrom(r.Context()), q.Get("status"), q.Get("cursor"))
	h.writeNoStore(w, r, http.StatusOK, out, err)
}

// getApplication is GET /api/v1/admin/applications/{id}.
func (h *handlers) getApplication(w http.ResponseWriter, r *http.Request) {
	id, err := applicationID(r)
	if err != nil {
		h.writeNoStore(w, r, 0, nil, err)
		return
	}
	out, err := h.applications.Get(r.Context(), domain.ViewerFrom(r.Context()), id)
	h.writeNoStore(w, r, http.StatusOK, out, err)
}

// reviewRequest refuses a viewer who may not review, then parses {id} and
// decodes the body strictly into in: who may ask never depends on what was
// asked (review 1425).
func reviewRequest(r *http.Request, in any) (int64, error) {
	if err := applications.Staff(domain.ViewerFrom(r.Context())); err != nil {
		return 0, err
	}
	id, err := applicationID(r)
	if err != nil {
		return 0, err
	}
	if in != nil {
		if err := decodeStrict(r.Body, in); err != nil {
			return 0, err
		}
	}
	return id, nil
}

// approveApplication is POST /api/v1/admin/applications/{id}/approve with
// {handle, note?}. There is no role field, and an unknown field is
// bad-request: approval makes a member and nothing else.
func (h *handlers) approveApplication(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Handle *string `json:"handle"`
		Note   string  `json:"note"`
	}
	id, err := reviewRequest(r, &in)
	if err == nil && in.Handle == nil {
		err = problem.Coded(problem.CodeBadRequest, errors.New("api: handle is required"))
	}
	if err != nil {
		h.writeNoStore(w, r, 0, nil, err)
		return
	}
	out, err := h.applications.Approve(r.Context(), domain.ViewerFrom(r.Context()), id, *in.Handle, in.Note, web.ClientIP(r))
	h.writeNoStore(w, r, http.StatusOK, out, err)
}

// denyApplication is POST /api/v1/admin/applications/{id}/deny with {note,
// block}.
func (h *handlers) denyApplication(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Note  string `json:"note"`
		Block bool   `json:"block"`
	}
	id, err := reviewRequest(r, &in)
	if err != nil {
		h.writeNoStore(w, r, 0, nil, err)
		return
	}
	out, err := h.applications.Deny(r.Context(), domain.ViewerFrom(r.Context()), id, in.Note, in.Block, web.ClientIP(r))
	h.writeNoStore(w, r, http.StatusOK, out, err)
}

// noteApplication is POST /api/v1/admin/applications/{id}/notes with
// {body}. It answers 201 with the note.
func (h *handlers) noteApplication(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Body string `json:"body"`
	}
	id, err := reviewRequest(r, &in)
	if err != nil {
		h.writeNoStore(w, r, 0, nil, err)
		return
	}
	out, err := h.applications.AddNote(r.Context(), domain.ViewerFrom(r.Context()), id, in.Body)
	h.writeNoStore(w, r, http.StatusCreated, out, err)
}

// unblockApplication is POST /api/v1/admin/applications/{id}/unblock, with
// an empty body or {}. The body is read whole, as withdrawApplication's is.
func (h *handlers) unblockApplication(w http.ResponseWriter, r *http.Request) {
	id, err := reviewRequest(r, nil)
	if err == nil {
		var body []byte
		body, err = io.ReadAll(r.Body)
		switch {
		case err != nil:
			err = problem.Coded(problem.CodeBadRequest, fmt.Errorf("api: the request body: %w", err))
		case len(bytes.TrimSpace(body)) > 0:
			var in struct{}
			err = decodeStrict(bytes.NewReader(body), &in)
		}
	}
	if err != nil {
		h.writeNoStore(w, r, 0, nil, err)
		return
	}
	out, err := h.applications.Unblock(r.Context(), domain.ViewerFrom(r.Context()), id, web.ClientIP(r))
	h.writeNoStore(w, r, http.StatusOK, out, err)
}
