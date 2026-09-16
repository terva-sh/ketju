---
schema: 3
id: TKT-01M29C5Y3SAD25GYYEMHYPDF24
title: Build the HTTP server, router, and middleware chain
type: task
status: in-progress
status_reason: null
priority: urgent
due_on: null
labels:
  - area:api
  - effort:M
assignees: []
milestone: phase-1
parent: TKT-01M29BKBMCNDSYHYF9WXAPDNRN
origin: null
dependencies:
  - TKT-01M29BW9QFZ2C7Q24KN1JW5MFW
  - TKT-01M29BW9QYZCT9JNZ00WNXFKR8
blocks_on: none
references:
  - ref: design:architecture-deployment
    path: docs/02-architecture.md
claim:
  actor: agent:claude-code/web
  branch: http-server
  worktree: /home/sothr/workspace/git.local.sothr.com/terva-sh/ketju
  commit: d0b6fb9cbc46231a5d2646de58f8bec55cd9f297
  session: null
  claimed_at: 2026-09-16T20:03:16Z
  expires_at: null
archive: null
created_at: 2026-09-11T23:17:26Z
updated_at: 2026-09-16T20:04:37Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/web
  name: ""
extensions: {}
---

## Description

`net/http` with the standard library router, and the middleware chain from docs/02: session resolution, rate limiting, CSRF posture, request logging, panic recovery, and body limits.

The route table is where the URL scheme in docs/07 becomes real, and it covers more than `/api`: `/auth/*`, `/blobs/*`, `/events`, `/assets/*`, and the SPA shell catch-all. Getting the catch-all right matters, because it must serve the shell for an unknown application path and a 404 for an unknown API path.

Order in the chain is not cosmetic. Rate limiting before session lookup means an unauthenticated flood does not cost a database query each.

## Acceptance criteria

- [x] Every URL prefix in docs/02 routes to its handler group, with a test per prefix
- [x] An unknown path under /api returns a problem response and an unknown application path returns the SPA shell
- [x] A panic in a handler returns 500 and is logged without taking down the server
- [x] Middleware order is documented next to the chain with the reason for each position

## Notes

**agent:claude-code/web** at 2026-09-16T20:04:04Z

Built as `internal/web`: `server.go` (route table, chain), `middleware.go`
(problem responses, panic recovery, body limit), `server_test.go`.

Scope boundary: this ticket produces the root `http.Handler` and stops there.
Listening, the `http.Server`, and drain order belong to TKT-01M29CCBYX
(`ketju serve` with graceful shutdown), so nothing here binds a port.

### Decisions, and what lost

**Handler groups are a struct of named fields, not a map or a registry.**
`Groups{API, Auth, Blobs, Events, Assets, SPA}` mirrors the prefixes in
docs/02 one-for-one. A map keyed by prefix was the alternative and lost
because it moves the route table out of this file: with a map, the set of
prefixes is whatever the caller happened to put in, and the compiler stops
being able to tell anyone they missed one.

**A nil group answers 501, not 404.** Most of these groups do not exist in
Phase 1. "This server does not implement that endpoint" and "there is no such
endpoint" are different facts to both a developer and a monitoring check, and
collapsing them means a missing mount looks exactly like a typo.

**The bare `/api` and `/api/v1` are registered explicitly.** Go 1.22's
ServeMux answers a subtree pattern's bare path with an implicit 307 to the
trailing-slash form. That is right for a browser and wrong here: a client
POSTing JSON to `/api/v1` gets a redirect, and a redirect is the one response
where the request body and method are at the mercy of what the client does
next. Both now route to a 404 problem, which cannot be misfollowed.

**The catch-all splits on `isAPIPath`, not on a prefix test alone.** Anything
under `/api/` that is not this version -- `/api/v2/threads`, `/api/oops` -- is
a client bug and gets a problem response; everything else gets the SPA shell,
because every deep link into a single-page app is indistinguishable from a
path the server does not know. Getting this backwards produces one of the two
classic failures: deep links 404 on refresh, or a mistyped API path returns
200 and a page of HTML that a JSON client reports as a parse error.

Note that `/api/v1/*` never reaches the catch-all -- the API group owns its
own 404s, because only it knows its routes.

**Session, RateLimit and CSRF are named `Options` fields, nil until their own
tickets land** (TKT-01M29C800X for rate limiting and CSRF; session resolution
waits on TKT-01M29BSPEF deciding what a session is). A general "add middleware
here" slice was the alternative and lost for the reason the ticket calls out:
order is a decision, and a chain assembled by whoever calls it is a chain
whose order is nobody's decision. As fields, the order stays a property of
`chain()` and holds as each middleware lands rather than being rediscovered.

**`ErrAbortHandler` is re-panicked rather than recovered.** net/http panics
with that value when a handler aborts a response deliberately. Swallowing it
would turn an intentional abort into a 500 and log a crash that did not
happen.

**Panic values are truncated to 256 characters.** A panic value is frequently
an error carrying whatever the handler was working on, which on this server
can be a post body or a token. The stack is what locates the bug; the value
only needs to identify it.

### Middleware order (AC4)

Documented in the `chain` doc comment with a numbered reason per position.
The two that are load-bearing rather than arbitrary:

- Panic recovery sits *immediately inside* the request log. A panic has to
  become a 500 before the logger observes the status, or every crash is logged
  as the 200 the recorder still held. Outside the log instead, and the line is
  never written at all.
- Rate limiting sits *before* session resolution. A session lookup is a
  database query, so checking the limit afterwards means an unauthenticated
  flood costs a query per request -- the limiter would be protecting the thing
  it is standing behind.

### Verification

Tests were checked by mutation, not just by passing. Four mutations, each
caught by a named test:

1. Serve the SPA shell for unknown API paths -> `/api/v2/threads was answered
   with the SPA shell`.
2. Rate limiting after session -> `ran [session ratelimit csrf handler], want
   [ratelimit session csrf handler]`.
3. Recovery outside the request log -> `the request line does not record the
   500 the client got`.
4. Swallow `ErrAbortHandler` -> `recovered <nil>, want ErrAbortHandler to pass
   through` and `a deliberate abort was logged as a crash`.

Two tests were wrong on first writing: they asserted the catch-all answers
`/api/v1/*`, which it does not, since the API group owns its own 404s. They
were retargeted to `/api/v2/...`.

`go vet ./...` and `go test -race ./...` are clean.

## Summary

internal/web serves the root http.Handler: a ServeMux route table for the six prefixes in docs/02, and the middleware chain in the order docs/02 requires. Nil handler groups answer 501 rather than 404, so a group that is not built yet is distinguishable from a path that does not exist. The bare /api and /api/v1 are registered explicitly to suppress ServeMux's implicit 307, which would put a JSON client's body and method at the mercy of its redirect handling. The catch-all splits on isAPIPath: an unknown path under /api gets an RFC 9457 problem, anything else gets the SPA shell. Panic recovery sits immediately inside the request log so a crash is logged as the 500 it became, and ErrAbortHandler is re-panicked rather than recovered. Session, rate limiting and CSRF are named Options fields, nil until their own tickets land, so the chain order stays a property of this package. Listening and graceful shutdown are TKT-01M29CCBYX and are not here. Order and catch-all behaviour were each confirmed by a mutation that a named test caught.
