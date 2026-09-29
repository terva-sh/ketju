package api

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/service/groups"
	"github.com/terva-sh/ketju/internal/service/users"
	"github.com/terva-sh/ketju/internal/web"
)

// The admin users, groups and permissions routes (TKT-01M29CXK1X, docs/07
// "Moderation and admin"). The list and one account's detail are staff's,
// because suspension is a moderator action reached from this screen
// (docs/08 "Primitives"); the rest is an admin's. The services ask again.
//
// Every answer is the viewer's own and about accounts that change, so none
// is cached. Writes are JSON, as the chain requires of every mutating
// request, and are charged to the write limiter with the rest of the API.
func (h *handlers) registerAdminUsers(mux *http.ServeMux) {
	h.adminRoute(mux, "GET /api/v1/admin/users", staffGate, h.adminUsers)
	h.adminRoute(mux, "GET /api/v1/admin/users/{handle}", staffGate, h.adminUser)
	h.adminRoute(mux, "PUT /api/v1/admin/users/{handle}/role", adminGate, h.adminSetRole)
	h.adminRoute(mux, "GET /api/v1/admin/groups", adminGate, h.adminGroups)
	h.adminRoute(mux, "POST /api/v1/admin/groups", adminGate, h.adminCreateGroup)
	h.adminRoute(mux, "GET /api/v1/admin/groups/{id}", adminGate, h.adminGroup)
	h.adminRoute(mux, "PATCH /api/v1/admin/groups/{id}", adminGate, h.adminUpdateGroup)
	h.adminRoute(mux, "DELETE /api/v1/admin/groups/{id}", adminGate, h.adminDeleteGroup)
	h.adminRoute(mux, "POST /api/v1/admin/groups/{id}/members", adminGate, h.adminAddMember)
	h.adminRoute(mux, "DELETE /api/v1/admin/groups/{id}/members/{handle}", adminGate, h.adminRemoveMember)
	h.adminRoute(mux, "GET /api/v1/admin/permissions", adminGate, h.adminGrid)
	h.adminRoute(mux, "POST /api/v1/admin/permissions", adminGate, h.adminApply)
	h.adminRoute(mux, "POST /api/v1/admin/permissions/preview", adminGate, h.adminPreview)
}

// answer writes v, or the error, with no-store.
func answer[T any](h *handlers, w http.ResponseWriter, r *http.Request, v T, err error) {
	if err != nil {
		h.fail(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, v)
}

// groupID parses {id}.
func groupID(r *http.Request) (int64, error) {
	s := r.PathValue("id")
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n < 1 {
		return 0, problem.Coded(problem.CodeBadRequest, fmt.Errorf("api: group id %q is not a positive integer", s))
	}
	return n, nil
}

// adminUsers is GET /api/v1/admin/users?prefix=&role=&status=&kind=&cursor=.
func (h *handlers) adminUsers(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	res, err := h.users.AdminList(r.Context(), domain.ViewerFrom(r.Context()), users.AdminFilter{
		Prefix: q.Get("prefix"), Role: q.Get("role"), Status: q.Get("status"), Kind: q.Get("kind"),
	}, q.Get("cursor"))
	answer(h, w, r, res, err)
}

// adminUser is GET /api/v1/admin/users/{handle}.
func (h *handlers) adminUser(w http.ResponseWriter, r *http.Request) {
	res, err := h.users.AdminDetail(r.Context(), domain.ViewerFrom(r.Context()), r.PathValue("handle"))
	answer(h, w, r, res, err)
}

// adminSetRole is PUT /api/v1/admin/users/{handle}/role with {role}.
func (h *handlers) adminSetRole(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Role *string `json:"role"`
	}
	if err := decodeStrict(r.Body, &in); err != nil {
		h.fail(w, r, err)
		return
	}
	if in.Role == nil {
		h.fail(w, r, problem.Coded(problem.CodeBadRequest, errors.New("api: the change has no role")))
		return
	}
	res, err := h.users.SetRole(r.Context(), domain.ViewerFrom(r.Context()), web.ClientIP(r), r.PathValue("handle"), *in.Role)
	answer(h, w, r, res, err)
}

// adminGroups is GET /api/v1/admin/groups.
func (h *handlers) adminGroups(w http.ResponseWriter, r *http.Request) {
	res, err := h.groups.List(r.Context(), domain.ViewerFrom(r.Context()))
	answer(h, w, r, res, err)
}

