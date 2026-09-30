package api

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/service/member"
)

// The admin API is everything under /api/v1/admin/ (docs/07 "Moderation and
// admin"). Every route there is registered through adminRoute with one of
// three gates, which runs before the handler does:
//
//   - admin admits an active admin;
//   - staff admits an active moderator or admin, by role;
//   - moderates admits an active member who may moderate somewhere: staff by
//     role, or a member whose groups grant moderate in at least one forum
//     (perm.ModeratesAny). The moderator queue uses it, and its handlers
//     still filter what they show by per-forum moderate. So do the thread
//     moderation routes, lock, unlock, pin, unpin, move and slow mode, whose
//     threads service decides the forum at the log length (TKT-01M3RQP0TG).
//
// Each gate answers unauthenticated to a viewer with no member session and
// forbidden to a pre-user session, as member.Require does, and forbidden to
// a member it does not admit. A viewer whose status is not active has no
// role (session.Session.Viewer), so no gate needs a status check of its own.
// A handler that needs a finer rule checks it after its gate.
//
// Routes register through adminRoute rather than on the mux directly so the
// list of admin routes, each with its gate, is data: TestAdminRoutesAreGated
// walks it, and covers a route added later without being edited.

// adminPrefix is the path every admin route is under.
const adminPrefix = "/api/v1/admin/"

// gate is one of the three admission rules, named so the route list can say
// which one a route has.
type gate struct {
	name  string
	admit func(h *handlers, w http.ResponseWriter, r *http.Request) bool
}

var (
	adminGate     = gate{"admin", (*handlers).admin}
	staffGate     = gate{"staff", (*handlers).staff}
	moderatesGate = gate{"moderates", (*handlers).moderates}
)

// adminRouteEntry is one registered admin route.
type adminRouteEntry struct {
	pattern string
	gate    gate
	// handler is what the mux runs: the gate, then the route's handler.
	handler http.HandlerFunc
}

// registerAdmin registers every admin route. Each section's ticket adds its
// routes here, through adminRoute.
func (h *handlers) registerAdmin(mux *http.ServeMux) {
	h.registerForms(mux)        // forms.go, TKT-01M29CP2KE
	h.registerAdminUsers(mux)   // admin_users.go, TKT-01M29CXK1X
	h.registerLifecycle(mux)    // account.go, TKT-01M29CKRFS
	h.registerApplications(mux) // applications.go, TKT-01M29CP2MG
	h.registerModeration(mux)   // moderate.go, TKT-01M29CKRGS
	h.registerAgents(mux)       // agents.go, TKT-01M29P9RSW
	h.registerQueue(mux)        // admin_queue.go, TKT-01M29CRJ4E
	h.registerSlowMode(mux)     // slowmode.go, TKT-01M29CRJ50
}

// adminRoute registers handler for pattern behind g, and records the route.
// pattern must name a method and a path under /api/v1/admin/; anything else
// is a wiring mistake and panics at startup.
func (h *handlers) adminRoute(mux *http.ServeMux, pattern string, g gate, handler http.HandlerFunc) {
	method, path, ok := strings.Cut(pattern, " ")
	if !ok || method == "" || !strings.HasPrefix(path, adminPrefix) || g.admit == nil {
		panic(fmt.Sprintf("api: admin route %q must be \"METHOD %s...\" with a gate", pattern, adminPrefix))
	}
	gated := func(w http.ResponseWriter, r *http.Request) {
		if g.admit(h, w, r) {
			handler(w, r)
		}
	}
	h.adminRoutes = append(h.adminRoutes, adminRouteEntry{pattern: pattern, gate: g, handler: gated})
	mux.HandleFunc(pattern, h.charged(method, path, gated))
}

// admin admits an active admin, or writes the refusal and reports false.
func (h *handlers) admin(w http.ResponseWriter, r *http.Request) bool {
	v := domain.ViewerFrom(r.Context())
	return h.admitted(w, r, v, v.Role == domain.RoleAdmin, "the admin gate")
}

// staff admits an active moderator or admin by role, or writes the refusal
// and reports false.
func (h *handlers) staff(w http.ResponseWriter, r *http.Request) bool {
	v := domain.ViewerFrom(r.Context())
	return h.admitted(w, r, v, v.Role == domain.RoleAdmin || v.Role == domain.RoleModerator, "the staff gate")
}

// moderates admits an active member who may moderate somewhere, or writes
// the refusal and reports false.
func (h *handlers) moderates(w http.ResponseWriter, r *http.Request) bool {
	v := domain.ViewerFrom(r.Context())
	// False, at no cost, for a viewer with no member session, whom
	// admitted then refuses as member.Require does.
	ok, err := h.perm.ModeratesAny(r.Context(), v)
	if err != nil {
		h.fail(w, r, err)
		return false
	}
	return h.admitted(w, r, v, ok, "the moderates gate")
}

// admitted answers for a gate: a viewer with no member session is refused
// as member.Require refuses them, and a member the gate does not admit is
// forbidden.
func (h *handlers) admitted(w http.ResponseWriter, r *http.Request, v domain.Viewer, ok bool, what string) bool {
	if err := member.Require(v, "pass "+what); err != nil {
		h.fail(w, r, err)
		return false
	}
	if !ok {
		h.fail(w, r, problem.Coded(problem.CodeForbidden, errors.New("api: "+what+" does not admit this member")))
		return false
	}
	return true
}
