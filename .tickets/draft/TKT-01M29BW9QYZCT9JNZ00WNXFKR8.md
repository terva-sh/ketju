---
schema: 3
id: TKT-01M29BW9QYZCT9JNZ00WNXFKR8
title: Add structured logging with slog
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
dependencies: []
blocks_on: none
references:
  - ref: design:observability
    path: docs/02-architecture.md
claim: null
archive: null
created_at: 2026-09-11T23:12:10Z
updated_at: 2026-09-11T23:12:10Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:terva/mieli
  name: Mieli
extensions: {}
---

## Description

`slog` throughout, JSON in production and text in development, with the level configurable. Request logging in middleware carrying method, path, status, duration, and the viewer id when there is one.

The rule that matters more than the format: no post source, no session id, no token, and no secret ever reaches a log line. A forum's logs end up in someone's aggregator, and a body that was soft-deleted in the log is not deleted if it is also in a log line.

## Acceptance criteria

- [ ] Production output is JSON and development output is text, selected by config
- [ ] Request logs carry method, path, status, duration, and viewer id
- [ ] A test asserts that session ids and post source never appear in log output
