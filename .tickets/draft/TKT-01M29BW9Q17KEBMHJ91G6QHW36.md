---
schema: 3
id: TKT-01M29BW9Q17KEBMHJ91G6QHW36
title: Build the database access layer and migration runner
type: task
status: draft
status_reason: null
priority: urgent
due_on: null
labels:
  - area:db
  - effort:M
assignees: []
milestone: phase-1
parent: TKT-01M29BKBKV6HH8W0DN6QXQNZ0R
origin: null
dependencies:
  - TKT-01M29BSPE8J56YC8CME7N8NC1W
blocks_on: none
references:
  - ref: design:data-model-engine
    path: docs/04-data-model.md
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

`internal/store/db`: opening the database in WAL mode, the connection pool capped at 4 to 8, `busy_timeout` set explicitly rather than left to chance, and writes wrapped in `BEGIN IMMEDIATE` so a write transaction never upgrades mid-flight into a busy error.

Plus the migration runner that every schema ticket depends on: ordered, forward-only, recorded in the database, and refusing to start against a database newer than the binary.

SQL stays driver-agnostic. That constraint is the mitigation for the ncruces pre-1.0 churn risk in docs/09, and it only holds if it is enforced from the first query.

## Acceptance criteria

- [ ] The database opens in WAL mode with an explicit busy_timeout, verified by a test that reads back the pragmas
- [ ] Concurrent writers hit BEGIN IMMEDIATE and serialize rather than failing with SQLITE_BUSY
- [ ] Migrations run in order, are recorded, and are idempotent on a second start
- [ ] A binary older than the recorded schema version refuses to start with a clear message
