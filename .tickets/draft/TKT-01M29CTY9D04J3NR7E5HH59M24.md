---
schema: 3
id: TKT-01M29CTY9D04J3NR7E5HH59M24
title: Ship ketju reindex --search
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:search
  - area:ops
  - effort:S
assignees: []
milestone: phase-3
parent: TKT-01M29BN36D34YVKVRPNQD3PHDV
origin: null
dependencies:
  - TKT-01M29CRJ3BEMZREJQRGAK3AVGK
blocks_on: none
references:
  - ref: design:storage-recovery
    path: docs/03-thread-storage.md
claim: null
archive: null
created_at: 2026-09-11T23:28:54Z
updated_at: 2026-10-01T09:47:41Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase3-groom-search
  name: ""
extensions: {}
---

## Description

Rebuild the full-text index from the thread logs.

The search index is the largest object in the database and the most disposable. Being able to drop and rebuild it is what lets an operator recover from a corrupt index, change the tokenizer, or shrink a database without touching content.

## Acceptance criteria

- [ ] Rebuilding with reindex --search, from the index as it is, from an emptied index, or from one whose FTS segments are damaged, gives the same rows and the same results as the incremental path, and FTS5 integrity-check passes (TestReindexSearchEqualsIncremental)
- [ ] Progress is reported on stderr and in the report, and a run cancelled or failing before its commit leaves the earlier index in service unchanged (TestReindexSearchProgress, TestReindexSearchInterrupted)
- [ ] The command refuses beside serve and works with serve stopped, from the data directory alone (TestReindexSearchOffline)
- [ ] --search, --projections and both select as docs/03 decides, and a projections-only run keeps the search rows (TestReindexFlags, TestReindexProjectionsKeepsSearch)
- [ ] The README recovery procedure rebuilds the search index from the logs, and TestREADMERecoveryProcedure runs it
- [ ] A thread damaged before its last frame, even after some of its posts were indexed, is reported with exit 1 and left with no search rows, while the other threads are indexed (TestReindexSearchDamagedThread)

## Implementation plan

Groomed 2026-10-01 against main at a599b48 (TKT-01M3V3BVPY, Phase 3 grooming, lane `search`). `ketju reindex` exists for projections (TKT-01M29C5Y25, `cmd/ketju/reindex.go`). Today, `--search` is a usage error naming TKT-01M29CRJ3B, and `--projections=false` alone is refused.

### Settled in grooming
- **The command's contract** (docs/03 "Durability and recovery"). The form is `ketju reindex [--search] [--projections]`, with `--projections` the default. It takes the data-directory lock, so it refuses beside `serve`, and `serve` cannot start under it. It only reads the logs. A torn final frame is worked around and left for `serve`. A thread damaged before its last frame gets no rows, is reported, and makes the exit status 1. A failure that is not the log's stops the run.
- **The search index is disposable and rebuildable** (docs/04 "search (FTS)"). Because `reindex --search` rebuilds it, it is not docs/14 "stop rule" class 2.
- **Run decision of 2026-10-01, pending maintainer review**, in docs/03 "Durability and recovery":
  - `--search` rebuilds only the search index, and both flags together do both, projections first;
  - the rebuild is one transaction, so an interrupted run leaves the old index in service, and the WAL grows by about the index's size while it runs;
  - a database with no search rows needs no command, because `serve` indexes in the background.

  The alternatives, a transaction per thread and a second set of tables to swap in, are listed there.
- **From TKT-01M29CRJ3B's plan:**
  - `db.ClearSearch` (FTS5 `delete-all`, safe on a damaged index) and `db.RebuildSearchIndex` (FTS5 `rebuild`);
  - `search_meta.version`;
  - the indexer's per-thread path, which a full rebuild shares, so incremental and full cannot drift apart.

### Approach
1. **Flags** (`reindexMain`). Use `flag.Visit` to learn whether `--projections` was given. The flags select as follows:

   | Flags given | Rebuilds |
   |---|---|
   | none | projections |
   | `--search` | search |
   | `--search --projections` | both, projections first |
   | `--search --projections=false` | search |
   | `--projections=false` alone | usage error, as now |

   Delete the `--search` usage error. Rewrite `reindexUsage`:
   - drop "arrives in Phase 2";
   - say what `--search` rebuilds, and that the run is one transaction that leaves the old index on any failure;
   - say that free disk of about the index's size is needed for the WAL.
2. **The rebuild.** `search.Rebuild(ctx, d *db.DB, root string, progress func(search.RebuildProgress)) (search.RebuildReport, error)` in `internal/events/search`, sharing `IndexThread`'s range walk from the first frame. It walks `threads/` with the walker the projection's reindex uses, then runs one `d.Write`:
   - `db.ClearSearch`;
   - for each thread: open the log read-only, get its index in memory with `threadlog.Open` (never writing the `.idx`), index every visible post and the title row, and set `search_state`. A torn tail is indexed to the last whole frame and reported as torn. **Each thread runs inside its own `SAVEPOINT`** (Terva review 1711). A log fault found partway through a thread rolls back to the savepoint, so the thread keeps none of the rows it had already put, and it is reported as failed. The run goes on, as the projection reindex does. A database error is not the log's, so it rolls back the whole transaction and stops the run;
   - `search_meta.version = search.Version`;
   - `db.RebuildSearchIndex`. The documents are written plainly and FTS5 builds its index once, which is faster than row by row;
   - `optimize`, then commit.

   Cancelling the context (`signalContext`, Ctrl-C) or any database error rolls back, and the command says the search index was not changed.
