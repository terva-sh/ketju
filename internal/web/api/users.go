package api

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/problem"
)

// usersByPrefix is GET /api/v1/users?prefix=&limit= (docs/07): the composer's
// mention autocomplete. Who may ask and what matches are the users service's
// to decide (MaySuggest, Suggest); this asks, reads the parameters, and
// writes the answer. It asks first, so a stranger's malformed query is
// refused as a stranger's (review 522).
//
// prefix is required, and may be empty. limit is optional; given, it must be
// a positive integer, and the service caps it. The answer is the viewer's own
// question about accounts that change, so no cache keeps it.
func (h *handlers) usersByPrefix(w http.ResponseWriter, r *http.Request) {
	v := domain.ViewerFrom(r.Context())
	if err := h.users.MaySuggest(v); err != nil {
		h.fail(w, r, err)
		return
	}
	q := r.URL.Query()
	if !q.Has("prefix") {
		h.fail(w, r, problem.Coded(problem.CodeBadRequest, errors.New("api: /users needs a prefix")))
		return
	}
	limit := 0
	if q.Has("limit") {
		s := q.Get("limit")
		n, err := strconv.ParseInt(s, 10, 32)
		if err != nil || n < 1 {
			h.fail(w, r, problem.Coded(problem.CodeBadRequest, fmt.Errorf("api: limit=%q is not a positive integer", s)))
			return
		}
		limit = int(n)
	}
	res, err := h.users.Suggest(r.Context(), v, q.Get("prefix"), limit)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, res)
}
