// Package api is the JSON API under /api/v1 (docs/07 "Resources (v1)").
//
// Handlers here parse a request, call a service, and write a dto value or a
// problem. They hold no rules of their own: what a thread page contains is
// internal/service/threads's to decide, so the HTML client can ask the same
// question and get the same answer.
package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/terva-sh/ketju/internal/auth/session"
	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/perm"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/service/account"
	"github.com/terva-sh/ketju/internal/service/applications"
	"github.com/terva-sh/ketju/internal/service/auditlog"
	"github.com/terva-sh/ketju/internal/service/blobs"
	formsvc "github.com/terva-sh/ketju/internal/service/forms"
	"github.com/terva-sh/ketju/internal/service/forums"
	"github.com/terva-sh/ketju/internal/service/groups"
	"github.com/terva-sh/ketju/internal/service/identity"
	"github.com/terva-sh/ketju/internal/service/sitesettings"
	"github.com/terva-sh/ketju/internal/service/threads"
	"github.com/terva-sh/ketju/internal/service/users"
	"github.com/terva-sh/ketju/internal/store/db/views"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/web"
)

// Deps are the services the handlers call.
type Deps struct {
	Threads *threads.Service
	Users   *users.Service
	Forums  *forums.Service
	// Forms answers the admin form routes (forms.go).
	Forms *formsvc.Service
	// Applications answers an applicant's routes under /api/v1/apply
	// (apply.go).
	Applications *applications.Service
	// Perm is the process's evaluator. The admin API's moderates gate asks
	// it (admin.go).
	Perm *perm.Evaluator
	// Blobs answers POST /api/v1/blobs (blobs.go, TKT-01M29CXK0K). It is
	// the one optional service: nil answers the route not-implemented, as
	// web.Groups answers a group it lacks, so a test of something else
	// need not build a blob store.
	Blobs *blobs.Service
	// Sessions lists and ends the viewer's own sessions under
	// /api/v1/settings (settings.go, TKT-01M29CCC0X), through
	// identity.Service: build wires it there, and no handler holds it
	// (TKT-01M3YME9DM). serve passes the store its session middleware
	// reads, so the two share a clock; nil is a store over Users.DB on
	// Users.Now.
	Sessions *session.Store
	// Providers are the identity providers config.toml configures now. An
	// identity on any other provider cannot sign in, so it does not count
	// when another is removed (identity.Service.Unlink). None means no
	// identity may be removed.
	Providers []identity.ProviderName
	Logger    *slog.Logger
	// Now is the clock of the per-identity limiter and of the per-agent
	// bucket; nil is time.Now. Tests set it.
	Now func() time.Time
}

// New returns the /api/v1 handler, for web.Groups.API. It receives full
// paths, so routes are registered as docs/07 writes them.
//
// Every service is required, and a missing one panics here rather than on
// the first request to a route that needs it: a server wired without one is
// a build mistake, and it should not start.
func New(d Deps) http.Handler {
	_, mux := build(d)
	return mux
}

