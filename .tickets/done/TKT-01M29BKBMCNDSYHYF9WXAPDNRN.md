---
schema: 3
id: TKT-01M29BKBMCNDSYHYF9WXAPDNRN
title: JSON API and service layer
type: epic
status: done
status_reason: null
priority: urgent
due_on: null
labels:
  - area:api
  - effort:XL
assignees: []
milestone: phase-1
parent: null
origin: null
dependencies:
  - TKT-01M29BKBKV6HH8W0DN6QXQNZ0R
  - TKT-01M29BKBM7004ZPAA9T81HC939
blocks_on: children
references:
  - ref: design:api
    path: docs/07-api-and-clients.md
  - ref: design:architecture-flows
    path: docs/02-architecture.md
claim: null
archive: null
created_at: 2026-09-11T23:07:17Z
updated_at: 2026-10-03T07:38:33Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase4-run-orchestrator
  name: ""
extensions: {}
---

## Description

`internal/service` and `internal/web/api`: the use-case layer that is the only package touching both stores, and the thin JSON handlers over it.

Handlers contain no authorization and no validation of their own. That discipline is the concrete mechanism that makes the non-JS client cheap later: the HTML handlers will call the same services and render the same DTOs. Golden JSON tests per endpoint are the contract, and they serve both clients.

Errors are RFC 9457 problem details with a stable type code. Thread posts are page-numbered because the UI is, and log-ordered or time-ordered lists are cursor-paged.

The thread page response is the load-bearing endpoint. Everything above the viewer block comes from a response cache keyed on thread, page, lens, log_length, renderer version, and viewer class. The viewer block is the only per-user part and it stays cheap.

## Acceptance criteria

- [x] No handler in internal/web/api reads from a store package directly
- [x] Golden JSON tests cover every endpoint in docs/07 and fail on any shape change
- [x] Every error response is application/problem+json with a stable type code
- [x] The thread page returns a correct ETag of thread id, log length, and renderer version, and honors If-None-Match
- [ ] Every mutation in the API is expressible as a form post, so the future HTML client needs no new service call

## Notes

**agent:claude-code/phase4-run-orchestrator** at 2026-10-02T15:41:59Z

Verification on 2026-10-02, against origin/main at adeba570, done the same way as the other Phase 1 epics.
1. **No handler reads a store directly: partly. Left unticked.** Three settings handlers call `session.Store`, which runs SQL (`settings.go:83,112,136`), and no test fences the package. Follow-up: TKT-01M3YME9DM (Keep API handlers off the stores, and test the layering).
2. **Golden JSON for every endpoint: partly. Left unticked.** Every DTO type has a fixture golden, but served-response goldens exist for only a few endpoints, and nothing maps the route table to goldens. Follow-up: TKT-01M3YME9G0 (Golden-test what every API endpoint actually serves).
3. **problem+json with stable codes: holds.** Every error path goes through `problem.write`. Tests: `TestCatalogueGolden`, `TestTheDocumentedCatalogueMatchesTheCode`, `TestAProblemBodyIsRFC9457Shaped`. Caveat: a wrong verb answers `not-found`, not `method-not-allowed`. Follow-up: TKT-01M3YME9KG.
4. **ETag and If-None-Match: holds.** The tag also carries a body digest, as docs/07:701 records. Tests: `TestETagAndNotModified`, `TestETagCoversAuthorsAndForum`, `TestETagCoversPageSize`.
5. **Every mutation as a form post: partly. Left unticked.** `TestEveryViewerStateRouteHasAFormPost` covers the 13 viewer-state routes. Posting, editing, settings and the admin writes have no form route. That is the non-JS client's work, epic TKT-01M29BN36W (Phase 5).

**agent:claude-code/phase4-run-orchestrator** at 2026-10-02T22:47:32Z

Criterion 1 met after close, 2026-10-02: TKT-01M3YME9DM (Keep API handlers off the stores, and test the layering) merged via PR #367 (cac3ed9d). TestHandlersReachNoStore (internal/web/api/layering_test.go) fails CI when a handler imports a store package, database/sql, or calls through a session store. Criteria 2 and 5 stay unticked: 2 waits on TKT-01M3YME9G0, 5 on the Phase 5 HTML client.

**agent:claude-code/phase4-run-orchestrator** at 2026-10-03T07:38:33Z

Criterion 2 met after close, 2026-10-03: TKT-01M3YME9G0 (Golden-test what every API endpoint actually serves) merged via PRs #390, #392 and #393 (bd5fdc20). TestEveryRouteHasAServedGolden walks api.Routes and all 105 routes have a served golden under internal/web/api/testdata/golden/served/; renaming a DTO json tag fails TestServedGoldens. Criterion 5 stays unticked for the Phase 5 HTML client.

## Summary

Phase 1's JSON API epic. All 18 children are done. On 2026-10-02 the criteria were checked against the code: 3 and 4 hold and are ticked. Criterion 1 is partial, follow-up TKT-01M3YME9DM in Phase 4. Criterion 2 is partial, follow-up TKT-01M3YME9G0 in Phase 4. Criterion 5 is partial and belongs to the Phase 5 non-JS client.
