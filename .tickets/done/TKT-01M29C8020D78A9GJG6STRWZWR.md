---
schema: 3
id: TKT-01M29C8020D78A9GJG6STRWZWR
title: Add security headers, CSP nonces, and request limits
type: task
status: done
status_reason: AC1, AC2 and AC4 are met and tested. AC3 requires the Preact client, which does not exist yet; its browser verification is recorded as an inherited check on TKT-01M29CCC02 and the shell-side half is already enforced by TestTheShellHasNoInlineScriptOrStyle.
priority: high
due_on: null
labels:
  - area:api
  - effort:S
assignees: []
milestone: phase-1
parent: TKT-01M29BKBMCNDSYHYF9WXAPDNRN
origin: null
dependencies:
  - TKT-01M29C5Y3SAD25GYYEMHYPDF24
blocks_on: none
references:
  - ref: design:api-security
    path: docs/07-api-and-clients.md
claim: null
archive: null
created_at: 2026-09-11T23:18:33Z
updated_at: 2026-09-16T20:52:10Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/web
  name: ""
extensions: {}
---

## Description

CSP with per-response nonces and no inline scripts, `img-src 'self' data:`, no third-party origins, `frame-ancestors 'none'`, `Referrer-Policy: strict-origin-when-cross-origin`, and `X-Content-Type-Options: nosniff`.

Request limits belong with them: 64 KB for a post body and a configurable attachment cap defaulting to 10 MB.

A strict CSP is easy to add on day one and painful to retrofit, because every inline handler written in the meantime has to be found. The SPA shell is a Go template that injects the nonce, so this ticket and the embedding ticket have to agree on that mechanism.

## Acceptance criteria

- [x] Every response carries the documented headers, tested
- [x] The CSP nonce is per response and the SPA shell template injects it
- [ ] No inline script or style is needed by the client, verified with the CSP in report-only off
- [x] A post body over 64 KB and an upload over the cap are refused with a problem response rather than by a truncated read

## Notes

**agent:claude-code/web** at 2026-09-16T20:51:36Z

Built as `internal/web/security.go` (headers, CSP, nonce) and
`internal/web/shell.go` (the SPA shell template), with the request limits
rewritten in `middleware.go`, a `limits` section added to config, and docs/07's
security section expanded to match what shipped.

### AC3 is not met, and cannot be here

"No inline script or style is needed by the client, verified with the CSP in
report-only off" needs a client. There is no client directory and `Groups.SPA`
was nil until this ticket; TKT-01M29CCC02 scaffolds the Preact client and its
embedding. The browser half of that check belongs to whoever runs it against
a real page.

What is checkable now is the mechanism this ticket owns and the shell it
ships, and that is tested: `TestTheShellHasNoInlineScriptOrStyle` parses the
rendered shell with `golang.org/x/net/html` and fails on any script or style
with an inline body, any script without a nonce, and any `on*` attribute --
an inline event handler being inline script by another name. If that were not
true the client would inherit a shell that already needed `'unsafe-inline'`,
which is the retrofit the ticket exists to avoid.

AC3 should be re-checked when TKT-01M29CCC02 lands. Left unticked rather than
claimed.

### Chain position is the load-bearing decision

Security headers go at position 3, inside panic recovery and outside the body
limit, and they are set on the way *in*. That is what makes them survive
everything downstream: the 413 from the body limit, the 500 from recovery, the
404 from the router. Headers written on the way out would arrive after the
status was already sent and be dropped, and a response nobody wrote on purpose
is exactly the one that must not go out without a policy. Moving the middleware
one position inward is mutation 3 below, and it strips the headers off the 413.

### The nonce travels the opposite way from the viewer

A plain context value is correct here, where it was wrong in
`internal/logging`. The nonce is created by `securityHeaders` on the way in and
read by the shell further down, and a context value travels inward. The viewer
needed a pointer slot precisely because it goes the other way: set deep in the
chain, read by the logger wrapped around the outside. The comment in
`NonceFrom` says so, because the two cases look identical and are not.

### Decisions

**The policy carries six directives docs/07 did not list.** `default-src
'self'` is the no-third-party rule stated positively; `base-uri 'self'` stops
an injected `<base>` re-pointing every relative URL on the page; `form-action
'self'` matters for the non-JS client docs/07 plans; `object-src 'none'`
removes the plugin surface; `connect-src 'self'` bounds fetch and the SSE
stream. docs/07 has been updated to list the policy as shipped.

