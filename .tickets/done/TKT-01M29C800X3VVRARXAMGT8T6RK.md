---
schema: 3
id: TKT-01M29C800X3VVRARXAMGT8T6RK
title: Set the CSRF posture and write rate limits
type: task
status: done
status_reason: AC1, AC2 and AC4 are met and tested; AC3 is met per IP with Retry-After but not per session. Per-session limiting is impossible at the limiter's chain position, which deliberately sits outside session resolution, and docs/08 assigns per-identity limiting to agents via TKT-01M29P9RSW rather than to humans. The ticket text and docs/08 disagree; docs/08 was followed and the conflict is recorded in the note.
priority: high
due_on: null
labels:
  - area:identity
  - area:api
  - effort:S
assignees: []
milestone: phase-1
parent: TKT-01M29BKBKZW26HNHFM1YFFGVZC
origin: null
dependencies:
  - TKT-01M29C5Y3SAD25GYYEMHYPDF24
blocks_on: none
references:
  - ref: design:identity-csrf
    path: docs/05-identity-and-accounts.md
claim: null
archive: null
created_at: 2026-09-11T23:18:33Z
updated_at: 2026-09-19T23:21:16Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/web
  name: ""
extensions: {}
---

## Description

No token plumbing in the SPA. The API is JSON-only and rejects non-JSON content types on mutating routes, cookies are SameSite=Lax, and Go's `CrossOriginProtection` from 1.25 is enabled for the form posts the non-JS client will make.

Enabling `CrossOriginProtection` now, before any form post exists, is one of the seven constraints in docs/07 that keep the second client cheap. It costs nothing today and it is awkward to retrofit once handlers exist.

Rate limits belong here too, but only the transport-level half: per address on `/auth/*` and on writes, with `Retry-After` on refusal.

This paragraph originally read "per IP and per identity on `/auth/*` and on application submission, and per session and per IP on writes", which contradicted docs/08. It was rewritten after the ticket shipped; the note dated 2026-09-19 records the decision and the reasoning. In short: `/auth/*` has no identity to key on, that being what the route is for; the per-identity limit on application submission moved to TKT-01M29BKBM3 with the form it protects; and per-session write limits for members were ruled out in docs/08 rather than deferred.

## Acceptance criteria

- [x] A mutating API request with a non-JSON content type is refused
- [x] CrossOriginProtection is enabled and a cross-origin form post is refused, tested
- [x] Auth and write rate limits are enforced per address, and a limited response says when to retry
- [x] No CSRF token is threaded through the SPA

## Notes

**agent:claude-code/web** at 2026-09-16T21:04:45Z

Built as `internal/web/csrf.go` (JSON-only rule, CrossOriginProtection) and
`internal/web/ratelimit.go` (token buckets), with both wired on by default and
docs/07 and docs/08 updated to record what shipped.

### AC3 is partly met, and the missing half is not a matter of effort

"Auth and write rate limits are enforced per IP and per session." Per IP is
done and tested, both limiters, with `Retry-After` on every refusal. Per
session is not, for two independent reasons, and I found the first by writing
the test and watching it fail.

**The limiter runs outside session resolution.** Chain position 5, session at
6. That ordering is deliberate and was decided in TKT-01M29C5Y3S: a session
lookup is a database query, so limiting after it means an unauthenticated flood
costs a query per request -- the limiter would be protecting the thing it is
standing behind. The consequence is that no viewer exists when the limiter
runs. My first draft keyed on `logging.ViewerFrom`, which at that position
always finds nothing; the test I wrote to prove per-identity keying failed and
showed the key was the address all along. A key that silently never varies is
worse than an honest one, so `clientKey` became `clientIP` and says why.

`TestNoViewerIsVisibleToTheLimiter` now pins the constraint: if somebody moves
session resolution outward to make per-identity limiting possible here, that
test fails and tells them what they traded away.

**docs/08 does not want per-human write limits anyway.** "Humans are unlimited.
There is no per-user post rate limit", on the reasoning that for a community of
known people it is low value and slow mode covers the case a moderator actually
meets. Agents are the ones that get a token bucket, charged against the agent
credential -- which is TKT-01M29P9RSW, not this ticket.

