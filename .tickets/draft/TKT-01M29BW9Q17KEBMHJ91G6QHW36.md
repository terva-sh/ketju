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
updated_at: 2026-09-16T18:40:07Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase1
  name: ""
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

## Comments

**agent:claude-code/phase1** at 2026-09-16T18:40:07Z

The access layer needs a decision this ticket does not yet record: how Go
reaches the tables. Nothing in docs/04, docs/09, docs/01 or any area:db ticket
names a tool, so "SQL stays driver-agnostic" is currently a rule with no
mechanism behind it.

Raised by the repository owner: use sqlc, generating from .sql files.

### Why it fits the constraint this ticket already states

sqlc's SQLite output targets database/sql. Both ncruces and modernc ship a
database/sql driver, so the "modernc is a drop-in" mitigation in docs/09 stops
depending on a reviewer noticing a driver-specific call and becomes something
the generator cannot emit. That is the difference between a documented rule and
an enforced one.

sqlc does not run migrations, which suits this ticket rather than conflicting
with it: the ordered, forward-only, recorded runner stays ours, and sqlc reads
the migration directory as its schema source.

### What was checked rather than assumed

- FTS5 parses. sqlc-dev/sqlc#1797 is closed-completed, so a
  `CREATE VIRTUAL TABLE ... USING fts5` statement in the schema path is not the
  blocker older write-ups describe. Worth confirming against the real search
  schema in docs/04 before committing.
- The known SQLite timestamp trap does not apply here. sqlc maps DATETIME and
  TIMESTAMP to time.Time, which fails against drivers storing epoch integers
  (sqlc-dev/sqlc#3736). docs/04 already specifies unix-millisecond INTEGER
  columns, so the trap is closed by a decision already made.

### The honest costs

- Dynamic queries are sqlc's weak point, and the lens-filtered thread views
  with cursors are exactly that shape. `sqlc.slice()` covers IN-lists; some
  view queries may still want hand-written SQL beside the generated set. That
  is a mixed layer, and mixing should be a decision rather than a discovery.
- Generated code must be committed and verified in CI with a diff step, or it
  drifts from the schema silently. Without that step the type safety is a
  claim about a file nobody regenerated.
- It is a build-time dependency on a code generator for every schema ticket
  that depends on this one, which is eight of them.

No decision recorded here. This is the shape of the choice and what was
verified, so that whoever decides is not starting from a blank page.
