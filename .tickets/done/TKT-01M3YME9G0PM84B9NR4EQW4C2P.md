---
schema: 3
id: TKT-01M3YME9G0PM84B9NR4EQW4C2P
title: Golden-test what every API endpoint actually serves
type: task
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area:api
assignees: []
milestone: phase-4
parent: null
origin: null
dependencies: []
blocks_on: none
references:
  - ref: pr:362
    path: null
  - ref: pr:390
    path: null
  - ref: pr:392
    path: null
claim: null
archive: null
created_at: 2026-10-02T15:41:33Z
updated_at: 2026-10-03T05:25:19Z
created_by:
  id: agent:claude-code/phase4-run-orchestrator
  name: ""
updated_by:
  id: agent:claude-code/phase4-run-me9g0
  name: ""
extensions: {}
---

## Description

TKT-01M29BKBMC says: "Golden JSON tests cover every endpoint in docs/07 and fail on any shape change". `internal/dto/golden_test.go` covers every DTO type, but builds them from hand-written fixtures. Goldens of what the server actually serves exist only for the thread page, edit, lock and notifications. No test maps the route table (`api.go`, plus about 50 admin routes) to a golden. So a handler that changes what it serves fails no test.

Found on 2026-10-02 by the check of that epic's criterion 2.

### What to do
Add a test that walks the registered API routes and fails for any route without a served-response golden. Then fill the goldens in.

## Acceptance criteria

- [x] TestEveryRouteHasAServedGolden walks api.Routes and fails on a route with no served case, a stale case and a mutation route registered in a test world
- [x] Every route api.Routes returns has a served golden under testdata/golden/served, with status, Content-Type, Cache-Control and any Location, Allow and ETag, and the pending list is deleted
- [x] Every volatile mask names a value random by design, and TestTheVolatileTableNamesOnlyRandomValues passes
- [x] The PR that closes the ticket records a shape mutation (a dto.SessionInfo field) caught by TestServedGoldens, and every PR on the ticket passed a full Terva review without context_limit

## Implementation plan

Groomed 2026-10-02 for the Phase 4 run (docs/15), against main at ed82f15e.

### Settled in grooming

- **Placement and origin.** Filed by the Phase 1 cleanup. The orchestrator's verification note on TKT-01M29BKBMC (JSON API and service layer), criterion 2, "Golden JSON tests cover every endpoint in docs/07 and fail on any shape change", found it partly met. docs/15 "Scope" places this ticket in Phase 4.
- **What exists on main.**
  - `internal/dto/golden_test.go` covers every DTO type from hand-built fixtures.
  - `internal/web/api/testdata/golden/` holds eight served goldens: the thread page for four viewer classes and for a lens, an edit, a lock, and notifications. Together they are about 23 KB.
  - `api.Routes(d)` already returns every route `New` serves, the admin routes included (`api.go`, TKT-01M29CKRG9).
  - Today that is 53 routes from `routes()` and `applyRoutes()`, and 50 from `adminRoute`.
- **Scope: the JSON API under `/api/v1`, the routes `api.Routes` returns.** Endpoints in docs/07 outside it have their own body tests and are not JSON API responses:
  - `GET /api/v1/events` (SSE, `internal/web/sse`);
  - blob bytes (`internal/web/blobs`);
  - `/auth/*` redirects (`internal/web/auth`);
  - `/healthz` (`internal/web/health`, which already has goldens).
- **What a served golden records.** One file per route, `testdata/golden/served/<METHOD>_<path>.json`, where the path's `/` and `{}` become `_`. It holds the response's status; `Content-Type`, `Cache-Control`, `Location`, `Allow` and `ETag` when present; and the body as indented JSON, or `null` for an empty one. One canonical request per route covers its success case. The problem catalogue's error bodies are already pinned by `TestCatalogueGolden`.
- **Determinism is injected, not masked.**
  - The world uses a fixed clock, as `newWorld` does (`clock: 1_757_500_000_000`).
  - Where a value comes from `crypto/rand` (session ids, credential secrets, invite or form tokens), the harness reads it from the response and the database. It replaces that value with a placeholder through one `volatile` table, keyed by route and JSON path, each row with a comment giving the reason.
  - A row may name only a value that is random by design. A timestamp or a counter is fixed instead.
- **A walker, and a pending list that only shrinks.** `TestEveryRouteHasAServedGolden` takes `api.Routes(deps)`. It fails when:
  - a route has neither a case in `servedCases` nor an entry in `pending`;
  - a case or a `pending` entry names no route, because it is stale;
  - a route is in both.

  `pending` exists only so that the work can merge in pieces that each stay under the review's size limit (docs/12 "The merge gate": a review that stops with `context_limit` is not a clean review, as on #47). It is a literal list, frozen at the first PR. A route added after that PR adds a case, never a `pending` entry. The comment above `pending` says so, and the last PR deletes the list.
