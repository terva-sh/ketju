package api

import (
	"net/http"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/service/auditlog"
)

// The audit log (TKT-01M29CP2R4, docs/07 "The audit log"). It says who did
// what, and from which address, so it is an admin's alone, and moderators
// see none of it (docs/08 "Decided 2026-09-29"). The service asks again.
// Every answer is no-store.
func (h *handlers) registerAudit(mux *http.ServeMux) {
	h.adminRoute(mux, "GET /api/v1/admin/audit", adminGate, h.adminAudit)
}

// adminAudit is GET /api/v1/admin/audit?actor=&system=&action=&target_type=
// &target_id=&from=&to=&cursor=.
func (h *handlers) adminAudit(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	res, err := h.auditlog.Query(r.Context(), domain.ViewerFrom(r.Context()), auditlog.Query{
		Actor: q.Get("actor"), System: q.Get("system"), Action: q.Get("action"), TargetType: q.Get("target_type"),
		TargetID: q.Get("target_id"), From: q.Get("from"), To: q.Get("to"), Cursor: q.Get("cursor"),
	})
	answer(h, w, r, res, err)
}
