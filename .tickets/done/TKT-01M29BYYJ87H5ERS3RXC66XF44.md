---
schema: 3
id: TKT-01M29BYYJ87H5ERS3RXC66XF44
title: Add the projection tables and projection_state
type: task
status: done
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
updated_at: 2026-09-24T08:23:37Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase1-run-415af6aa
  name: ""
extensions: {}
---

## Description

`threads`, `thread_tags`, `post_meta`, `mentions`, and `projection_state`: every row here is derived from a thread log and every one is disposable.

`post_meta` exists only for questions that cross thread boundaries: a user's post history, reverse reply lookups when the thread is not open, and moderation lists. Within-thread questions are answered by the index file, and adding a column here that duplicates an index field is the mistake to avoid.

`threads.log_length_projected` and `projection_state.log_length` are what startup catch-up compares against the real log, so they are the mechanism rather than bookkeeping.

## Acceptance criteria

- [x] Every table in this migration is marked as a projection in a comment naming `ketju reindex` as its recovery path
- [x] post_meta rows stay tiny, with the indexes from docs/04 on (author_id, created_at) and (thread_id, reply_to)
- [ ] Dropping every table in this migration and rebuilding from logs produces identical rows, tested end to end

## Implementation plan

Revised at claim (2026-09-24), after reading the code as it stands.

### Approach
1. Migration `0004_projections.sql` adds `threads`, `thread_tags`, `post_meta`, `mentions` and `projection_state`, with docs/04's columns.
   - Each CREATE carries `-- PROJECTION: derived from thread logs; rebuild with ketju reindex` inside its parentheses, because sqlite_schema keeps the statement text from CREATE onward, and a comment above the CREATE would be lost.
   - There are no foreign keys, as settled in grooming.
   - CHECKs cover the 0/1 flags, ordinals, lengths and pin shape.
   - `post_meta`, `thread_tags` and `mentions` are WITHOUT ROWID.
   - post_meta has exactly the two documented indexes.
   - Also added: `threads_forum_activity (forum_id, last_post_at)` for the forum thread list.
2. `db.ProjectionTables`, and `db.ClearProjections(ctx, q)`, which runs five sqlc DELETEs inside a Write.
   - **Change from the pre-groom plan:** reindex empties the tables rather than dropping them. The migration runner never re-runs a recorded migration, so recreating dropped tables would need a second copy of the DDL.
3. `queries/projection.sql` adds GetThread, ThreadTags and GetProjectionState, the reads CA9AX needs, plus the Clear statements. The upserts are left to TKT-01M29C2WW5, which knows what it writes.
4. `internal/store/db/dbtest.DumpProjections(t, db)` is canonical text. It covers the tables in list order, rows in primary-key order, and columns read from the schema. It drops `updated_at` and spells types distinctly.
5. docs/04 "Projection tables" records the no-FK rule and empty-not-drop. docs/03 now names `projection_state` instead of `projections`.

## Notes

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T08:23:36Z

### AC3 is left unticked

It says: "Dropping every table in this migration and rebuilding from logs produces identical rows, tested end to end". Rebuilding from logs needs the projection writer (TKT-01M29C2WW5) and `ketju reindex` (TKT-01M29C5Y25). Neither exists yet. C5Y25's first criterion is the same check, so the end-to-end test belongs there.

What landed here is the half the criterion depends on: `dbtest.DumpProjections`, with TestDumpIsCanonical. Two databases holding the same rows, inserted in a different order and at a different wall-clock time, dump identically, and a changed row dumps differently.

### Mutation check

10 mutants, all killed:
- the marker dropped from one table;
- ProjectionTables missing a table;
- ClearProjections skipping a table;
- wrong columns on a post_meta index;
- an extra post_meta index;
- an extra post_meta column;
- a foreign key added;
- updated_at dumped;
- dump order reversed;
- strings formatted unquoted.

## Summary

Landed migration 0004 with the five projection tables (threads, thread_tags, post_meta, mentions, projection_state). They carry no foreign keys, and each is marked as a projection inside its CREATE statement.

- **Reindex support.** `db.ProjectionTables` lists the tables and `db.ClearProjections` empties them. Reindex empties rather than drops, because the DDL lives only in the migration.
- **Reads for the thread page.** GetThread, ThreadTags and GetProjectionState.
- **Shared test helper.** `dbtest.DumpProjections` is the canonical comparison for the writer and reindex tests.
- **Docs.** docs/04 records the no-FK and empty-not-drop rules. docs/03 now names projection_state.
- **Criteria.** AC1 and AC2 are ticked. AC3 (rebuild from logs gives identical rows) needs the writer and reindex, and is TKT-01M29C5Y25's first criterion.
