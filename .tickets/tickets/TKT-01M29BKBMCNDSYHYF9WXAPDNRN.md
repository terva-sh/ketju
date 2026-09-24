---
schema: 3
id: TKT-01M29BKBMCNDSYHYF9WXAPDNRN
title: JSON API and service layer
type: epic
status: ready
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
updated_at: 2026-09-24T05:21:19Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude/phase1-groom
  name: ""
extensions: {}
---

## Description

`internal/service` and `internal/web/api`: the use-case layer that is the only package touching both stores, and the thin JSON handlers over it.

Handlers contain no authorization and no validation of their own. That discipline is the concrete mechanism that makes the non-JS client cheap later: the HTML handlers will call the same services and render the same DTOs. Golden JSON tests per endpoint are the contract, and they serve both clients.

Errors are RFC 9457 problem details with a stable type code. Thread posts are page-numbered because the UI is, and log-ordered or time-ordered lists are cursor-paged.

The thread page response is the load-bearing endpoint. Everything above the viewer block comes from a response cache keyed on thread, page, lens, log_length, renderer version, and viewer class. The viewer block is the only per-user part and it stays cheap.

## Acceptance criteria

- [ ] No handler in internal/web/api reads from a store package directly
- [ ] Golden JSON tests cover every endpoint in docs/07 and fail on any shape change
- [ ] Every error response is application/problem+json with a stable type code
- [ ] The thread page returns a correct ETag of thread id, log length, and renderer version, and honors If-None-Match
- [ ] Every mutation in the API is expressible as a form post, so the future HTML client needs no new service call