- **Order with other Phase 4 tickets.** Phase 4 adds routes: split, merge, reply as a new thread, archive, tags, profile editing and the upload bound. Once this walker is on main, each new route fails CI until it has a served case, which is the point. Nothing here waits on those tickets. TKT-01M3YME9AZ (Record quotes as backlinks, as docs/06 promises) adds `quoted_by` to every post, and whichever ticket lands second regenerates the affected goldens with `-update`.
- **Size.** About 103 goldens at 0.3 to 4 KB each, plus the harness. That is too large for one reviewed diff, so the work is three PRs on this one ticket, each a slice that merges green.

### Approach

1. **PR 1: harness, walker and public reads.**
   - `internal/web/api/served_test.go`:
     - `servedWorld(t)` builds on `newWorld` and `newAdminWorld`. It seeds the following in one place:
       - a forum tree with a hidden forum;
       - a thread with replies, an edit, a moderator hide and an attachment;
       - a draft;
       - a notification;
       - a watch;
       - a flag;
       - an application and a form;
       - an agent;
       - a group.
     - `servedCases` maps each route pattern to `{viewer, path, body}`.
     - `TestServedGoldens` runs each case and compares it with its golden, rewriting it under `-update`.
     - It also holds `TestEveryRouteHasAServedGolden` and the `pending` list.
   - Fill in the cases for every public `GET` route outside `/api/v1/admin/`.
   - Leave the existing eight goldens as they are. They pin viewer classes, which this table does not.
2. **PR 2: public writes.** Fill in the cases for every public `POST`, `PUT`, `PATCH` and `DELETE` route, the `/api/v1/apply` routes included. Each case runs against a fresh `servedWorld`, because a write changes what later cases see.
3. **PR 3: admin routes.** Fill in the cases for the 50 admin routes, as an admin, or as a moderator where the route's gate is staff or moderates. Delete `pending`.
4. Each PR quotes its diff size and the review run. If a PR still hits the review limit, split it again by area before retrying, as docs/12 says.

### Tests

- `TestEveryRouteHasAServedGolden`. For a mutation check, register a fixture route through `adminRoute` in a test-only world, and confirm that the walker fails for it.
- `TestServedGoldens`, one subtest per route pattern.
- `TestTheVolatileTableNamesOnlyRandomValues`. Every `volatile` row names a route that has a case and a JSON path present in that route's response, so a stale mask fails.
- A shape check: changing one field of `dto.SessionInfo` in a scratch build fails `TestServedGoldens` for `GET /api/v1/settings/sessions`. The PR records this as a mutation tried, as docs/12 asks.

### Out of scope

- SSE, blob bytes, `/auth/*` and `/healthz`, for the reasons above.
- Error-path goldens per route. The catalogue golden pins every problem body.
- Golden tests for the HTML client (Phase 5, TKT-01M29BN36W).

## Notes

**agent:claude-code/phase4-run-me9g0** at 2026-10-03T03:46:55Z

Claimed 2026-10-02 on main at 2f2f914d. Since the grooming base ed82f15e, `api.Routes` has grown from 103 routes to 105: `GET /api/v1/settings/handle`, `PUT /api/v1/admin/threads/{id}/tags` and `POST /api/v1/admin/threads/{id}/archive` were added, and nothing was removed. The three-PR plan stands and is not revised.

### PR 1: the harness, the walker and the public reads

- **Cases.** `servedCases` covers the 25 public GET routes. `pending` lists the other 80, frozen in this PR: 28 public writes and 52 admin routes. A route that lands on main before this PR merges joins `pending`. After it merges, a new route needs a case.
- **A fresh world for every case, reads included.** The plan required that only for writes. One case takes about 0.1 s, so a shared world was not worth making the goldens depend on the order the cases run in.
- **What a golden holds.** Each golden also records the request line, with the seeded thread ids replaced by `{thread}` and `{other}`, so a reviewer can see what was asked. The body is decoded and re-encoded with sorted keys, which pins every field and value but not key order. A JSON object has no order, and the dto goldens pin the encoder's order.
- **The volatile check is stricter than planned.** `TestTheVolatileTableNamesOnlyRandomValues` serves each masked route in two fresh worlds and requires the masked value to differ. A clock value or a counter would come out the same in both and fail. The only row is a listed session's id, which is derived from the session's random token.
- **The walker's failure tells a later PR what to do.** It names the route and says to add a `servedCases` entry in internal/web/api/served_test.go, to seed what the route needs in `req` or `servedSeed`, to run `go test ./internal/web/api -run TestServedGoldens -update` and commit the golden, and not to add the route to `pending`.
- The eight existing goldens under testdata/golden are unchanged.

**agent:claude-code/phase4-run-me9g0** at 2026-10-03T03:59:58Z

