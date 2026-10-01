---
schema: 3
id: TKT-01M29CTY9D04J3NR7E5HH59M24
title: Ship ketju reindex --search
type: task
status: done
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
  - ref: pr:307
    path: null
claim: null
archive: null
created_at: 2026-09-11T23:28:54Z
updated_at: 2026-10-01T13:01:52Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase3-search-cli
  name: ""
extensions: {}
---

## Description

Rebuild the full-text index from the thread logs.

The search index is the largest object in the database and the most disposable. Being able to drop and rebuild it is what lets an operator recover from a corrupt index, change the tokenizer, or shrink a database without touching content.

## Acceptance criteria

- [x] Rebuilding with reindex --search, from the index as it is, from an emptied index, or from one whose FTS segments are damaged, gives the same rows and the same results as the incremental path, and FTS5 integrity-check passes (TestReindexSearchEqualsIncremental)
- [x] Progress is reported on stderr and in the report, and a run cancelled or failing before its commit leaves the earlier index in service unchanged (TestReindexSearchProgress, TestReindexSearchInterrupted)
- [x] The command refuses beside serve and works with serve stopped, from the data directory alone (TestReindexSearchOffline)
- [x] --search, --projections and both select as docs/03 decides, and a projections-only run keeps the search rows (TestReindexFlags, TestReindexProjectionsKeepsSearch)
- [x] The README recovery procedure rebuilds the search index from the logs, and TestREADMERecoveryProcedure runs it
- [x] A thread damaged before its last frame, even after some of its posts were indexed, is reported with exit 1 and left with no search rows, while the other threads are indexed (TestReindexSearchDamagedThread)

## Implementation plan

Groomed 2026-10-01 against main at a599b48 (TKT-01M3V3BVPY, Phase 3 grooming, lane `search`). Revised at claim, 2026-10-01, against main at d80c233, after TKT-01M29CRJ3B (PR #301) landed the index. Today `--search` is a usage error naming TKT-01M29CRJ3B, and `--projections=false` alone is refused.

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
- **Requirement carried from review 1731** (note 3, binding): reindex --search never calls `Indexer.Prepare`. The version read, `ClearSearch` and the `search_meta.version` write are all inside the rebuild's one transaction, so a cancelled or failing run leaves the earlier index and its version exactly as they were, whatever version they carry.

### Approach
1. **Flags** (`reindexMain`). `flag.Visit` tells whether `--projections` was given:

   | Flags given | Rebuilds |
   |---|---|
   | none | projections |
   | `--search` | search |
   | `--search --projections` | both, projections first |
   | `--search --projections=false` | search |
   | `--projections=false` alone | usage error, as now |

   The `--search` usage error goes. `reindexUsage` drops "arrives in Phase 2", says what `--search` rebuilds, that it is one transaction that leaves the old index on any failure, and that it needs free disk of about the index's size for the WAL. The projection part moves into `reindexProjections`, unchanged.
2. **The rebuild** is `(*search.Indexer).Rebuild(ctx, progress)` in `internal/events/search/rebuild.go`, a method rather than the package function the groomed plan named, so it shares the indexer's seams (`open`, `walk`). It walks `threads/` with `threadid.WalkLogs` first, so a failed walk changes nothing, then runs one `d.Write`:
   - read `search_meta.version` (reported as the old version), `db.ClearSearch`;
   - for each thread, in id order, `inSavepoint` with a `target{rebuild: true}`: the indexer's own `indexThread`, from the first frame. In rebuild mode the source is opened from the log alone, with no `.idx` read and the cache not peeked, so the index is built by replaying every frame and a stale or damaged sidecar cannot reach it (this also keeps the rebuild clear of TKT-01M3VHSYFR). Rows are written with new `db.PutSearchRow` / `db.DropSearchRow`, which write `search_docs` without the FTS5 table. A log fault (new `search.IsLogFault`, marked where the log's bytes are read) rolls back to the savepoint and is reported; any other thread error, or a cancelled context, rolls back the whole transaction;
   - `SetSearchVersion(search.Version)`, `db.RebuildSearchIndex`, new `db.OptimizeSearchIndex`, commit.
   A torn tail is indexed to the last whole frame and reported torn. A header-only log is indexed (it has no rows) and counted.
3. **Reporting.** Progress on stderr, prefixed `reindex --search:`, at most every 2 s. Text: `search: indexed N of M thread(s), P post(s), in T`, a `search: FAILED` line per failed thread, and `search: TORN` per torn one. `--json` adds `search` (`threads`, `indexed`, `posts`, `torn`, `failed`, `elapsed_ms`), `null` when not run; a search-only run leaves the projection fields zero. Exit 1 when a thread failed in either rebuild, 4 on a database or lock failure, with "the search index was not changed".
4. **A missing table.** New `db.MissingSearchTables` reads `sqlite_schema` after `Migrate`. Any missing search table exits 4 naming it and pointing to a backup; nothing is recreated.
5. **README.** The recovery block runs `ketju reindex --root . --projections --search`, with the sentence on what happens without `--search`.
6. **docs/03** gets an "As built" note under the run decision; the `Prepare` doc comment no longer says reindex calls it.