// build is New, with the handlers it registered, for a test that walks the
// admin routes.
func build(d Deps) (*handlers, *http.ServeMux) {
	if d.Threads == nil || d.Users == nil || d.Forums == nil || d.Forms == nil || d.Applications == nil || d.Perm == nil {
		panic(fmt.Sprintf("api.New: Threads, Users, Forums, Forms, Applications and Perm are required "+
			"(Threads set: %t, Users set: %t, Forums set: %t, Forms set: %t, Applications set: %t, Perm set: %t)",
			d.Threads != nil, d.Users != nil, d.Forums != nil, d.Forms != nil, d.Applications != nil, d.Perm != nil))
	}
	log := d.Logger
	if log == nil {
		log = slog.Default()
	}
	sessions := d.Sessions
	if sessions == nil {
		sessions = &session.Store{DB: d.Users.DB, Now: d.Users.Now}
	}
	h := &handlers{threads: d.Threads, users: d.Users, forums: d.Forums, forms: d.Forms, applications: d.Applications,
		perm: d.Perm, blobs: d.Blobs, log: log, applyLimit: web.KeyedLimit(web.ApplyRate, applicantKey, d.Now),
		// The per-agent bucket (agents.go, TKT-01M29P9RSW): one for the
		// handler set, so every route it wraps draws on the same buckets.
		agentLimit: web.KeyedRateLimit(agentCharge, d.Now),
		// The groups service needs nothing Users and Perm do not already
		// carry, so it is built here rather than being one more required
		// dependency (TKT-01M29CXK1X).
		groups: &groups.Service{DB: d.Users.DB, Perm: d.Perm, Now: d.Users.Now},
		// The lifecycle service likewise (TKT-01M29CKRFS), with the thread
		// service a suspension hides and restores posts through
		// (TKT-01M29CP2QK).
		account: &account.Service{DB: d.Users.DB, Perm: d.Perm, Threads: d.Threads, Now: d.Users.Now},
		// Handle changes likewise (TKT-01M3NRP3W9): a rename needs only the
		// database and the clock. Removing a sign-in (TKT-01M29CCC0X) needs
		// the configured providers too, and listing and ending sessions the
		// session store (TKT-01M3YME9DM). No handler here signs anyone in:
		// the auth routes do, through the identity.Service serve builds.
		identity: &identity.Service{DB: d.Users.DB, Sessions: sessions, Perm: d.Perm, Now: d.Users.Now,
			Providers: d.Providers},
		// The audit log likewise (TKT-01M29CP2R4): it only reads.
		auditlog: &auditlog.Service{DB: d.Users.DB},
		// The site settings likewise (TKT-01M29CXK2H): the database and
		// the clock a change is stamped with.
		sitesettings: &sitesettings.Service{DB: d.Users.DB, Now: d.Users.Now}}
	mux := http.NewServeMux()
	for _, rt := range h.routes() {
		mux.HandleFunc(rt.Pattern(), h.charged(rt.Method, rt.Path, rt.Handler))
	}
	h.registerAdmin(mux)
	mux.Handle(unroutedPattern, unrouted(mux))
	return h, mux
}

// unroutedPattern is the catch-all under /api/v1/: every request no route
// matches with its method comes here. It matches any method, so ServeMux
// never answers 405 itself, and every error stays problem+json (docs/07
// "Error catalogue").
const unroutedPattern = "/api/v1/"

// probeMethods are the methods a route may be registered under, in the
// order Allow lists them, as internal/web/health writes it.
var probeMethods = []string{http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch,
	http.MethodDelete}

// unrouted answers a request no route took (TKT-01M3YME9KG). When the path
// is a route's under another method, the answer is method-not-allowed, with
// Allow naming every method the path accepts; HEAD is among them whenever
// GET is, because ServeMux serves HEAD through GET. Otherwise it is
// not-found.
//
// It asks mux which pattern each method would reach, so wildcards and {$}
// match exactly as they do for a real request, and there is no second
// matcher to drift from the route table. The probe never runs the handler
// it finds. A 405 says that a path is a route, which docs/07 and the client
// bundle already publish; the route's own gate still answers its own verb.
func unrouted(mux *http.ServeMux) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var allow []string
		for _, m := range probeMethods {
			p := r.Clone(r.Context())
			p.Method = m
			if _, pattern := mux.Handler(p); pattern != "" && pattern != unroutedPattern {
				allow = append(allow, m)
			}
		}
		if len(allow) == 0 {
			problem.Write(w, r, problem.CodeNotFound)
			return
		}
		w.Header().Set("Allow", strings.Join(allow, ", "))
		problem.Write(w, r, problem.CodeMethodNotAllowed)
	})
}

