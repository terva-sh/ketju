package web

import (
	"fmt"
	"net/http"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/problem"
)

// Route is one route a handler group serves: a method, a ServeMux path
// pattern, and the handler. The groups keep their routes as a table of these
// (api.Routes, auth.Routes) rather than as calls on a mux, because a
// ServeMux cannot be asked what it holds, and TestPreUserRoutesAreExactlyTheAllowlist
// has to walk every route there is.
type Route struct {
	Method string
	// Path is a ServeMux path pattern: /api/v1/threads/{id}, or /assets/
	// for a subtree.
	Path    string
	Handler http.HandlerFunc
}

// Pattern is the route as ServeMux names it: "GET /api/v1/me".
func (r Route) Pattern() string { return r.Method + " " + r.Path }

// ApplyStatusPath is the application status page, where a pre-user session
// is sent from every page it may not reach (docs/07 "URL scheme").
const ApplyStatusPath = "/apply/status"

// PreUserRoutes is everything a pre-user session may reach: an approval-mode
// applicant, signed in with an identity and no user (docs/05 "mode =
// approval" step 1: "the application form, the application status page, and
// logout"). Their Handler is unused.
//
// The list is positive on purpose. A route added later is closed to a
// pre-user session until someone adds it here, because the failure it
// prevents is a pending applicant posting in a forum (TKT-01M29CKRG9). A
// pre-user is refused even the public reads an anonymous visitor may make:
// an applicant who wants to read as a guest signs out.
//
// The application endpoints under /api/v1/apply join the list with the
// ticket that registers them (TKT-01M29CP2KZ).
var PreUserRoutes = []Route{
	// Who is signed in: pre_user tells the client to show the status page.
	{Method: http.MethodGet, Path: APIPrefix + "me"},
	// Signing in, with this identity again or another, and signing out.
	{Method: http.MethodGet, Path: "/auth/signin"},
	{Method: http.MethodGet, Path: "/auth/{provider}/start"},
	{Method: http.MethodGet, Path: "/auth/{provider}/callback"},
	{Method: http.MethodPost, Path: "/auth/logout"},
	// The built client, which draws the two pages below.
	{Method: http.MethodGet, Path: "/assets/"},
	// The application form and the status page, served as the shell.
	{Method: http.MethodGet, Path: "/apply"},
	{Method: http.MethodGet, Path: ApplyStatusPath},
}

// PreUserGate refuses a pre-user session every request that matches no
// route in allow, and passes every other viewer through untouched (chain
// position 7). A request under /api/ is answered 403 forbidden, as a
// problem the client can read. Any other GET or HEAD is a page, and is
// answered 303 to the status page, which is what docs/05 step 4 shows an
// applicant who comes back. Anything else is 403 forbidden.
//
// A request is matched as the router would match it: allow is compiled into
// a ServeMux, and a request is let through only when that mux names one of
// allow's own patterns for it. A GET pattern admits HEAD, as it does in the
// router. A path the router would clean first, such as /x/../assets/a.js,
// is let through when its clean form is allowed, because the router answers
// such a path only with a redirect to the clean one, and the redirect comes
// back through here.
//
// It panics on a malformed or conflicting entry, at startup.
func PreUserGate(allow []Route) func(http.Handler) http.Handler {
	mux := http.NewServeMux()
	patterns := map[string]bool{}
	for _, r := range allow {
		if r.Method == "" || r.Path == "" {
			panic(fmt.Sprintf("web: pre-user route %q needs a method and a path", r.Pattern()))
		}
		mux.Handle(r.Pattern(), http.NotFoundHandler())
		patterns[r.Pattern()] = true
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !domain.ViewerFrom(r.Context()).PreUser {
				next.ServeHTTP(w, r)
				return
			}
			if _, pattern := mux.Handler(r); patterns[pattern] {
				next.ServeHTTP(w, r)
				return
			}
			switch {
			case isAPIPath(r.URL.Path):
				problem.Write(w, r, problem.CodeForbidden)
			case r.Method == http.MethodGet || r.Method == http.MethodHead:
				w.Header().Set("Cache-Control", "no-store")
				http.Redirect(w, r, ApplyStatusPath, http.StatusSeeOther)
			default:
				problem.Write(w, r, problem.CodeForbidden)
			}
		})
	}
}
