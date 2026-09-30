package api

import (
	"errors"
	"net/http"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/service/threads"
	"github.com/terva-sh/ketju/internal/store/threadid"
)

// flagRequest is the body of POST /api/v1/threads/{id}/flags (docs/07).
type flagRequest struct {
	Ordinal *uint32 `json:"ordinal"`
	Reason  string  `json:"reason"`
	Note    string  `json:"note"`
}

// flag is POST /api/v1/threads/{id}/flags (TKT-01M29CP2PJ): a member's
// report of a post. The rules, and the auto-hide a report may cause, are
// the threads service's (Flag). The answer is dto.FlagReceipt and nothing
// else, the same for every accepted report, and like every answer about the
// viewer's own action it is not cached. A kept flag whose auto-hide failed
// (threads.ErrHideDeferred) is accepted too: it is logged at warn and gets
// the same receipt, since other words would reveal the threshold.
func (h *handlers) flag(w http.ResponseWriter, r *http.Request) {
	v := domain.ViewerFrom(r.Context())
	if err := h.threads.MayWrite(v); err != nil {
		h.fail(w, r, err)
		return
	}
	id, err := threadid.Parse(r.PathValue("id"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	var in flagRequest
	if err := decodeStrict(r.Body, &in); err != nil {
		h.fail(w, r, err)
		return
	}
	if in.Ordinal == nil {
		h.fail(w, r, problem.Coded(problem.CodeBadRequest, errors.New("api: the flag names no ordinal")))
		return
	}
	res, err := h.threads.Flag(r.Context(), v, id, threads.FlagRequest{Ordinal: *in.Ordinal, Reason: in.Reason, Note: in.Note})
	switch {
	case errors.Is(err, threads.ErrHideDeferred):
		h.log.WarnContext(r.Context(), "flag kept, its auto-hide is left to the sweep",
			"thread", id.String(), "ordinal", *in.Ordinal, "err", err)
	case err != nil:
		h.fail(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, res)
}