// routes is every route the API serves outside /api/v1/admin/, whose routes
// register through adminRoute with their gates. build registers exactly
// these, so the table is the route list rather than a copy of it
// (TKT-01M29CKRG9): TestPreUserRoutesAreExactlyTheAllowlist walks it through
// Routes, and TestEveryRouteIsInTheTable fails on a route registered any
// other way.
func (h *handlers) routes() []web.Route {
	get, post, put, patch, del := http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete
	return append([]web.Route{
		{Method: get, Path: "/api/v1/me", Handler: h.me},
		{Method: put, Path: "/api/v1/me/read", Handler: h.markAllRead},
		{Method: get, Path: "/api/v1/unread", Handler: h.crossList(views.Unread)},
		{Method: get, Path: "/api/v1/new", Handler: h.crossList(views.New)},
		{Method: get, Path: "/api/v1/participated", Handler: h.crossList(views.Participated)},
		{Method: get, Path: "/api/v1/watching", Handler: h.crossList(views.Watching)},
		{Method: get, Path: "/api/v1/forums", Handler: h.forumTree},
		{Method: get, Path: "/api/v1/threads/{id}", Handler: h.thread},
		{Method: patch, Path: "/api/v1/threads/{id}", Handler: h.renameThread},
		{Method: get, Path: "/api/v1/threads/{id}/posts/{ordinal}", Handler: h.post},
		{Method: get, Path: "/api/v1/threads/{id}/posts/{ordinal}/replies", Handler: h.replies},
		{Method: get, Path: "/api/v1/threads/{id}/posts/{ordinal}/history", Handler: h.history},
		{Method: get, Path: "/api/v1/threads/{id}/posts/{ordinal}/source", Handler: h.postSource},
		{Method: post, Path: "/api/v1/render", Handler: h.render},
		{Method: post, Path: "/api/v1/threads/{id}/posts", Handler: h.reply},
		{Method: patch, Path: "/api/v1/threads/{id}/posts/{ordinal}", Handler: h.editPost},
		{Method: del, Path: "/api/v1/threads/{id}/posts/{ordinal}", Handler: h.deletePost},
		{Method: post, Path: "/api/v1/threads/{id}/posts/{ordinal}/restore", Handler: h.restorePost},
		{Method: put, Path: "/api/v1/threads/{id}/read", Handler: h.markRead},
		{Method: del, Path: "/api/v1/threads/{id}/read", Handler: h.markUnread},
		{Method: post, Path: "/api/v1/threads/{id}/watch", Handler: h.watchThread},
		{Method: post, Path: "/api/v1/threads/{id}/flags", Handler: h.flag},
		{Method: post, Path: "/api/v1/blobs", Handler: h.upload},
		{Method: get, Path: "/api/v1/drafts/{thread_id}", Handler: h.getDraft},
		{Method: put, Path: "/api/v1/drafts/{thread_id}", Handler: h.putDraft},
		{Method: del, Path: "/api/v1/drafts/{thread_id}", Handler: h.deleteDraft},
		{Method: get, Path: "/api/v1/forums/{slug}/threads", Handler: h.threadList},
		{Method: post, Path: "/api/v1/forums/{slug}/threads", Handler: h.createThread},
		{Method: get, Path: "/api/v1/forums/{slug}/draft", Handler: h.getForumDraft},
		{Method: put, Path: "/api/v1/forums/{slug}/draft", Handler: h.putForumDraft},
		{Method: del, Path: "/api/v1/forums/{slug}/draft", Handler: h.deleteForumDraft},
		{Method: post, Path: "/api/v1/forums/{slug}/watch", Handler: h.watchForum},
		{Method: put, Path: "/api/v1/forums/{slug}/read", Handler: h.markForumRead},
		{Method: get, Path: "/api/v1/users", Handler: h.usersByPrefix},
		{Method: get, Path: "/api/v1/users/{handle}", Handler: h.userProfile},
		{Method: get, Path: "/api/v1/users/{handle}/posts", Handler: h.userPosts},
		{Method: get, Path: "/api/v1/search", Handler: h.search},
		{Method: get, Path: "/api/v1/notifications", Handler: h.notifications},
		{Method: post, Path: "/api/v1/notifications/read", Handler: h.markNotificationsRead},
		{Method: get, Path: "/api/v1/settings", Handler: h.settings},
		{Method: patch, Path: "/api/v1/settings", Handler: h.patchSettings},
		{Method: get, Path: "/api/v1/settings/profile", Handler: h.profileSettings},
		{Method: patch, Path: "/api/v1/settings/profile", Handler: h.patchProfile},
		{Method: put, Path: "/api/v1/settings/avatar", Handler: h.putAvatar},
		{Method: del, Path: "/api/v1/settings/avatar", Handler: h.deleteAvatar},
		{Method: get, Path: "/api/v1/settings/sessions", Handler: h.sessionList},
		{Method: del, Path: "/api/v1/settings/sessions", Handler: h.endEverySession},
		{Method: del, Path: "/api/v1/settings/sessions/{id}", Handler: h.endSession},
		{Method: get, Path: "/api/v1/settings/identities", Handler: h.identityList},
		{Method: del, Path: "/api/v1/settings/identities/{id}", Handler: h.removeIdentity},
		{Method: post, Path: "/api/v1/settings/deactivate", Handler: h.deactivate},
		{Method: get, Path: "/api/v1/settings/handle", Handler: h.handleState},
		{Method: put, Path: "/api/v1/settings/handle", Handler: h.changeHandle},
	}, h.applyRoutes()...)
}

