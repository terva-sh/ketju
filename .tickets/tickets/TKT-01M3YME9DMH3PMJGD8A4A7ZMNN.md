---
schema: 3
id: TKT-01M3YME9DMH3PMJGD8A4A7ZMNN
title: Keep API handlers off the stores, and test the layering
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
claim:
  actor: agent:claude-code/phase4-run-me9dm
  branch: run/me9dm-api-layering
  worktree: /home/sothr/workspace/git.local.sothr.com/terva-sh/ketju/.claude/worktrees/agent-afae7cbdfc4c034cc
  commit: 30834b8e9b8dc69a282dac88cc580503fd08244c
  session: null
  claimed_at: 2026-10-02T19:15:19Z
  expires_at: null
archive: null
created_at: 2026-10-02T15:41:33Z
updated_at: 2026-10-02T19:15:21Z
created_by:
  id: agent:claude-code/phase4-run-orchestrator
  name: ""
updated_by:
  id: agent:claude-code/phase4-run-me9dm
  name: ""
extensions: {}
---

## Description

TKT-01M29BKBMC (JSON API and service layer) says: "No handler in internal/web/api reads from a store package directly". Three handlers in `internal/web/api/settings.go` call `session.Store` themselves (lines 83, 112 and 136: `ListForUser`, `RevokeOwn`, `RevokeUser`), and those run SQL. `handle.go:111` passes `u.DB` into a service function. No test fences the package. The only layering test of this kind is `internal/dto/layering_test.go`.

Found on 2026-10-02 by the check of that epic's criterion 1.

### What to do
- Move the session-list and revoke calls behind a service, as the other handlers are.
- Add a layering test that fails when `internal/web/api` imports a store or session-store package, other than the parsing and enum packages it needs (`threadid`, `views`).

## Acceptance criteria

- [ ] The session list, end-one and end-every handlers call identity.Service (ListSessions, EndSession, EndEverySession), and handlers has no session.Store field
- [ ] ProfileRedirects calls users.Service.Resolve and reads no DB handle
- [ ] TestHandlersReachNoStore passes and TestTheLayeringScanFindsAViolation proves it can fail; the PR quotes the first failing on main, naming settings.go:83, :112, :136 and handle.go:111
- [ ] The three session routes serve the same status, headers and body as before; settings_test.go, TestShellRedirectsReleasedProfiles and TestReleasedHandleRedirects pass unchanged
- [ ] TestOneDoorIntoSessions and TestTheIssuersAreKnown pass with their caller lists unedited

## Implementation plan

Groomed 2026-10-02 for the Phase 4 run (docs/15), against main at ed82f15e.

### Settled in grooming

- **Placement and origin.** Filed by the Phase 1 cleanup. The orchestrator's verification note on TKT-01M29BKBMC (JSON API and service layer), criterion 1, "No handler in internal/web/api reads from a store package directly", found it partly met. docs/15 "Scope" places this ticket in Phase 4.
- **The violations on main at ed82f15e.** `go list -f '{{.Imports}}' ./internal/web/api` and a search for `.DB` and `h.sessions.` find these:
  - `settings.go`: `sessionList` calls `h.sessions.ListForUser`, `endSession` calls `h.sessions.RevokeOwn`, and `endEverySession` calls `h.sessions.RevokeUser`. These are `*session.Store` methods that run SQL.
  - `handle.go:111`: `ProfileRedirects` calls `member.Account(ctx, u.DB, v, handle, now)`, so a handler takes the database handle off the users service and passes it on.
  - `api.go` `build` reads `d.Users.DB` six times, to construct the groups, account, identity, auditlog and sitesettings services and the default session store. That is wiring, not a handler reading a store, and it stays.

  The only store packages the API imports are `internal/store/threadid` (id parsing) and `internal/store/db/views` (the cross-list enum), which the description names as allowed.
- **Where the session calls go: `identity.Service`.** It already owns sign-in state. It already holds a session store for `Login` (`Sessions` in `login.go`), and it already answers the neighbouring settings routes (`ListIdentities` and `Unlink`). Alternatives:
  - a new `internal/service/sessions` package lost. It would be a fourth service on the settings screen holding three thin methods, beside one that already holds the store.
  - the `account` service lost, because it owns status changes (suspend, deactivate), not sign-ins.
