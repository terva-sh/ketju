---
schema: 3
id: TKT-01M29CCBZ8DV7086HBMN12F1HP
title: Add the healthz endpoint
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:ops
  - effort:S
assignees: []
milestone: phase-1
parent: TKT-01M29BN36Q9D32WQGYPFS336J0
origin: null
dependencies:
  - TKT-01M29C5Y3SAD25GYYEMHYPDF24
blocks_on: none
references:
  - ref: design:observability
    path: docs/02-architecture.md
claim: null
archive: null
created_at: 2026-09-11T23:20:57Z
updated_at: 2026-09-24T05:06:25Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude/phase1-groom
  name: ""
extensions: {}
---

## Description

`/healthz` checks that the database opens and the data directory is writable, which are the two failures that make the process useless while it is still listening.

A health check that returns 200 whenever the HTTP server is up tells a supervisor nothing it did not already know from the TCP connection.

## Acceptance criteria

- [ ] Returns non-200 when the database cannot be queried or the data directory is not writable
- [ ] The endpoint requires no authentication and leaks no version or configuration detail
- [ ] Each request costs at most one SELECT 1 plus a data-directory writability probe cached for no more than 10 s, and a benchmark shows p99 under 5 ms

## Notes

**agent:claude/phase1-groom** at 2026-09-24T05:06:25Z

Acceptance criteria rewritten during Phase 1 grooming (2026-09-24), agreed with the user as part of preparing the autonomous run. Rewritten criteria move to the end of the list, so their numbers changed. 'Cheap enough to poll every few seconds' became concrete: one SELECT 1, a data-directory writability probe cached for at most 10 s (the first criterion still requires detecting an unwritable directory), and p99 under 5 ms.