3. **Reporting.**
   - Progress lines on stderr, at most every 2 s, as the projection reindex prints them, prefixed `reindex --search:`.
   - The text report adds `search: indexed N of M thread(s), P post(s), in T` and a `FAILED` line per thread.
   - `--json` adds a `search` object (`threads`, `indexed`, `posts`, `torn`, `failed`, `elapsed_ms`), or `null` when not run. When only search runs, the projection fields keep their zero values, as a run with nothing to project gives them now.
   - Exit codes are unchanged: 1 when a thread failed, 4 on a database or lock failure.
4. **A missing table.** When the search tables are absent although migrations are recorded, someone dropped them by hand. The run exits 4, naming the tables, and suggests restoring the database from a backup. Recreating them would need their DDL outside the migrations, which docs/03 refuses.
5. **README.** In "Recovering a lost or damaged database", the command becomes `ketju reindex --root . --projections --search`, so the restored index also comes from the logs, never from the backup. Add one sentence: without `--search`, `serve` rebuilds the index in the background and search says it is incomplete meanwhile. `TestREADMERecoveryProcedure` runs the block, so it covers the change.

### Tests
- `cmd/ketju`:
  - `TestReindexSearchEqualsIncremental`. A data directory is built through `threads.Service` with posts, edits, an author delete, a moderator hide, an auto-hide, a restore, a split, a rename, and a `ketju redact` of one post and one title, and the indexer indexes it as it goes. Its `dbtest.DumpSearch` and the `views.Search` results for a fixed word list are recorded. Then each of three cases must give the same dump and results:
    - running `reindex --search`;
    - running `db.ClearSearch` and then `reindex --search`;
    - damaging `search_data` rows and then `reindex --search`.

    `integrity-check` passes in every case.
  - `TestReindexSearchInterrupted`. A context cancelled midway, and a forced database failure at the commit (a seam like `redactOpts.fail`), each leave the earlier index's rows and results unchanged and exit non-zero.
  - `TestReindexSearchOffline`: refused while `serve` holds the lock, and runs with `serve` stopped, from the data directory alone.
  - `TestReindexFlags`: the five combinations in the table.
  - `TestReindexSearchDamagedThread`: a thread damaged before its last frame, after at least one of its posts was indexed, is reported with exit 1. It has no `search_docs` row and no `search_state` row, and the other threads are indexed.
  - `TestReindexSearchProgress`: stderr lines and the JSON report.
  - `TestReindexProjectionsKeepsSearch`: a projections-only run leaves the search rows.
  - `TestREADMERecoveryProcedure` passes with the new command.

### Out of scope
- The index and the incremental indexer: TKT-01M29CRJ3B (Build the FTS5 search index and indexer).
- Shrinking the database file after a rebuild. `VACUUM` belongs to the backup documentation in Phase 4 (docs/09: "Metrics endpoint, backup documentation"). The rebuild only frees pages for reuse.
- Recreating search tables that were dropped by hand (step 4).

## Notes

**agent:claude-code/phase3-groom-search** at 2026-10-01T07:58:30Z

run decision: pending maintainer review. Recorded in docs/03 "Durability and recovery" (2026-10-01): --search rebuilds only the search index, both flags do both (projections first), and the rebuild is one transaction so an interrupted run leaves the old index in service; serve builds a missing index in the background, so no command is needed after an upgrade. The same line corrects docs/03's stale 'arrives with full-text search in Phase 2 (TKT-01M29C5Y25)'. Criterion 1 used to say 'Dropping the FTS tables'; it now says emptying the index or finding its segments damaged, because the tables' DDL lives only in the migrations (as ClearProjections already holds) and FTS5 delete-all plus rebuild covers a corrupt index. A table dropped by hand is reported (exit 4) rather than recreated.

**agent:claude-code/phase3-groom-search** at 2026-10-01T08:43:37Z

Terva review 1711 (PR #270, head e5998ef, base a599b48), fixed. (high) 'Discard partial rows when a thread's log is damaged': each thread of the rebuild runs in its own SAVEPOINT and a log fault rolls back to it, so the thread keeps no rows; TestReindexSearchDamagedThread now faults after a post was indexed (criterion 6). The same fix is applied to TKT-01M29CRJ3B's batched Deliver (TestAFailedThreadKeepsItsRows).

**agent:claude-code/phase3-groom-search** at 2026-10-01T09:47:41Z

Requirement carried from review 1731 (2026-10-01; PR #275, head 64a8983, base 6dfd01d; medium, 'Keep version preparation inside the reindex transaction'). This binds the implementing lane and overrides any plan text it contradicts. reindex --search must not call Indexer.Prepare as a separate committed step. The version check, ClearSearch and the search_meta.version write all happen inside the rebuild's one transaction, so a run cancelled or failing before commit leaves the earlier index, and its version, exactly as they were, whatever version they carry. TestReindexSearchInterrupted adds a case where the stored version differs from search.Version and the run is cancelled after the clear: the old rows and old version remain. The TKT-01M29CRJ3B plan's sentence that 'ketju reindex --search and ketju redact call it [Prepare] too' does not apply to reindex. serve's Prepare then clears that old-version index at its next start, before listening. Not fixed in the plan because PR #275 had used its two review rounds.