- **Where the profile redirect goes: `users.Service`.** A new method, `users.Service.Resolve(ctx, v, handle) error`, wraps `member.Account` at the service's own clock. `ProfileRedirects` already holds the users service. The existing `moved` helper reads the error exactly as it does now.
- **The test fences the package by its source, not by its imports alone.** The API must import `internal/auth/session` to read the request's session (`session.From`) and to clear the cookie (`session.ClearCookie`, `session.CookieName`), so a ban on the import is impossible. The test bans what reaches a store:
  - an import under `github.com/terva-sh/ketju/internal/store/`, other than `threadid` and `db/views`;
  - an import of `database/sql`;
  - a reference to `session.Store` anywhere except the `Deps` type and `build`, where `serve` hands the store in and `build` wires it into `identity.Service`;
  - a selector `.DB` anywhere except inside `build`.

  It parses the package's non-test files with `go/parser`, the way `internal/dto/layering_test.go` and `internal/auth/session/chokepoint_test.go` do. Alternatives:
  - `go/packages` with type information lost, because `golang.org/x/tools` is not a dependency, and the four rules need no types.
  - banning the `session` import lost, because the handlers need the request's session.
- **Invariants.**
  - The session chokepoint (docs/13) is about issuing sessions. This ticket moves only listing and revoking, so `TestOneDoorIntoSessions` and `TestTheIssuersAreKnown` keep passing with no edit to their caller lists.
  - No route, status code, body or header changes. The served bodies of the three session routes are the evidence; see the tests.

### Approach

1. `internal/service/identity`:
   - Add `ListForUser`, `RevokeOwn` and `RevokeUser` to the `Sessions` interface in `login.go`. `*session.Store` already satisfies them, so check that every fake that implements the interface in tests still compiles.
   - Add three methods to `Service`:
     - `ListSessions(ctx, v domain.Viewer, current string) (dto.SessionList, error)`, which moves the body of `sessionList`, `session.DeviceLabel` included;
     - `EndSession(ctx, v, id string) (ended bool, err error)`, which answers `not-found` itself when `ended` is false, as the handler does now;
     - `EndEverySession(ctx, v) error`.
   - The agent and member checks stay in the handler's `ownSession`. They read the request's credential and session, which is request parsing.
2. `internal/web/api/api.go`:
   - Pass `Sessions: sessions` into the `identity.Service` that `build` constructs.
   - Delete the `sessions` field from `handlers`.
   - Keep `Deps.Sessions` as it is, so `serve`'s wiring does not change.
3. `internal/web/api/settings.go`: the three handlers call `h.identity`. The cookie clearing stays in the handlers.
4. `internal/service/users`: add `Resolve`. `handle.go` `ProfileRedirects` calls `u.Resolve(r.Context(), v, handle)` and no longer reads `u.Now` or `u.DB`.
5. `internal/web/api/layering_test.go`: add `TestHandlersReachNoStore`, which applies the four rules above, and `TestTheLayeringScanFindsAViolation`. The second runs the same scanner over `testdata/layering/` fixtures, each breaking one rule, so the first test cannot pass by finding nothing. It also fails if the scan parsed no non-test files.
6. Commit the test first, alone. It must fail on main and name `settings.go:83`, `:112`, `:136` and `handle.go:111`. Quote that failure in the PR.

### Tests

- `TestHandlersReachNoStore` and `TestTheLayeringScanFindsAViolation`, as above.
- `TestSessionListIsUnchangedByTheMove`: GET `/api/v1/settings/sessions`, DELETE `/api/v1/settings/sessions/{id}` (one of the viewer's own, then one belonging to someone else, which is `not-found`) and DELETE `/api/v1/settings/sessions` serve the same status, headers and body as on main. Check them against the existing settings tests (`internal/web/api/settings_test.go`) and, if TKT-01M3YME9G0 (Golden-test what every API endpoint actually serves) has landed, against its served goldens for these routes.
- `TestShellRedirectsReleasedProfiles`, `TestReleasedHandleRedirects` and `TestAnonymizeReleasesWithoutRedirect` (`handle_test.go`) keep passing unchanged. They cover `/u/{old}` answering `302` and a stranger getting the shell.
- `identity` unit tests for the three methods, with a fake `Sessions`. A revoke of someone else's session id returns `ended == false`.
- `TestOneDoorIntoSessions` and `TestTheIssuersAreKnown` pass unchanged.

### Out of scope

- Moving service construction out of `build` and into `serve`. The `.DB` reads there are wiring, and the test allows exactly that function.
- Other handler packages (`internal/web/auth`, `internal/web/sse`). The criterion names `internal/web/api`.
- Ticking TKT-01M29BKBMC's criterion 1. That epic is the orchestrator's. This ticket's summary says the criterion is now met and cites the test.
