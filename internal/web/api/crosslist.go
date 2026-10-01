package api

import (
	"net/http"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/store/db/views"
)

// crossList is GET /api/v1/unread, /new, /participated and /watching with
// ?cursor= (docs/07, TKT-01M29CTY7K), the cross-forum lists. The rules are
// the threads service's (CrossList). Every answer is the viewer's own and
// no-store; none goes near the response cache.
func (h *handlers) crossList(list views.List) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		res, err := h.threads.CrossList(r.Context(), domain.ViewerFrom(r.Context()), list, r.URL.Query().Get("cursor"))
		if err != nil {
			h.fail(w, r, err)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, res)
	}
}

// markAllRead and markForumRead are PUT /api/v1/me/read and PUT
// /api/v1/forums/{slug}/read, mark all read at site and forum level
// (TKT-01M29CTY7K). Each takes no body, or an empty JSON object, and
// answers 200 with dto.MarkedRead, not cached. Like a read position, they
// record the viewer's own state, so an agent's bucket does not pay for them
// (unchargedPaths).
func (h *handlers) markAllRead(w http.ResponseWriter, r *http.Request) {
	v := domain.ViewerFrom(r.Context())
	if err := h.threads.MayKeepState(v); err != nil {
		h.fail(w, r, err)
		return
	}
	if err := optionalJSON(r.Body, maxReadBody, &struct{}{}); err != nil {
		h.fail(w, r, err)
		return
	}
	res, err := h.threads.MarkAllRead(r.Context(), v)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, res)
}

func (h *handlers) markForumRead(w http.ResponseWriter, r *http.Request) {
	v := domain.ViewerFrom(r.Context())
	if err := h.threads.MayKeepState(v); err != nil {
		h.fail(w, r, err)
		return
	}
	if err := optionalJSON(r.Body, maxReadBody, &struct{}{}); err != nil {
		h.fail(w, r, err)
		return
	}
	res, err := h.threads.MarkForumRead(r.Context(), v, r.PathValue("slug"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, res)
}
