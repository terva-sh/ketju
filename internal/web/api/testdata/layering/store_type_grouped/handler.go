// Package api is a fixture for TestTheLayeringScanFindsAViolation: a
// handlers type declared in one group with Deps names session.Store. Deps
// may; handlers may not (review 1961). It is parsed, never compiled.
package api

import (
	"net/http"

	"github.com/terva-sh/ketju/internal/auth/session"
)

type (
	Deps struct {
		Sessions *session.Store
	}
	handlers struct {
		sessions *session.Store
	}
)

func (h *handlers) sessionList(w http.ResponseWriter, r *http.Request) {
	s := h.sessions
	s.ListForUser(r.Context(), 1)
}