**`X-Frame-Options` is deliberately absent.** `frame-ancestors` supersedes it
in every browser this targets, and a second header saying the same thing is a
second place to keep in sync.

**A nonce is minted for every response, including JSON ones.** One policy for
every route is simpler to reason about and to test than a policy that varies by
content type, and the cost is one 16-byte `crypto/rand` read per request.

**The shell is `no-store`.** It carries a per-response nonce, so a cached copy
hands a stale nonce to the next reader and the policy blocks every script on
the page. The assets it references are the cacheable part.

**The shell renders into a buffer.** A template that fails halfway through has
already written a partial page if it renders straight to the `ResponseWriter`,
and a partial page arrives with a 200 nobody can take back.

**A shell reached with no nonce in context is a 500, loudly logged.** Serving a
page whose scripts the policy will block is worse than an error: the reader
gets a blank screen and the cause is two packages away.

**Limits are route-dependent.** A single global limit has to be the larger of
the two, which would let every endpoint accept an attachment-sized body. The
choice is made in the middleware rather than per handler, for the reason the
chain exists at all.

**Only the attachment cap is configurable.** docs/07 fixes the post body at
64 KB, and it stays a constant because it is part of what the API promises a
client: an installation that quietly raised it would accept posts another
installation could not take. The attachment cap is bounded above at 512 MB,
because an upload is buffered before the uploader is authenticated and a
four-figure value turns one request into a memory exhaustion tool.

**AC4 needed two mechanisms, not one.** An honest `Content-Length` over the
limit is refused before a byte is read. A chunked body, or one whose
`Content-Length` lies, cannot be judged up front, so `MaxBytesReader` catches
it and `*http.MaxBytesError` was added to the `internal/problem` mapping --
which is what turns it into a problem response rather than the truncated read
the AC rules out. The old `TestTheBodyLimitApplies` only covered the second
path and passed for the wrong reason once the fast path existed; it was split
into two tests that name which mechanism each is measuring.

`DefaultMaxBody` (1 MiB) is gone, replaced by `MaxPostBody` (64 KB) and the
configured upload cap.

### Verification

Seven mutations, each caught by a named test:

1. Add `'unsafe-inline'` to script-src -> `the policy contains "unsafe-inline",
   which defeats the nonce`.
2. One nonce for the whole process -> `nonce "adTnS9..." was reused across
   responses`.
3. Security headers one position inward -> the 413 case loses all three
   headers.
4. Shell mints its own nonce -> `script nonce ... does not match the policy's
   ...; every script on the page would be blocked`.
5. An inline script and an onclick creep into the shell -> both reported.
6. Drop the Content-Length fast path -> `the handler ran for a body the server
   had already decided was too large`.
7. One limit for every route -> `the upload route was measured against the post
   limit`.

`gofmt`, `go vet` and `go test -race -count=1 ./...` are clean across the
module.

**agent:claude-code/web** at 2026-09-16T20:52:09Z

in-progress to done: AC1, AC2 and AC4 are met and tested. AC3 requires the Preact client, which does not exist yet; its browser verification is recorded as an inherited check on TKT-01M29CCC02 and the shell-side half is already enforced by TestTheShellHasNoInlineScriptOrStyle.

## Summary

Every response carries the CSP, Referrer-Policy and X-Content-Type-Options, set on the way in at chain position 3 so they survive the 413 from the body limit, the 500 from recovery and the 404 from the router. The policy carries a fresh per-response nonce on script-src and style-src and never 'unsafe-inline', which would make the nonce decorative. The SPA shell is a Go template rendered per response with that nonce, sent no-store because a cached shell hands a stale nonce to the next reader, and rendered into a buffer so a template failure cannot emit a partial page under a 200. Request limits are route-dependent: 64 KB fixed for a body, a configurable cap defaulting to 10 MB for an upload, bounded above because an upload is buffered before the uploader is authenticated. Both refuse with a problem response through two mechanisms, since an honest Content-Length can be refused unread while a chunked body can only be caught by MaxBytesReader, whose error was added to the internal/problem mapping. AC3 is unmet and unticked: verifying the client needs no inline script requires the client, which TKT-01M29CCC02 scaffolds; that ticket carries a comment naming the inherited check.
