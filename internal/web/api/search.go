package api

import (
	"net/http"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/service/threads"
)

// search is GET /api/v1/search?q=&forum=&author=&after=&before=&cursor=
// (docs/07 "Search"), one page of threads.Service.Search. The answer depends
// on the viewer and on posts that change, so it is no-store, and the
// response cache never sees it: a search is no thread page, and its key
// would have to name every forum the viewer may read. It is a read, so an
// agent's bucket is not charged (charged), and a pre-user never reaches it.
func (h *handlers) search(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	res, err := h.threads.Search(r.Context(), domain.ViewerFrom(r.Context()), threads.SearchQuery{
		Q: q.Get("q"), Forum: q.Get("forum"), Author: q.Get("author"),
		After: q.Get("after"), Before: q.Get("before"), Cursor: q.Get("cursor"),
	})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, res)
}
