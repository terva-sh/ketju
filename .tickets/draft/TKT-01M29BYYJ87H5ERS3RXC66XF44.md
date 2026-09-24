---
schema: 3
id: TKT-01M29BYYJ87H5ERS3RXC66XF44
title: Add the projection tables and projection_state
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
  - TKT-01M29BW9Q17KEBMHJ91G6QHW36
blocks_on: none
references:
  - ref: design:data-model-projections
    path: docs/04-data-model.md
claim: null
archive: null
created_at: 2026-09-11T23:13:37Z
updated_at: 2026-09-24T05:21:02Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude/phase1-groom
  name: ""
extensions: {}
---

## Description

`threads`, `thread_tags`, `post_meta`, `mentions`, and `projection_state`: every row here is derived from a thread log and every one is disposable.

`post_meta` exists only for questions that cross thread boundaries: a user's post history, reverse reply lookups when the thread is not open, and moderation lists. Within-thread questions are answered by the index file, and adding a column here that duplicates an index field is the mistake to avoid.

`threads.log_length_projected` and `projection_state.log_length` are what startup catch-up compares against the real log, so they are the mechanism rather than bookkeeping.

## Acceptance criteria

- [ ] Every table in this migration is marked as a projection in a comment naming `ketju reindex` as its recovery path
- [ ] post_meta rows stay tiny, with the indexes from docs/04 on (author_id, created_at) and (thread_id, reply_to)
- [ ] Dropping every table in this migration and rebuilding from logs produces identical rows, tested end to end

## Implementation plan

Pre-groom plan (2026-09-24). Revise at claim, after reading the code as it stands then.

### Approach
1. Add migration `NNNN_projections.sql` with the tables `threads`, `thread_tags`, `post_meta`, `mentions`, and `projection_state`, using the columns in docs/04 "Projection tables". Each CREATE carries the comment `-- PROJECTION: derived from thread logs; rebuild with ketju reindex`.
2. `post_meta` uses `PRIMARY KEY (thread_id, ordinal) WITHOUT ROWID`, plus `INDEX (author_id, created_at)` and `INDEX (thread_id, reply_to)`. No other columns and no other indexes. A schema comment says that within-thread questions belong to the `.idx` file. `mentions` has `PRIMARY KEY (thread_id, ordinal, mentioned_user_id)`. `thread_tags` has `PRIMARY KEY (thread_id, tag)`.
3. `projection_state(thread_id PK, log_length, updated_at)`. `threads.log_length_projected` duplicates it. Keep both, as docs/04 does, and state in a comment that the writer updates both in one transaction, and that catch-up reads `projection_state`.
4. Decision to record: the projection tables carry no FOREIGN KEY to authoritative tables. `author_id` and `forum_id` are plain INTEGERs. This is what lets reindex rebuild content into a database whose users came from an older backup, or from no backup at all (C5Y25). docs/04 is silent on the point. Add a line to "Projection tables".
5. Export a Go list `db.ProjectionTables`. Reindex drops or truncates exactly this list, and the test asserts that the list equals the set of tables whose schema carries the PROJECTION comment.
6. Provide a test helper `dbtest.DumpProjections(t, db) string`. It writes every projection row, ordered by primary key, as canonical text, excluding `updated_at` wall-clock columns. It is shared with C2WW5 and C5Y25 for the "identical rows" checks.

### Files and packages
New: the migration, `queries/projection.sql` (the upserts C2WW5 calls, and `projection_state` reads), the regenerated `dbq/`, `internal/store/db/projection_tables.go` (the list), `internal/store/db/dbtest/dump.go`, `projections_schema_test.go`. Reuses BW9Q1's runner. Touched: docs/04 "Projection tables" (the no-FK line), and `cmd/ketju/fsck.go`, whose `DriftBytes` comment names this ticket. Leave fsck alone; the comment is for whoever wires drift later.

### Tests
- AC1: `TestProjectionTablesAreMarked` scans `sqlite_schema.sql` for the marker and compares that set with `ProjectionTables`, so a table added without the marker fails the test.
- AC2: `TestPostMetaIndexes` checks via `pragma_index_list` and `pragma_index_info` that exactly the two documented secondary indexes exist. A column-count assertion guards against additions.
- AC3, end to end: this ticket has no writer yet. It lands a test that runs migrate, inserts fixture rows, dumps, drops every table in `ProjectionTables`, re-runs migrate (which recreates them), and re-inserts. The real "rebuild from logs" equality is C2WW5's and C5Y25's, via `DumpProjections`. Leave AC3 unticked, with a note naming C5Y25, unless the writer has landed by then.

### Design
docs/04-data-model.md "Projection tables (rebuildable)" and "What deliberately is not in the database". docs/03-thread-storage.md "Write path" (last paragraph) and "Durability and recovery". Note that docs/03 calls the table `projections`. It is `projection_state`, so fix the name in docs/03 in the same commit.

### Interactions
Defines the tables that C2WW5 writes, that C5Y1S and C5Y25 compare and rebuild, and that TKT-01M29CA9AG (thread list) reads. TKT-01M29CRJ3B adds the `search` FTS table later, as a separate projection. It must add itself to `ProjectionTables`.

### Settled in grooming (2026-09-24)

- Projection tables have no foreign keys to users, so reindex works from the logs alone.
- Use docs/04's table names. Fix docs/03's `projections` wording to match.