So per-identity limiting needs a second stage after session resolution, and its
subject is agents. I did not build a stub for it, because a per-identity
limiter that applies to humans is the thing docs/08 rules out.

The ticket text and docs/08 disagree here. I followed docs/08, since it is the
document that argues its case, and left AC3 unticked rather than claiming it.
Worth a human deciding which one is right.

### Where the write ceiling stands

One write every 500ms with a burst of 60, per address. It is a
denial-of-service ceiling and not a posting policy: no member typing prose can
reach it, and a client stuck in a retry loop is stopped before it fills a disk.
The comment on `writeRate` says so, and says it must not be tightened until it
does contradict docs/08. Auth is far stricter (10 burst, one per 6s) because
that limiter is a credential-stuffing guard and is meant to bite.

Reads are not limited at all.

### The CSRF posture is three things, none sufficient alone

1. Mutating API routes accept JSON only. A form can send urlencoded, multipart
   or text/plain and nothing else, so a cross-origin form cannot produce a
   request the API will look at. This is the rule that makes a token
   unnecessary.
2. `http.CrossOriginProtection` covers the two places rule 1 cannot: the
   multipart upload route, which must accept a form content type, and the form
   posts the non-JS client will make. Mutation 2 below removes it from the
   upload route and the cross-site multipart gets straight through, which is
   the whole argument for keeping both.
3. `SameSite=Lax` cookies, which belong to the session ticket. Named in the
   comment because this posture assumes it, and a reader deciding to relax it
   should find out there rather than from an incident.

Content types are parsed with `mime.ParseMediaType` rather than prefix-matched,
so `application/json; charset=utf-8` is accepted and `application/jsonp` is
not. A prefix match accepts both.

### Decisions

**Both middleware default to on.** `Options.RateLimit` and `Options.CSRF` were
nil-by-default, which would have meant the posture only applied if `ketju
serve` remembered to pass it. Security middleware that has to be opted into is
security middleware that will be forgotten. To disable one in a test, pass a
middleware that returns its argument unchanged -- explicit at the call site,
which is where that decision should be visible.

**`X-Forwarded-For` is ignored.** Trusting it without a configured trusted-proxy
list would not weaken the limiter but remove it: every request could claim a
fresh key and never be limited. Reading it needs a config setting that does not
exist yet.

**Two generations rather than an LRU** for bucket storage. When the live map
fills it is demoted wholesale and a fresh one takes its place: eviction is O(1),
no request pays for a scan, and memory is bounded at twice the cap. A key that
falls out of both generations gets a fresh bucket, so the failure mode under
memory pressure is permissiveness -- the right direction, since the alternative
is denying a member their allowance because somebody else filled the table,
which turns the limiter into the attack.

**`Retry-After` is rounded up**, so a client that obeys it exactly does not
arrive one tick early and get refused again. `TestObeyingRetryAfterWorks`
checks that, because otherwise the header would be advice that makes things
worse.

**Unimplemented verbs count as mutating.** A limiter that has to be told about
each new method is a limiter with a gap.

### Verification

Eight mutations, each caught by a named test:

1. Prefix-match the content type -> `jsonp is refused, not prefix-matched` and
   the unparseable case both fail.
2. Exempt the upload route from CrossOriginProtection as well -> `cross-site
   multipart to the upload route` gets through.
3. Default the posture off -> `the handler ran for a cross-origin form post on
   a default server`, `upload status = 200, want 403`.
4. Trust X-Forwarded-For -> `rotating X-Forwarded-For bypassed the limiter
   entirely`.
5. Refill without the burst cap -> `allowed 10 after a long idle, want the
   burst of 2`.
6. Drop the old generation instead of promoting -> `victim's bucket was reset
   by other callers, handing them a fresh burst`.
7. Remove the table bound -> `tracking 1280 keys with a bound of 128`.
8. Drop Retry-After -> `a limited response carried no Retry-After`.

Mutation 8 is worth a note: the first attempt left an unused variable and the
package failed to build, which produced no test failure and looked like a
caught mutation if read carelessly. A build failure proves nothing about a
test. It was rerun so that it compiled, and then it failed properly.

Two existing body-limit tests had to start sending JSON once the posture
defaulted to on. They were testing limits, not content types, so that is a
correction to the tests rather than a change in what they measure.