// Routes is every route New serves, the admin routes with their gates
// included, as the handlers New registers. It exists so a test can walk the
// API route by route (TKT-01M29CKRG9); a server is built with New.
func Routes(d Deps) []web.Route {
	h, _ := build(d)
	out := h.routes()
	for _, a := range h.adminRoutes {
		method, path, _ := strings.Cut(a.pattern, " ")
		out = append(out, web.Route{Method: method, Path: path, Handler: a.handler})
	}
	return out
}

type handlers struct {
	threads      *threads.Service
	users        *users.Service
	forums       *forums.Service
	forms        *formsvc.Service
	applications *applications.Service
	perm         *perm.Evaluator
	blobs        *blobs.Service
	groups       *groups.Service
	account      *account.Service
	identity     *identity.Service
	auditlog     *auditlog.Service
	sitesettings *sitesettings.Service
	log          *slog.Logger
	// applyLimit is the per-identity stage the application writes share
	// (apply.go). There is one per handler set, so every route it wraps
	// draws on the same buckets.
	applyLimit func(http.Handler) http.Handler
	// agentLimit is the per-agent token bucket every write route but the
	// read position, drafts and watches is charged to (agents.go).
	agentLimit func(http.Handler) http.Handler

	// adminRoutes is every route adminRoute registered, with its gate.
	adminRoutes []adminRouteEntry
}

