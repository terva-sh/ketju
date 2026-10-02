package api

import (
	"net/http"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/service/threads"
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

// threadList is GET /api/v1/forums/{slug}/threads?cursor=&sort=&tag= (docs/07): a
// page of the forum's threads, pins first on the first page. Like the tree,
// it is the viewer's and moves with every post, so no cache keeps it.
func (h *handlers) threadList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	list, err := h.threads.List(r.Context(), domain.ViewerFrom(r.Context()), r.PathValue("slug"),
		threads.ListQuery{Cursor: q.Get("cursor"), Sort: q.Get("sort"), Tag: q.Get("tag")})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, list)
}
