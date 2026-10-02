---
schema: 3
id: TKT-01M3YME9KGJ1XW7RHCRFZ621A0
title: Answer method-not-allowed for a wrong verb on an API route
type: bug
status: draft
status_reason: null
priority: low
due_on: null
labels:
  - area:api
assignees: []
milestone: phase-4
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-02T15:41:33Z
updated_at: 2026-10-02T17:27:52Z
created_by:
  id: agent:claude-code/phase4-run-orchestrator
  name: ""
updated_by:
  id: agent:claude-code/phase4-groom-correctness
  name: ""
extensions: {}
---

## Description

The problem catalogue (docs/07) has `method-not-allowed`, "Right route, wrong verb". A wrong verb on a real `/api/v1` route answers `not-found` instead. `render_test.go:250` and `users_test.go:100` accept either answer, so nothing pins the behaviour.

docs/07 around line 681 also still shows the thread page's ETag without the digest that line 701 and the code (`view.go:515-522`) add.

Both found on 2026-10-02 by the check of TKT-01M29BKBMC.

### What to do
- Answer `method-not-allowed` with an `Allow` header for a known route and an unknown verb, and make the two tests require it.
- Correct the docs/07 example.

## Acceptance criteria

- [ ] A wrong verb on any /api/v1 route answers 405 method-not-allowed with Allow listing exactly the path's methods (HEAD with GET); TestEveryRouteAnswersAWrongVerbWithMethodNotAllowed passes over api.Routes
- [ ] An unknown path is still not-found with no Allow, and HEAD on a GET route is served; TestAnUnknownPathIsStillNotFound and TestHeadOnAGetRouteIsServed pass
- [ ] render_test.go and users_test.go require method-not-allowed and the exact Allow
- [ ] docs/07's catalogue row names Allow, and its ETag example carries the renderer and the digest as view.go writes them

## Implementation plan

Groomed 2026-10-02 for the Phase 4 run (docs/15), against main at ed82f15e.

### Settled in grooming

- **Placement and origin.** Filed by the Phase 1 cleanup. The orchestrator's verification note on TKT-01M29BKBMC (JSON API and service layer), criterion 3, holds, with one caveat: a wrong verb answers `not-found`. docs/15 "Scope" places this ticket in Phase 4.
- **Why it answers not-found today.** `build` in `internal/web/api/api.go` registers every route with a method (`"GET /api/v1/me"`), then a catch-all `"/api/v1/"` that writes `CodeNotFound`. Go's `ServeMux` answers 405 only when no pattern matches the request with its method. The catch-all matches any method, so it wins.
- **The rule (docs/07 "Error catalogue": `method-not-allowed`, "Right route, wrong verb").** When the path matches a registered route under some other method, the answer is `method-not-allowed`, with an `Allow` header listing the methods the path accepts. Otherwise it is `not-found`, as now.
  - A `GET` route also accepts `HEAD`, because `ServeMux` serves `HEAD` through `GET`, so `Allow` lists both.
  - The order is `GET, HEAD, POST, PUT, PATCH, DELETE`. `internal/web/health` already writes `Allow: GET, HEAD` in that form.
- **How the fallback finds the methods: it asks the mux.** For each of the five methods, it clones the request with that method and calls `mux.Handler`. Any pattern other than the catch-all means the method is allowed. That uses the mux's own matching, wildcards and `{$}` included, so there is no second matcher to drift from the route table. Alternatives:
  - matching the path against the route table by hand lost, because it re-implements `ServeMux`'s pattern rules;
  - dropping the catch-all, so that `ServeMux` answers 405 itself, lost. The mux would then write its plain-text 404 and 405, not problem+json, and docs/07 "Error catalogue" says every error response is `application/problem+json`.
- **What an anonymous caller learns.** A 405 with `Allow` says that a path is a route. docs/07 and the client bundle already publish every route, admin routes included, so this reveals nothing new. The route's own gate still answers its own verb as before.
- **Order relative to middleware.** CSRF, the pre-user allowlist and the rate limiters run outside the API mux and are unchanged. An unsafe verb sent without a CSRF token is still refused there first. The fallback is not charged to a limiter, as the catch-all is not today.
- **The docs/07 ETag example.** Line 681 shows `ETag: "3yymw47g00001-483920-r3"`. The format, given at about line 701 and written by `etag` in `view.go`, is `"<id>-<log_length>-r<renderer>-<digest>"`, where the digest is 16 hex digits. The example becomes `"3yymw47g00001-483920-r5-6b1f0e2a9c4d7e13"`, with renderer 5 as `render.Version` is now. The digest is illustrative, and the sentence after the example already explains it.

### Approach

1. `internal/web/api/api.go` `build`: replace the catch-all's closure with `h.unrouted(mux)`, a handler that:
   1. computes the allowed methods by probing `mux.Handler`;
   2. if there are none, writes `CodeNotFound`;
   3. otherwise sets `Allow` and writes `CodeMethodNotAllowed`.

   The probe's clone uses `r.Clone(r.Context())` with `Method` set and the body left alone, and it never runs the handler it finds.
2. `render_test.go:250` and `users_test.go:100` require `method-not-allowed` and the exact `Allow` (`POST` for `/api/v1/render`, `GET, HEAD` for `/api/v1/users`), instead of accepting either code.
3. docs/07:
   - In the catalogue row for `method-not-allowed`, add "with `Allow` naming the methods the route accepts".
   - Correct the ETag example as above.

### Tests

- `TestEveryRouteAnswersAWrongVerbWithMethodNotAllowed` (`internal/web/api`). It walks `api.Routes(deps)` and groups the patterns by path. For each path:
  - it sends one method the path does not accept, filling path parameters as `adminWorld.request` does;
  - the answer is 405 with problem code `method-not-allowed`;
  - `Allow` lists exactly the path's methods, with `HEAD` when `GET` is among them.

  A route added later is covered with no edit.
- `TestAnUnknownPathIsStillNotFound`: `GET /api/v1/nope` and `DELETE /api/v1/threads` (no such route under any method) answer `not-found` with no `Allow`.
- `TestHeadOnAGetRouteIsServed`: `HEAD /api/v1/forums` is answered as `GET` is, not 405.
- The two tightened tests in `render_test.go` and `users_test.go`.
- `TestCatalogueGolden` and `TestTheDocumentedCatalogueMatchesTheCode` keep passing.

### Out of scope

- CORS preflight. The API serves no cross-origin callers, so `OPTIONS` on a route gets 405 with `Allow` like any other wrong verb.
- Routes outside `internal/web/api`: `/auth/*`, `/healthz` and the SPA shell already answer 405 themselves.
- The rest of docs/07's cache-key prose next to the ETag example. It is not what the description names.
