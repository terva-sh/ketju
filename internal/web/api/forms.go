package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/problem"
)

// The admin form routes (docs/07 "Moderation and admin"): read a form and
// its versions, save a version, and preview a draft. Every one is behind
// the admin gate, and formsvc checks the role again. Every answer is
// no-store: it is an admin's view of data that changes when any admin
// saves.
func (h *handlers) registerForms(mux *http.ServeMux) {
	h.adminRoute(mux, "GET /api/v1/admin/forms/{key}", adminGate, h.formAdmin)
	h.adminRoute(mux, "GET /api/v1/admin/forms/{key}/versions/{version}", adminGate, h.formVersion)
	h.adminRoute(mux, "POST /api/v1/admin/forms/{key}/versions", adminGate, h.createFormVersion)
	h.adminRoute(mux, "POST /api/v1/admin/forms/{key}/preview", adminGate, h.previewForm)
}

// formAdmin is GET /api/v1/admin/forms/{key}: the active version.
func (h *handlers) formAdmin(w http.ResponseWriter, r *http.Request) {
	h.writeFormAdmin(w, r, http.StatusOK, 0)
}

// formVersion is GET /api/v1/admin/forms/{key}/versions/{version}.
func (h *handlers) formVersion(w http.ResponseWriter, r *http.Request) {
	s := r.PathValue("version")
	n, err := strconv.ParseInt(s, 10, 32)
	if err != nil || n < 1 {
		h.fail(w, r, problem.Coded(problem.CodeBadRequest, fmt.Errorf("api: version %q is not a positive integer", s)))
		return
	}
	h.writeFormAdmin(w, r, http.StatusOK, int(n))
}

// createFormVersion is POST /api/v1/admin/forms/{key}/versions, taking
// {base_version, schema}. It answers 201 with the form at its new version.
func (h *handlers) createFormVersion(w http.ResponseWriter, r *http.Request) {
	var in struct {
		BaseVersion *int            `json:"base_version"`
		Schema      json.RawMessage `json:"schema"`
	}
	if err := decodeStrict(r.Body, &in); err != nil {
		h.fail(w, r, err)
		return
	}
	if in.BaseVersion == nil || in.Schema == nil {
		h.fail(w, r, problem.Coded(problem.CodeBadRequest, errors.New("api: base_version and schema are required")))
		return
	}
	n, err := h.forms.Create(r.Context(), domain.ViewerFrom(r.Context()), r.PathValue("key"), *in.BaseVersion, in.Schema, remoteIP(r))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.writeFormAdmin(w, r, http.StatusCreated, n)
}

// previewForm is POST /api/v1/admin/forms/{key}/preview, taking {schema}.
// It answers the draft rendered, and writes nothing.
func (h *handlers) previewForm(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Schema json.RawMessage `json:"schema"`
	}
	if err := decodeStrict(r.Body, &in); err != nil {
		h.fail(w, r, err)
		return
	}
	if in.Schema == nil {
		h.fail(w, r, problem.Coded(problem.CodeBadRequest, errors.New("api: schema is required")))
		return
	}
	f, err := h.forms.Preview(r.Context(), domain.ViewerFrom(r.Context()), r.PathValue("key"), in.Schema)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, f)
}

func (h *handlers) writeFormAdmin(w http.ResponseWriter, r *http.Request, status, version int) {
	out, err := h.forms.Admin(r.Context(), domain.ViewerFrom(r.Context()), r.PathValue("key"), version)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, status, out)
}

// remoteIP is the request's address for an audit row, as the per-address
// rate limiter reads it (web.ClientIP), never X-Forwarded-For (docs/08).
func remoteIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
