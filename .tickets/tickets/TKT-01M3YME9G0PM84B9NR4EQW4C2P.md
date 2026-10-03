---
schema: 3
id: TKT-01M3YME9G0PM84B9NR4EQW4C2P
title: Golden-test what every API endpoint actually serves
type: task
status: in-progress
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
claim:
  actor: agent:claude-code/phase4-run-me9g0
  branch: run/me9g0-served-goldens
  worktree: /home/sothr/workspace/git.local.sothr.com/terva-sh/ketju/.claude/worktrees/agent-me9g0
  commit: 2f2f914df101afe6e7a2f4dd9e4c9004319130c5
  session: null
  claimed_at: 2026-10-03T03:41:37Z
  expires_at: null
archive: null
created_at: 2026-10-02T15:41:33Z
updated_at: 2026-10-03T03:59:58Z
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
- [ ] Every route api.Routes returns has a served golden under testdata/golden/served, with status, Content-Type, Cache-Control and any Location, Allow and ETag, and the pending list is deleted
- [ ] Every volatile mask names a value random by design, and TestTheVolatileTableNamesOnlyRandomValues passes
- [ ] The PR that closes the ticket records a shape mutation (a dto.SessionInfo field) caught by TestServedGoldens, and every PR on the ticket passed a full Terva review without context_limit

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