### Tests
- `internal/events/search/rebuild_test.go`: `TestRebuildEqualsIncremental` (random histories, three loss cases, same rows and same hits, integrity-check), `TestRebuildRollsBackAFailedThread` (a read fault injected on the second post's text after the first post's row was put), `TestRebuildKeepsTheOldIndexOnFailure` (cancel after the clear, a database error inside a thread, a failing version write; old rows and old version `Version-1` kept), `TestRebuildReportsATornTail`, `TestRebuildReadsTheLogNotTheIdx`.
- `cmd/ketju/reindex_search_test.go`: `TestReindexSearchEqualsIncremental` (posts, an edit, an author delete, a moderator hide, an auto-hide, a hide restored, a copy as split makes one, a rename, and `ketju redact` of a post and of a title; split itself is reserved in this build), `TestReindexSearchInterrupted` (cancel after the clear, and a database failure at the commit, both with an old version), `TestReindexSearchOffline`, `TestReindexFlags`, `TestReindexProjectionsKeepsSearch`, `TestReindexSearchDamagedThread`, `TestReindexSearchProgress`, `TestReindexSearchMissingTable`.
- `TestREADMERecoveryProcedure` runs the new block and checks the recovered index equals the one the logs give, not the backup's.
- `dbtest.DumpSearch` is the canonical dump both packages compare.

### Out of scope
- The index and the incremental indexer: TKT-01M29CRJ3B (Build the FTS5 search index and indexer).
- Shrinking the database file after a rebuild. `VACUUM` belongs to the backup documentation in Phase 4 (docs/09: "Metrics endpoint, backup documentation"). The rebuild only frees pages for reuse.
- Recreating search tables that were dropped by hand (step 4).
- Clearing the index in `ketju redact`: TKT-01M3NS122F (Clear the search index in the hard-redaction tool).

## Notes

**agent:claude-code/phase3-groom-search** at 2026-10-01T07:58:30Z

run decision: pending maintainer review. Recorded in docs/03 "Durability and recovery" (2026-10-01): --search rebuilds only the search index, both flags do both (projections first), and the rebuild is one transaction so an interrupted run leaves the old index in service; serve builds a missing index in the background, so no command is needed after an upgrade. The same line corrects docs/03's stale 'arrives with full-text search in Phase 2 (TKT-01M29C5Y25)'. Criterion 1 used to say 'Dropping the FTS tables'; it now says emptying the index or finding its segments damaged, because the tables' DDL lives only in the migrations (as ClearProjections already holds) and FTS5 delete-all plus rebuild covers a corrupt index. A table dropped by hand is reported (exit 4) rather than recreated.

**agent:claude-code/phase3-groom-search** at 2026-10-01T08:43:37Z

Terva review 1711 (PR #270, head e5998ef, base a599b48), fixed. (high) 'Discard partial rows when a thread's log is damaged': each thread of the rebuild runs in its own SAVEPOINT and a log fault rolls back to it, so the thread keeps no rows; TestReindexSearchDamagedThread now faults after a post was indexed (criterion 6). The same fix is applied to TKT-01M29CRJ3B's batched Deliver (TestAFailedThreadKeepsItsRows).

**agent:claude-code/phase3-groom-search** at 2026-10-01T09:47:41Z

Requirement carried from review 1731 (2026-10-01; PR #275, head 64a8983, base 6dfd01d; medium, 'Keep version preparation inside the reindex transaction'). This binds the implementing lane and overrides any plan text it contradicts. reindex --search must not call Indexer.Prepare as a separate committed step. The version check, ClearSearch and the search_meta.version write all happen inside the rebuild's one transaction, so a run cancelled or failing before commit leaves the earlier index, and its version, exactly as they were, whatever version they carry. TestReindexSearchInterrupted adds a case where the stored version differs from search.Version and the run is cancelled after the clear: the old rows and old version remain. The TKT-01M29CRJ3B plan's sentence that 'ketju reindex --search and ketju redact call it [Prepare] too' does not apply to reindex. serve's Prepare then clears that old-version index at its next start, before listening. Not fixed in the plan because PR #275 had used its two review rounds.

**agent:claude-code/phase3-search-cli** at 2026-10-01T12:58:49Z

Implementation decisions (internal, recorded in docs/03 'Durability and recovery' As built): (1) the rebuild is a method, search.Indexer.Rebuild, not the package function the groomed plan named, so it shares the indexer's open/walk seams. (2) In rebuild mode each log is opened alone: no .idx is read and the thread cache is not peeked, so the in-memory index comes from replaying every frame. Alternative, trusting the .idx as the incremental path does, lost because a recovery tool should depend on the logs only, and a stale sidecar (TKT-01M3VHSYFR) would otherwise reach the rebuilt index; TestRebuildReadsTheLogNotTheIdx holds it. (3) Rows are written with new db.PutSearchRow/DropSearchRow (search_docs only) and the FTS5 index is built once by 'rebuild' then 'optimize', as the plan said. (4) Log faults are marked positively (search.IsLogFault) where the log's bytes are read, as projection.IsLogFault does, so a database error inside a thread stops the run instead of being reported as a damaged thread. (5) A header-only log counts as indexed (it has no rows). Review 1731's requirement is met: Rebuild never calls Prepare; the version read, ClearSearch and the version write are in its one transaction (TestRebuildKeepsTheOldIndexOnFailure, TestReindexSearchInterrupted, both with an old version Version-1).

## Summary

ketju reindex --search rebuilds the search index from the logs in one transaction (search.Indexer.Rebuild): old version read, ClearSearch, every thread from its first frame through the indexer's per-thread path in a savepoint, version written, FTS5 rebuild and optimize, commit. Never calls Prepare (review 1731), so a cancelled or failing run leaves the old index and version. Log faults roll back their thread and exit 1; database errors and cancels roll back the run and exit 4. Rebuild reads each log alone, never its .idx. --search alone, --projections alone (default), both (projections first); --projections=false alone stays a usage error. JSON report gains a search object (null when not run). README recovery runs --projections --search. New: db.PutSearchRow/DropSearchRow/OptimizeSearchIndex/MissingSearchTables, search.IsLogFault, dbtest.DumpSearch. All six criteria met.