Terva review 2043 (run 3567abc7-5104-4d99-accf-6e873d523de6) on PR #390, head bb5264ff, base 2f2f914d. One medium finding, "Reject trailing content after the decoded JSON body". Accepted and fixed. The harness decoded only the first JSON value in a body, so a response with more bytes after that value would match its golden. `parseServed` now requires a second decode to return `io.EOF`. `TestParseServedRefusesTrailingContent` covers a second value, trailing garbage and a stray `]`, and accepts trailing whitespace. With the EOF check removed, the test fails.

**agent:claude-code/phase4-run-me9g0** at 2026-10-03T04:53:00Z

PR 2 of 3 (public writes): a served case and golden for each of the 28 public write routes; they come off `pending`, which now holds only the 52 admin routes.

Decisions:
- A thread created through the API takes its id from `threadid.Generator`, which read the wall clock. Rather than mask the id, `newGeneratorAt` is exported as `threadid.NewGeneratorAt` and the served world mints ids from its own fixed clock. Masking was rejected because `TestTheVolatileTableNamesOnlyRandomValues` exists to keep times out of the volatile table; an id built from a time is a time.
- POST /api/v1/blobs needs a blob store; the served world wires `blobs.Service` over a per-test temp root with a fixed upload cap, and the case sends a multipart body with a fixed boundary.
- DELETE /api/v1/settings/sessions/{id} needs a second session whose id is random (it derives from the token). The request line names it `{session}` rather than printing the id, so the golden does not vary.
- Each write runs in a fresh world, so a golden is the answer to that one write and the cases do not depend on order.

No golden from PR 1 changed.

**agent:claude-code/phase4-run-me9g0** at 2026-10-03T05:25:11Z

PR 3 of 3 (admin routes): a served case and golden for each of the 52 admin routes. `pending` is deleted and `servedCoverage(routes, cases)` no longer takes it. All 105 routes `api.Routes` returns now have a golden, every one a 2xx, and they are stable across runs (`-count=2`).

Decisions:
- Admin writes stamp times through perm (audit rows) and the forms service (`created_at`). Both now read the served world's clock: `perm.NewAt(db, now)` is new beside `perm.New`, and `formsvc.Service.Now` was already injectable. The alternative was to mask those values. That was rejected for the same reason as the thread id in PR 2: the volatile table holds only values that are random by design, and `TestTheVolatileTableNamesOnlyRandomValues` enforces it.
- dana is seeded as an admin with a session, because admin routes need an admin viewer who is not also the subject of the case. As a side effect, GET /api/v1/forums counts 5 members instead of 4; that is the only PR 1/PR 2 golden that changes.
- POST /api/v1/admin/threads/{id}/move moves the thread to a new public forum, "lounge", not to the restricted forum. bob is a moderator but not a member of the restricted forum, so moving the thread there answers 404, and a 404 golden would be pinning a refusal instead of the route's answer.
- The second volatile row is the credential `POST /api/v1/admin/users/{handle}/credential` issues: `agentcred.Issue` draws it from crypto/rand. The volatile test passes for it.
- The walker's message no longer mentions `pending`, now that the list is gone. The fixture walk still requires a missing case, a stale case and two adminRoute fixture routes to be reported, four problems in all.

Mutation (AC 4): renaming the JSON tag of `dto.SessionInfo.Device` from "device" to "device_name" fails `TestServedGoldens/GET_/api/v1/settings/sessions`. I reverted it afterwards.

Reviews: PR 1 #390 got a medium finding in review 2043 (trailing content after a served body went unrefused). It was fixed, and the second full review was clean (comment 25115). PR 2 #392 had one full review, clean (run 956739ee). None hit context_limit. AC 4 is ticked in this PR before its own review runs, because the merge gate refuses a PR whose review did not run. If PR 3's review fails on context_limit, the PR is split and the tick removed before anything merges.

## Summary

Every route `api.Routes` returns (105) has a served golden under `internal/web/api/testdata/golden/served/`. Each golden records the status, `Allow`, `Cache-Control`, `Content-Type`, `ETag`, `Location` and the canonical body, served through `web.New` with the real session and agent-credential middleware and fixed clocks. This landed in three PRs: #390 (harness, walker, public reads), #392 (public writes), and the third (admin routes; `pending` deleted).

`TestEveryRouteHasAServedGolden` walks `api.Routes`. It fails on a route with no case and on a case that names no route, and it requires adminRoute fixture routes to be reported. Its message tells the PR author exactly what to add. `TestServedGoldens` also fails on a golden no case writes. `TestTheVolatileTableNamesOnlyRandomValues` keeps the volatile table to two values that are random by design: a session id, and an issued agent credential. Values derived from time are pinned through injected clocks instead: `threadid.NewGeneratorAt`, `perm.NewAt`, and `formsvc.Service.Now`.

For later work: a PR that adds an `/api/v1` route adds a `servedCases` entry and runs `go test ./internal/web/api -run TestServedGoldens -update`. A PR that changes a DTO shape regenerates the goldens it moves and reviews that diff as the API change it is.