// adminCreateGroup is POST /api/v1/admin/groups with {slug, name,
// description}, answered 201.
func (h *handlers) adminCreateGroup(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Slug        string `json:"slug"`
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := decodeStrict(r.Body, &in); err != nil {
		h.fail(w, r, err)
		return
	}
	res, err := h.groups.Create(r.Context(), domain.ViewerFrom(r.Context()), web.ClientIP(r), in.Slug, in.Name, in.Description)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Location", "/api/v1/admin/groups/"+strconv.FormatInt(res.ID, 10))
	writeJSON(w, http.StatusCreated, res)
}

// adminGroup is GET /api/v1/admin/groups/{id}.
func (h *handlers) adminGroup(w http.ResponseWriter, r *http.Request) {
	id, err := groupID(r)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	res, err := h.groups.Detail(r.Context(), domain.ViewerFrom(r.Context()), id)
	answer(h, w, r, res, err)
}

// adminUpdateGroup is PATCH /api/v1/admin/groups/{id} with {name?,
// description?}.
func (h *handlers) adminUpdateGroup(w http.ResponseWriter, r *http.Request) {
	id, err := groupID(r)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	var in struct {
		Name        *string `json:"name"`
		Description *string `json:"description"`
	}
	if err := decodeStrict(r.Body, &in); err != nil {
		h.fail(w, r, err)
		return
	}
	res, err := h.groups.Update(r.Context(), domain.ViewerFrom(r.Context()), web.ClientIP(r), id, in.Name, in.Description)
	answer(h, w, r, res, err)
}

// adminDeleteGroup is DELETE /api/v1/admin/groups/{id}, answered 204.
func (h *handlers) adminDeleteGroup(w http.ResponseWriter, r *http.Request) {
	id, err := groupID(r)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	if err := h.groups.Delete(r.Context(), domain.ViewerFrom(r.Context()), web.ClientIP(r), id); err != nil {
		h.fail(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}

// adminAddMember is POST /api/v1/admin/groups/{id}/members with {handle}.
func (h *handlers) adminAddMember(w http.ResponseWriter, r *http.Request) {
	id, err := groupID(r)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	var in struct {
		Handle *string `json:"handle"`
	}
	if err := decodeStrict(r.Body, &in); err != nil {
		h.fail(w, r, err)
		return
	}
	if in.Handle == nil {
		h.fail(w, r, problem.Coded(problem.CodeBadRequest, errors.New("api: the member has no handle")))
		return
	}
	res, err := h.groups.AddMember(r.Context(), domain.ViewerFrom(r.Context()), web.ClientIP(r), id, *in.Handle)
	answer(h, w, r, res, err)
}

// adminRemoveMember is DELETE /api/v1/admin/groups/{id}/members/{handle}.
func (h *handlers) adminRemoveMember(w http.ResponseWriter, r *http.Request) {
	id, err := groupID(r)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	res, err := h.groups.RemoveMember(r.Context(), domain.ViewerFrom(r.Context()), web.ClientIP(r), id, r.PathValue("handle"))
	answer(h, w, r, res, err)
}

// adminGrid is GET /api/v1/admin/permissions.
func (h *handlers) adminGrid(w http.ResponseWriter, r *http.Request) {
	res, err := h.groups.Grid(r.Context(), domain.ViewerFrom(r.Context()))
	answer(h, w, r, res, err)
}

// adminApply is POST /api/v1/admin/permissions with {changes}.
func (h *handlers) adminApply(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Changes []groups.Change `json:"changes"`
	}
	if err := decodeStrict(r.Body, &in); err != nil {
		h.fail(w, r, err)
		return
	}
	res, err := h.groups.Apply(r.Context(), domain.ViewerFrom(r.Context()), web.ClientIP(r), in.Changes)
	answer(h, w, r, res, err)
}

// adminPreview is POST /api/v1/admin/permissions/preview with {handle,
// changes}. It changes nothing, but it is a POST because the batch is a
// body.
func (h *handlers) adminPreview(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Handle  *string         `json:"handle"`
		Changes []groups.Change `json:"changes"`
	}
	if err := decodeStrict(r.Body, &in); err != nil {
		h.fail(w, r, err)
		return
	}
	if in.Handle == nil {
		h.fail(w, r, problem.Coded(problem.CodeBadRequest, errors.New("api: the preview names no account")))
		return
	}
	res, err := h.groups.Preview(r.Context(), domain.ViewerFrom(r.Context()), *in.Handle, in.Changes)
	answer(h, w, r, res, err)
}
