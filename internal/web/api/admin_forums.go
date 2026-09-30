package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/service/forums"
	"github.com/terva-sh/ketju/internal/web"
)

// The admin forum routes (TKT-01M29CXK19, docs/07 "Forums"). Every one is
// an admin's: the maintainer decided on 2026-09-30 that admin screens keep
// their role gates, so holding moderate in a forum, by role or through a
// group, opens none of them (docs/08 "Decided 2026-09-30"). The service asks
// again.
//
// Every answer is no-store, and every write answers with the whole list as
// it committed. Writes are JSON, as the chain requires of every mutating
// request, and are charged to the write limiter with the rest of the API.
func (h *handlers) registerAdminForums(mux *http.ServeMux) {
	h.adminRoute(mux, "GET /api/v1/admin/forums", adminGate, h.adminForums)
	h.adminRoute(mux, "POST /api/v1/admin/forums", adminGate, h.adminCreateForum)
	h.adminRoute(mux, "PATCH /api/v1/admin/forums/{id}", adminGate, h.adminUpdateForum)
	h.adminRoute(mux, "POST /api/v1/admin/forums/{id}/move", adminGate, h.adminMoveForum)
	h.adminRoute(mux, "PUT /api/v1/admin/forums/{id}/visibility", adminGate, h.adminForumVisibility)
	h.adminRoute(mux, "PUT /api/v1/admin/forums/{id}/archived", adminGate, h.adminForumArchived)
}

// forumID parses {id}.
func forumID(r *http.Request) (int64, error) {
	s := r.PathValue("id")
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n < 1 {
		return 0, problem.Coded(problem.CodeBadRequest, fmt.Errorf("api: forum id %q is not a positive integer", s))
	}
	return n, nil
}

// adminForums is GET /api/v1/admin/forums.
func (h *handlers) adminForums(w http.ResponseWriter, r *http.Request) {
	res, err := h.forums.List(r.Context(), domain.ViewerFrom(r.Context()))
	answer(h, w, r, res, err)
}

// adminCreateForum is POST /api/v1/admin/forums with {slug, name,
// description, parent_id, visibility}, answered 201 with the list. There
// is no Location: no route reads one forum, and the list is the answer
// (Terva review 1571).
func (h *handlers) adminCreateForum(w http.ResponseWriter, r *http.Request) {
	var in forums.NewForum
	if err := decodeStrict(r.Body, &in); err != nil {
		h.fail(w, r, err)
		return
	}
	// As on a move: a forum id is positive, and 0 and below are malformed
	// rather than a forum that is missing (Terva review 1576).
	if in.ParentID != nil && *in.ParentID < 1 {
		h.fail(w, r, problem.Coded(problem.CodeBadRequest, errors.New("api: parent_id is a positive forum id or null")))
		return
	}
	res, _, err := h.forums.Create(r.Context(), domain.ViewerFrom(r.Context()), web.ClientIP(r), in)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusCreated, res)
}

// adminUpdateForum is PATCH /api/v1/admin/forums/{id} with {name?,
// description?}.
func (h *handlers) adminUpdateForum(w http.ResponseWriter, r *http.Request) {
	id, err := forumID(r)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	var in forums.ForumEdit
	if err := decodeStrict(r.Body, &in); err != nil {
		h.fail(w, r, err)
		return
	}
	res, err := h.forums.Update(r.Context(), domain.ViewerFrom(r.Context()), web.ClientIP(r), id, in)
	answer(h, w, r, res, err)
}

// adminMoveForum is POST /api/v1/admin/forums/{id}/move with {parent_id,
// before?, after?}. parent_id is required, and null is the top level: a
// body without it is bad-request rather than a move to the top level,
// because a pointer alone cannot tell an omitted field from null (Terva
// review 1571).
func (h *handlers) adminMoveForum(w http.ResponseWriter, r *http.Request) {
	id, err := forumID(r)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	var in struct {
		ParentID json.RawMessage `json:"parent_id"`
		Before   int64           `json:"before"`
		After    int64           `json:"after"`
	}
	if err := decodeStrict(r.Body, &in); err != nil {
		h.fail(w, r, err)
		return
	}
	if len(in.ParentID) == 0 {
		h.fail(w, r, problem.Coded(problem.CodeBadRequest,
			errors.New("api: the move has no parent_id; null places the forum at the top level")))
		return
	}
	m := forums.ForumMove{Before: in.Before, After: in.After}
	if err := json.Unmarshal(in.ParentID, &m.ParentID); err != nil {
		h.fail(w, r, problem.Coded(problem.CodeBadRequest, fmt.Errorf("api: parent_id is a forum id or null: %w", err)))
		return
	}
	// A forum id is positive, as {id} is (forumID); 0 and below name no
	// forum and are malformed, not a forum that is missing (review 1574).
	// before and after are 0 when not sent, so only below 0 is malformed.
	if (m.ParentID != nil && *m.ParentID < 1) || in.Before < 0 || in.After < 0 {
		h.fail(w, r, problem.Coded(problem.CodeBadRequest,
			errors.New("api: parent_id is a positive forum id or null, and before and after are positive forum ids")))
		return
	}
	res, err := h.forums.Move(r.Context(), domain.ViewerFrom(r.Context()), web.ClientIP(r), id, m)
	answer(h, w, r, res, err)
}

// adminForumVisibility is PUT /api/v1/admin/forums/{id}/visibility with
// {visibility}.
func (h *handlers) adminForumVisibility(w http.ResponseWriter, r *http.Request) {
	id, err := forumID(r)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	var in struct {
		Visibility *string `json:"visibility"`
	}
	if err := decodeStrict(r.Body, &in); err != nil {
		h.fail(w, r, err)
		return
	}
	if in.Visibility == nil {
		h.fail(w, r, problem.Coded(problem.CodeBadRequest, errors.New("api: the change has no visibility")))
		return
	}
	res, err := h.forums.SetVisibility(r.Context(), domain.ViewerFrom(r.Context()), web.ClientIP(r), id, *in.Visibility)
	answer(h, w, r, res, err)
}

// adminForumArchived is PUT /api/v1/admin/forums/{id}/archived with
// {archived}.
func (h *handlers) adminForumArchived(w http.ResponseWriter, r *http.Request) {
	id, err := forumID(r)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	var in struct {
		Archived *bool `json:"archived"`
	}
	if err := decodeStrict(r.Body, &in); err != nil {
		h.fail(w, r, err)
		return
	}
	if in.Archived == nil {
		h.fail(w, r, problem.Coded(problem.CodeBadRequest, errors.New("api: the change does not say archived or not")))
		return
	}
	res, err := h.forums.SetArchived(r.Context(), domain.ViewerFrom(r.Context()), web.ClientIP(r), id, *in.Archived)
	answer(h, w, r, res, err)
}
