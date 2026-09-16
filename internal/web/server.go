// Package web is the HTTP surface: the router, the middleware chain, and the
// rules that decide what an unrouted path means.
//
// Handlers live in the packages under this one. What is here is the table that
// says which prefix belongs to whom, and the order the middleware runs in --
// both of which are decisions rather than plumbing, and both of which are
// easier to get wrong than they look.
package web

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/terva-sh/ketju/internal/logging"
	"github.com/terva-sh/ketju/internal/problem"
)

// APIPrefix is where the JSON API lives. The version is in the path because
// docs/07 puts it there: a client pins a version by pinning a URL.
const APIPrefix = "/api/v1/"

// Groups are the handler groups the router mounts, one per URL prefix in
// docs/02.
//
// Every field is optional. A nil group answers 501 rather than 404, because
// during Phase 1 most of these do not exist yet and "this server does not
// implement that endpoint" is a different fact from "there is no such
// endpoint", to both a developer and a monitoring check.
type Groups struct {
	// API serves /api/v1/*. It receives the full path, not a stripped one, so
	// that handlers register the routes docs/07 documents.
	API http.Handler

	// Auth serves /auth/* -- the OIDC start and callback, and logout.
	Auth http.Handler

	// Blobs serves /blobs/* -- attachment reads, outside /api so they can be
	// cached immutably.
	Blobs http.Handler

	// Events serves /events -- the SSE stream. It is outside the API version
	// prefix on purpose (docs/07).
	Events http.Handler

	// Assets serves /assets/* -- the built client, from an embed.FS.
	Assets http.Handler

	// SPA serves the application shell for every path that is not one of the
	// above. See the catch-all rule in New.
	SPA http.Handler
}

// Options configures the server.
type Options struct {
	// Logger is used by the request log and the panic handler. Required in
	// practice; slog.Default is used if it is nil.
	Logger *slog.Logger

	// Limits are the request body bounds. The zero value means MaxPostBody
	// for ordinary requests and the default attachment cap for uploads.
	Limits Limits

	// SiteURL is the public origin, from config. It is added as a trusted
	// origin for the CSRF check: behind a proxy the server sees an internal
	// Host while the browser sends the public one, and without this every
	// write from a real browser would be refused.
	SiteURL string

	// Session, RateLimit and CSRF are the middleware in the positions the
	// chain reserves for them.
	//
	// RateLimit and CSRF default to the real implementations when nil, because
	// security middleware that has to be opted into is security middleware
	// that will be forgotten -- and the one caller that would have to remember
	// is `ketju serve`, which is a different ticket. To disable one in a test,
	// pass a middleware that returns its argument unchanged; that is explicit
	// at the call site, which is where the decision should be visible.
	//
	// Session is nil until TKT-01M29BSPEF decides what a session is. It has no
	// safe default: there is nothing to resolve yet.
	//
	// They are fields rather than a general "add middleware here" list so that
	// the order below stays a property of this file. A chain assembled by
	// whoever calls it is a chain whose order is nobody's decision.
	Session   func(http.Handler) http.Handler
	RateLimit func(http.Handler) http.Handler
	CSRF      func(http.Handler) http.Handler
}

// New returns the server's root handler: the route table wrapped in the
// middleware chain.
func New(g Groups, opts Options) http.Handler {
	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}
	mux := http.NewServeMux()

	// One pattern per prefix in docs/02. The trailing slash is what makes a
	// ServeMux pattern a subtree match.
	mux.Handle(APIPrefix, group(g.API))
	mux.Handle("/auth/", group(g.Auth))
	mux.Handle("/blobs/", group(g.Blobs))
	mux.Handle("/events", group(g.Events))
	mux.Handle("/assets/", group(g.Assets))

	// The bare "/api" and "/api/v1", registered exactly so that ServeMux does
	// not answer them with its implicit redirect to the subtree.
	//
	// That redirect is fine for a browser and wrong here. A client that POSTs
	// JSON to /api/v1 gets a 307, and a redirect is the one response where a
	// request body and a method are at the mercy of whatever the client does
	// next. A 404 that says so is more useful and cannot be misfollowed.
	notFound := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		problem.Write(w, r, problem.CodeNotFound)
	})
	mux.Handle("/api", notFound)
	mux.Handle(strings.TrimSuffix(APIPrefix, "/"), notFound)

	// The catch-all, and the one route with a real decision in it.
	//
	// A path under /api that is not this version -- /api/v2/threads, /api/oops
	// -- is a client bug and must say so: a problem response, which a fetch()
	// can read. Anything under /api/v1/ has already gone to the API group
	// above, which owns its own 404s because only it knows its routes.
	//
	// Everything else is an application route the client knows about and the
	// server does not. Every deep link into a single-page app looks exactly
	// like this, so it gets the shell and the client routes it.
	//
	// Getting this backwards produces the two classic failures: deep links 404
	// on refresh, or a mistyped API path returns 200 and a page of HTML that a
	// JSON client reports as a parse error.
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isAPIPath(r.URL.Path) {
			problem.Write(w, r, problem.CodeNotFound)
			return
		}
		group(g.SPA).ServeHTTP(w, r)
	}))

	return chain(mux, log, opts)
}

