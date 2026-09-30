package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/web"
)

// The site settings (TKT-01M29CXK2H, docs/07 "Site settings"). Settings
// change what the site does, so both routes are an admin's, and holding
// moderate somewhere opens neither (08 "Decided 2026-09-30"). The service
// asks again. Every answer is no-store.
func (h *handlers) registerSiteSettings(mux *http.ServeMux) {
	h.adminRoute(mux, "GET /api/v1/admin/settings", adminGate, h.adminSettings)
	h.adminRoute(mux, "PUT /api/v1/admin/settings/{key}", adminGate, h.adminSetSetting)
}

// adminSettings is GET /api/v1/admin/settings.
func (h *handlers) adminSettings(w http.ResponseWriter, r *http.Request) {
	res, err := h.sitesettings.Get(r.Context(), domain.ViewerFrom(r.Context()))
	answer(h, w, r, res, err)
}

// adminSetSetting is PUT /api/v1/admin/settings/{key} with {value}, JSON of
// the key's kind. It answers with the key as it now stands. value is
// required, so an empty body is not read as a value.
func (h *handlers) adminSetSetting(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Value json.RawMessage `json:"value"`
	}
	if err := decodeStrict(r.Body, &in); err != nil {
		h.fail(w, r, err)
		return
	}
	if in.Value == nil {
		h.fail(w, r, problem.Coded(problem.CodeBadRequest, errors.New("api: value is required")))
		return
	}
	res, err := h.sitesettings.Set(r.Context(), domain.ViewerFrom(r.Context()), web.ClientIP(r), r.PathValue("key"), in.Value)
	answer(h, w, r, res, err)
}
