// Package api is a fixture for TestTheLayeringScanFindsAViolation: it does
// everything the layering rules allow, and the scan finds nothing. It is
// parsed, never compiled.
package api

import (
	"net/http"

	"github.com/terva-sh/ketju/internal/auth/session"
	"github.com/terva-sh/ketju/internal/service/users"
	"github.com/terva-sh/ketju/internal/store/db/views"
	"github.com/terva-sh/ketju/internal/store/threadid"
)

// Deps may name the store: serve hands it in.
type Deps struct {
	Users    *users.Service
	Sessions *session.Store
}

type handlers struct {
	identity *identityService
	lists    views.CrossList
}

type identityService struct{}

func (*identityService) ListSessions() {}

// build may read the database handle and wire the store.
func build(d Deps) *handlers {
	sessions := d.Sessions
	if sessions == nil {
		sessions = &session.Store{DB: d.Users.DB, Now: d.Users.Now}
	}
	sessions.Sweep(nil)
	return &handlers{identity: &identityService{}}
}

// A handler reads the request's session, clears the cookie, parses an id,
// and calls a service.
func (h *handlers) sessionList(w http.ResponseWriter, r *http.Request) {
	sess, _ := session.From(r.Context())
	_ = sess.ID()
	http.SetCookie(w, session.ClearCookie())
	_, _ = threadid.Parse(r.PathValue("id"))
	h.identity.ListSessions()
	out := struct{ Sessions []string }{}
	out.Sessions = append(out.Sessions, "")
}