`gofmt`, `go vet` and `go test -race -count=1 ./...` are clean across the
module.

**agent:claude-code/web** at 2026-09-16T21:04:59Z

in-progress to done: AC1, AC2 and AC4 are met and tested; AC3 is met per IP with Retry-After but not per session. Per-session limiting is impossible at the limiter's chain position, which deliberately sits outside session resolution, and docs/08 assigns per-identity limiting to agents via TKT-01M29P9RSW rather than to humans. The ticket text and docs/08 disagree; docs/08 was followed and the conflict is recorded in the note.

**agent:claude-code/web** at 2026-09-19T23:21:12Z

The rate-limit conflict recorded at close is settled. The repository owner chose
to follow docs/08; AC3 was rewritten to describe what the code does and is now
ticked, and the description's original sentence is quoted there so the change is
not silent.

### What the conflict actually was

The closing note called this a single disagreement between AC3 and docs/08. It
was four claims in one line, and only one of them was in conflict:

- `/auth/*` limited per address -- built, never disputed.
- A limited response carries `Retry-After` -- built, never disputed.
- Per *identity* on `/auth/*` and on application submission. Not built, and not
  a conflict with anything. docs/05 asks for it and docs/08 does not contradict
  it, because an applicant is not a member posting. This was read as part of the
  conflict at close, which was wrong.
- Per *session* on writes. This is the only sentence that fought docs/08.

### Why per-session write limits were ruled out rather than deferred

A session is used from one address. So a per-session write ceiling covers
nothing the existing per-address ceiling does not already cover, with one
exception: a session replayed from many addresses at once. That is a stolen
cookie, and a rate limit is not the answer to a stolen cookie -- revocation is,
and the session ticket owns it.

The runaway the per-session limit is usually argued for -- a member's browser tab
stuck in a retry loop -- comes from a single address and is stopped by the
per-address ceiling already. Building the second stage for members would buy a
limiter with no threat left to stop, and a limiter with no threat is a thing
future readers tighten because they assume it must be there for a reason.

The alternative considered and rejected was building the post-session stage now
and configuring it never to fire for humans, so that TKT-01M29P9RSW would not
have to touch middleware ordering later. That trades a real cost -- dead code
with a test pinning its deadness -- against a saved edit in one future ticket,
and the ordering constraint is documented in clientIP where that ticket's author
will be reading anyway.

### Where the remainder went

- Per-identity limiting on application submission: TKT-01M29BKBM3
  "Approval-mode registration and the forms system", as a new acceptance
  criterion. It sits with the form it protects and with the pre-user session it
  keys on, neither of which exists yet.
- Per-agent token bucket: unchanged, still TKT-01M29P9RSW.
- docs/05 and docs/08 disagreed with each other as well, which nobody had
  written down: docs/05 said "keyed by IP and identity" while docs/08 described
  per-address buckets. Both were rewritten to say that this is one sentence
  covering two stages at different points in the chain, and to name which stage
  each caller belongs to.

## Summary

The SPA carries no CSRF token. Three things hold instead: mutating API routes accept JSON only, which a cross-origin HTML form cannot produce since forms send only urlencoded, multipart or text/plain; http.CrossOriginProtection covers the two places that rule cannot, namely the multipart upload route and the form posts the non-JS client will make; and SameSite=Lax cookies, which belong to the session ticket and are named in the comment because this posture assumes them. Content types are parsed rather than prefix-matched, so application/json; charset=utf-8 is accepted and application/jsonp is not. Both middleware default to on, because security middleware that must be opted into will be forgotten; a test disables one by passing a passthrough. Rate limiting is two per-address token buckets, strict on /auth/* as a credential-stuffing guard and generous on writes as a denial-of-service ceiling that docs/08 requires not be tightened into a posting policy. Storage is two generations rather than an LRU, so eviction is O(1) and memory is bounded at twice the cap, and a key that falls out gets a fresh bucket because permissiveness is the right failure direction. Retry-After is rounded up so a client obeying it exactly is not refused again. AC3's per-session half is unmet and unticked: the limiter runs outside session resolution by design, so no viewer exists at that position, and docs/08 wants per-identity limiting to apply to agents rather than humans. That second stage belongs to TKT-01M29P9RSW.
