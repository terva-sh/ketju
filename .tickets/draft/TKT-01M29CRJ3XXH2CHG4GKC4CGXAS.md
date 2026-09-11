---
schema: 3
id: TKT-01M29CRJ3XXH2CHG4GKC4CGXAS
title: Build the SSE endpoint and fan-out
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:realtime
  - effort:M
assignees: []
milestone: phase-3
parent: TKT-01M29BN36D34YVKVRPNQD3PHDV
origin: null
dependencies:
  - TKT-01M29C0WF9GFGVG7QSAYRAKAA4
  - TKT-01M29C5Y3SAD25GYYEMHYPDF24
blocks_on: none
references:
  - ref: design:live-updates
    path: docs/02-architecture.md
claim: null
archive: null
created_at: 2026-09-11T23:27:36Z
updated_at: 2026-09-11T23:27:36Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:terva/mieli
  name: Mieli
extensions: {}
---

## Description

`GET /events?threads=&forums=` as a long-lived stream carrying `{thread_id, log_length}` invalidation hints and nothing else.

Payloads carry no content and no per-viewer logic, which is what keeps SSE stateless with respect to permissions: the client's follow-up fetch is where authorization happens. A subscriber to a thread they cannot read learns only that it changed, and that is acceptable where a forum listing already leaks as much.

The connection count is the operational risk. Every open tab is a held connection, and the limits need to be explicit rather than discovered when a community grows.

## Acceptance criteria

- [ ] Messages carry only thread id and log length, verified by a test asserting the payload shape
- [ ] Per-connection and total connection limits are configurable and enforced
- [ ] A dropped connection resumes without missing an update, because the client reconciles by log length
- [ ] Shutdown closes streams cleanly rather than leaving clients hanging
