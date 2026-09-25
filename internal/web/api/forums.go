package api

import (
	"net/http"

	"github.com/terva-sh/ketju/internal/domain"
)

// forumTree is GET /api/v1/forums (docs/07): the forums the viewer may read,
// with their counts and latest activity. The body depends on who is asking
// and changes with every post anywhere, and it has no ETag yet, so no cache
// keeps it.
func (h *handlers) forumTree(w http.ResponseWriter, r *http.Request) {
	tree, err := h.forums.Tree(r.Context(), domain.ViewerFrom(r.Context()))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, tree)
}
