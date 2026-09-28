package api

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/service/users"
)

// usersByPrefix is GET /api/v1/users?prefix=&limit= (docs/07): the composer's
// mention autocomplete. Who may ask and what matches are the users service's
// to decide (MaySuggest, Suggest); this asks, reads the parameters, and
// writes the answer. It asks first, so a stranger's malformed query is
// refused as a stranger's (review 522).
//
// prefix is required, and may be empty. limit is optional; given, it must be
// a positive integer, of any size, and is capped at users.MaxSuggestions.
// The answer is the viewer's own question about accounts that change, so no
// cache keeps it.
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
		// Digits only, then their value: ParseUint reports a range error
		// the moment a value overflows, before it has seen the rest of the
		// string, so it cannot also be the syntax check.
		if strings.Trim(s, "0123456789") != "" || strings.Trim(s, "0") == "" {
			h.fail(w, r, problem.Coded(problem.CodeBadRequest, fmt.Errorf("api: limit=%q is not a positive integer", s)))
			return
		}
		// Past uint64 it is still a positive integer, and a limit over the
		// cap is capped, not refused (review 524). The only error left is
		// that range error, whose value is the largest uint64, which the
		// cap takes care of.
		n, _ := strconv.ParseUint(s, 10, 64)
		limit = int(min(n, users.MaxSuggestions))
	}
	res, err := h.users.Suggest(r.Context(), v, q.Get("prefix"), limit)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, res)
}

// userProfile is GET /api/v1/users/{handle} (docs/07). Who may see which
// account, and what its post count counts, are the users service's to decide
// (Profile). The answer depends on the viewer and on accounts that change,
// so no cache keeps it.
func (h *handlers) userProfile(w http.ResponseWriter, r *http.Request) {
	res, err := h.users.Profile(r.Context(), domain.ViewerFrom(r.Context()), r.PathValue("handle"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, res)
}

// userPosts is GET /api/v1/users/{handle}/posts?cursor= (docs/07), one page
// of the account's post history (threads.Service.UserPosts). The service
// admits the viewer before it reads the cursor, so a stranger's malformed
// cursor is refused as a stranger's.
func (h *handlers) userPosts(w http.ResponseWriter, r *http.Request) {
	res, err := h.threads.UserPosts(r.Context(), domain.ViewerFrom(r.Context()), r.PathValue("handle"),
		r.URL.Query().Get("cursor"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, res)
}