// isAPIPath reports whether a path belongs to the API, including the bare
// "/api/v1" with no trailing slash and anything under /api that is not this
// version.
//
// The second case matters: /api/v2/threads is not an application route and
// must not receive the SPA shell just because this build does not serve v2.
func isAPIPath(p string) bool {
	return p == "/api" || p == strings.TrimSuffix(APIPrefix, "/") ||
		strings.HasPrefix(p, "/api/")
}

// group returns h, or a handler that says the group is not implemented.
//
// The 501 body does not name the group. It used to, and the name is gone
// because the catalogue's titles are fixed text: the path already says which
// group was asked for, so interpolating it bought nothing and would have been
// the one title in the catalogue assembled at a call site.
func group(h http.Handler) http.Handler {
	if h != nil {
		return h
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		problem.Write(w, r, problem.CodeNotImplemented)
	})
}

// chain wraps the router in the middleware, outermost first.
//
// The order is the decision this function exists to record, and each position
// has a reason that is not "it felt right":
//
//  1. Request logging, outermost. Every request produces exactly one line,
//     including one that panics or is refused by the rate limiter before any
//     handler sees it. Anything inside a middleware that rejects requests is a
//     middleware whose rejections are invisible.
//
//  2. Panic recovery, immediately inside the log. A panic must become a 500
//     *before* the logger observes the status, or every crash is logged as the
//     200 the recorder still held. Outside the log instead, the log line would
//     never be written at all.
//
//  3. Security headers, outside everything that can answer a request. They are
//     set on the way in, so they are already on the header map when the body
//     limit answers 413, when recovery answers 500, or when the router answers
//
//  404. A response nobody wrote on purpose is exactly the one that must not
//     go out without a policy.
//
//  4. Body limit, before anything reads a body. A limit applied by each
//     handler is a limit the next handler forgets.
//
//  5. Rate limiting, before session resolution. This is the one the HTTP
//     server ticket calls out: a session lookup is a database query, so
//     checking the limit afterwards means an unauthenticated flood costs a
//     query per request -- the limiter would be protecting the thing it is
//     standing behind.
//
//  6. Session resolution. It needs to be past the limiter, and everything
//     below needs the viewer: CSRF binds its token to the session, handlers
//     authorize against it, and the request log reports it.
//
//  7. CSRF, innermost. It has to be after the session because the token is
//     bound to one, and it must not run before the rate limiter, or a flood of
//     tokenless requests would be answered by the more expensive check.
//
// The nil middleware are no-ops, so the order holds as each one lands rather
// than being rediscovered.
func chain(h http.Handler, log *slog.Logger, opts Options) http.Handler {
	csrf := opts.CSRF
	if csrf == nil {
		csrf = CrossOrigin(opts.SiteURL)
	}
	rateLimit := opts.RateLimit
	if rateLimit == nil {
		rateLimit = RateLimit(nil)
	}

	h = csrf(h)                    // 7
	h = wrap(opts.Session, h)      // 6
	h = rateLimit(h)               // 5
	h = limitBody(opts.Limits)(h)  // 4
	h = securityHeaders(h)         // 3
	h = recoverPanics(log)(h)      // 2
	h = logging.Middleware(log)(h) // 1 outermost
	return h
}

func wrap(mw func(http.Handler) http.Handler, h http.Handler) http.Handler {
	if mw == nil {
		return h
	}
	return mw(h)
}