// me is GET /api/v1/me (docs/07): the viewer, their unread counts and their
// settings. It is the viewer's own and changes with every read, so no cache
// keeps it.
func (h *handlers) me(w http.ResponseWriter, r *http.Request) {
	me, err := h.users.Me(r.Context())
	if err != nil {
		h.fail(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, me)
}

// thread is GET /api/v1/threads/{id}?page=&ordinal=&lens=&after_log_length=
// (docs/07; ordinal is the permalink parameter, added in TKT-01M29CA9AX).
func (h *handlers) thread(w http.ResponseWriter, r *http.Request) {
	id, err := threadid.Parse(r.PathValue("id"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	q, err := threadQuery(r)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	// The viewer's own page size (TKT-01M29CCC0X), read from the account the
	// request authenticated as. The response cache keys a page by it, so a
	// page drawn at one size is never served at another.
	v := domain.ViewerFrom(r.Context())
	q.PageSize = users.PageSize(r.Context(), v)
	res, err := h.threads.View(r.Context(), v, id, q, r.Header.Get("If-None-Match"))
	respond(h, w, r, res, err)
}

// post, replies and history are GET /api/v1/threads/{id}/posts/{ordinal} and
// its /replies and /history (docs/07).
func (h *handlers) post(w http.ResponseWriter, r *http.Request) {
	id, ordinal, err := postPath(r)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	res, err := h.threads.Post(r.Context(), domain.ViewerFrom(r.Context()), id, ordinal, r.Header.Get("If-None-Match"))
	respond(h, w, r, res, err)
}

func (h *handlers) replies(w http.ResponseWriter, r *http.Request) {
	id, ordinal, err := postPath(r)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	res, err := h.threads.Replies(r.Context(), domain.ViewerFrom(r.Context()), id, ordinal, r.Header.Get("If-None-Match"))
	respond(h, w, r, res, err)
}

func (h *handlers) history(w http.ResponseWriter, r *http.Request) {
	id, ordinal, err := postPath(r)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	res, err := h.threads.History(r.Context(), domain.ViewerFrom(r.Context()), id, ordinal, r.Header.Get("If-None-Match"))
	respond(h, w, r, res, err)
}

// postSource is GET /api/v1/threads/{id}/posts/{ordinal}/source
// (TKT-01M3QJYV4Z): the post's current source, for its author or a
// moderator to edit. It is no-store with no ETag, so neither the response
// cache nor a browser or proxy keeps one reader's source to hand to anyone,
// and an edit always starts from the revision the server holds now.
func (h *handlers) postSource(w http.ResponseWriter, r *http.Request) {
	id, ordinal, err := postPath(r)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	res, err := h.threads.Source(r.Context(), domain.ViewerFrom(r.Context()), id, ordinal)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, res)
}

// respond writes a served body, a 304, or the error.
//
// Vary: Cookie because every body here depends on who is asking, and
// no-cache because every use must revalidate: the ETag makes that a 304 when
// nothing moved, and without it a browser would reuse a response that the
// next post made stale.
func respond[T any](h *handlers, w http.ResponseWriter, r *http.Request, res threads.Served[T], err error) {
	if err != nil {
		h.fail(w, r, err)
		return
	}
	hd := w.Header()
	hd.Set("ETag", res.ETag)
	hd.Set("Vary", "Cookie")
	hd.Set("Cache-Control", "private, no-cache")
	if res.NotModified {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	writeJSON(w, http.StatusOK, res.Body)
}

// postPath parses {id} and {ordinal}.
func postPath(r *http.Request) (threadid.ID, uint32, error) {
	id, err := threadid.Parse(r.PathValue("id"))
	if err != nil {
		return 0, 0, err
	}
	s := r.PathValue("ordinal")
	n, err := strconv.ParseUint(s, 10, 32)
	if err != nil || n < 1 {
		return 0, 0, problem.Coded(problem.CodeBadRequest, fmt.Errorf("api: ordinal %q is not a positive integer", s))
	}
	return id, uint32(n), nil
}

// threadQuery parses the page parameters. A malformed one is a client bug
// and says so, rather than being read as absent; that includes a parameter
// given with no value, such as ?page=, which URL.Query().Get cannot tell from
// one not given at all (review 394).
//
// A page is a signed 32-bit number, so an int on any build. An ordinal is a
// uint32, as in a post's path, so every ordinal parses, and one past the
// end is not-found (docs/07) rather than bad-request (TKT-01M3VVZE4G).
func threadQuery(r *http.Request) (threads.Query, error) {
	v := r.URL.Query()
	var q threads.Query
	positive := func(name string, max int64) (int64, error) {
		if !v.Has(name) {
			return 0, nil
		}
		s := v.Get(name)
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil || n < 1 || n > max {
			return 0, problem.Coded(problem.CodeBadRequest, fmt.Errorf("api: %s=%q is not a positive integer", name, s))
		}
		return n, nil
	}
	page, err := positive("page", math.MaxInt32)
	if err != nil {
		return q, err
	}
	ordinal, err := positive("ordinal", math.MaxUint32)
	if err != nil {
		return q, err
	}
	q.Page, q.Ordinal, q.Lens = int(page), uint32(ordinal), v.Get("lens")
	if v.Has("after_log_length") {
		s := v.Get("after_log_length")
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil || n < 0 {
			return q, problem.Coded(problem.CodeBadRequest, fmt.Errorf("api: after_log_length=%q is not a length", s))
		}
		q.After, q.HasAfter = n, true
	}
	return q, nil
}

func (h *handlers) fail(w http.ResponseWriter, r *http.Request, err error) {
	problem.WriteError(r.Context(), w, r, h.log, err)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		// A dto value that will not marshal is a programming error, and the
		// status line has not gone out yet, so it can still be a 500. The
		// ETag set for the page it failed to write must not go with it.
		w.Header().Del("ETag")
		problem.Write(w, nil, problem.CodeInternal)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(b)
}
