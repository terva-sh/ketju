// Package api is a fixture for TestTheLayeringScanFindsAViolation: a
// handler calls the store that Deps carries. It is parsed, never compiled.
package api

import (
	"net/http"

	"github.com/terva-sh/ketju/internal/auth/session"
)

type Deps struct {
	Sessions *session.Store
}

type handlers struct {
	deps Deps
}

func (h *handlers) sessionList(w http.ResponseWriter, r *http.Request) {
	h.deps.Sessions.ListForUser(r.Context(), 1)
}
