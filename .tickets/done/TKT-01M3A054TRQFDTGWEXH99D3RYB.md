---
schema: 3
id: TKT-01M3A054TRQFDTGWEXH99D3RYB
title: Report projection drift and quarantined threads in ketju fsck
type: task
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area:ops
  - effort:S
assignees: []
milestone: phase-2
parent: TKT-01M29BN36Q9D32WQGYPFS336J0
origin: null
dependencies: []
blocks_on: none
references:
  - ref: pr:196
    path: null
  - ref: pr:200
    path: null
claim: null
archive: null
created_at: 2026-09-24T15:22:13Z
updated_at: 2026-09-30T03:43:08Z
created_by:
  id: agent:claude-code/phase1-run-415af6aa
  name: ""
updated_by:
  id: agent:claude-code/phase2-run-a054tr
  name: ""
extensions: {}
---

## Description

`ketju fsck` has a `DriftBytes` placeholder and prints "projection drift not checked: there is no database yet". The database exists now, and startup catch-up (TKT-01M29C5Y1S) classifies each thread the same way fsck would need to:
- a log longer than its projection is drift;
- a log shorter than its projection, or a projection with no log, is corruption.

Grooming for C5Y1S settled that a corrupt thread is "reported by /healthz and `ketju fsck`" (docs/03 "Durability and recovery"). Healthz is covered by TKT-01M29CCBZ8. Nothing covers fsck.

### Approach
Open the database read-only when it exists. Reuse `projection.Writer`'s walk and classification, or factor them out of `catchup.go` into something fsck can call without a Writer. Fill `DriftBytes`, set `DriftChecked`, exit 3 on drift, and report a corrupt thread by name as damage.

### Open items
fsck's help says it is safe against a live directory. Reading `projection_state` while serve writes it is fine under WAL, but the sizes and the rows are not read at one instant, so fsck may see drift that catch-up would never see. Say so in the output rather than lock.

## Acceptance criteria

- [x] With a database present, ketju fsck names each thread whose log is longer than its projection_state row, reports its drift_bytes, sets drift_checked true, and exits 3 (TestFsckReportsDriftAndExitsThree)
- [x] A log shorter than its projection, and a projection row with no log, are each named, counted in quarantined_threads, and make fsck exit 1; CatchUp quarantines the same thread IDs on the same directory (TestFsckReportsAQuarantinedThread)
- [x] Corruption outranks drift in the exit code, and a torn final frame is not reported as drift (TestFsckCorruptionOutranksDrift, TestFsckTornTailIsNotDrift)
- [x] fsck opens the database read-only: ketju.db's size, mtime and SHA-256 are unchanged by a run, and a directory without a database gets no db/ created and says drift was not checked (TestFsckOpensTheDatabaseReadOnly, TestFsckWithoutADatabase)
- [x] Beside a concurrent append and projection, fsck never reports that thread as quarantined, and its text output says drift beside a live serve may be lag (TestFsckBesideALiveProjection)
- [x] CatchUp's decisions come from one shared Classify function, and every existing catchup_test.go test passes unchanged

## Implementation plan

Groomed 2026-09-29 against main at 7af2c5a (TKT-01M3M9RR26, Phase 2 grooming). Revised 2026-09-30 at claim, against main at a827aef.

### Settled in grooming
- A log longer than its projection is drift; a log shorter than its projection, or a projection with no log, is corruption and is the thread serve quarantines (docs/03 "Durability and recovery", decided 2026-09-24 in TKT-01M29C5Y1S). docs/03 says a quarantined thread is "reported by `/healthz` and `ketju fsck`". /healthz already does (TKT-01M29CCBZ8, `internal/web/health/health.go`); this ticket is the fsck half.
- fsck stays safe against a live data directory and takes no lock (its help text, `cmd/ketju/fsck.go`, and this ticket's open item). Drift seen beside a running serve can be lag in flight; fsck says so in its output rather than locking.
- Exit codes are the ones `cmd/ketju/exit.go` already defines: 1 for corruption, 3 for drift. Corruption wins when both are found.

- (From the claim brief, carried as settled.) fsck reads the projection with `db.OpenReadOnly` **before** it walks the logs. Logs only grow and the projection is written after the log, so this order can turn a live append into apparent drift but never into apparent corruption.

### Approach (revised at claim; the groomed plan stands except where noted)
1. **`db.OpenReadOnly(ctx, path) (*DB, error)`** in `internal/store/db/db.go`. It stats the path first and refuses one that does not exist, so nothing is created. The read pool only, through the existing `openPool`, with DSN `mode=ro`, `busy_timeout` and `query_only(1)`, and no `journal_mode`, `synchronous` or `foreign_keys` pragma, because setting a journal mode is a write. No `makeDirDurable`, no `ensureWAL`, no directory fsync. `Write` on it returns `ErrReadOnly`, and `Close` closes the read pool without a checkpoint. SQLite may create `-wal` and `-shm` beside a stopped database, which the groomed plan accepts; `ketju.db` itself does not change, and a test proves it by size, mtime and SHA-256.
2. **`projection.Classify(logLen, projected int64) Class`** in `internal/events/projection/classify.go`, with `projection.Missing` (-1) for an absent log or row. Classes: `ClassCurrent`, `ClassUnfinished`, `ClassDrift`, `ClassCorrupt`. *Changed from the groomed three-argument form* (`logLen, projected, hasProjection`): a projection with no log is its own input, and passing a log length of 0 for it would classify a row of length 0 with no log as current, a change CatchUp does not make today. `CatchUp` calls it at all three decision points (the walk size, the reopened length, and a row with no log); every `catchup_test.go` test passes unchanged. `TestClassify` is the table.
3. **fsck** (`cmd/ketju/fsck.go`): when `<root>/db/ketju.db` exists, `OpenReadOnly` it and read `ListProjectionLengths` into a map, then walk the logs as now. Each log that walked clean is classified on its whole-frame length (`lastGood`, so a torn tail is not drift) against its row. A log fsck already reports damaged is not classified: its damage is the finding, and it exits 1 either way. A row with no log at its id's path gets its own report with an empty `path`, named by id.
4. **Report.** `threadReport.Projection` (`projection`: `current`, `drift`, `unprojected`, `unfinished`, `corrupt`; empty when not checked), `DriftBytes` filled for drift. Summary: `DriftChecked`, `DriftThreads` (`drift_threads`), `DriftBytes` (`drift_bytes`), `Quarantined` (`quarantined_threads`), and `DriftNote` (`drift_note`) saying why the check did not run. Text: a `DRIFT` line per drifting thread, a `QUARANT` line per corrupt one, totals, and when drift is found one line saying drift beside a running `ketju serve` may be lag catch-up is closing, so rerun with serve stopped to confirm.
5. **Exit.** `errCorrupt` when `Damaged > 0 || Quarantined > 0`, else `errDrift` when `DriftThreads > 0`.
6. **A database fsck cannot read** (fails to open, or no `projection_state`) is `drift not checked: <reason>`, `DriftChecked` false, and not an error on its own.
7. **Help text** drops "no database yet" and says what drift and quarantine mean.
8. **Redaction (TKT-01M29CXJZ9, PR #191, not yet on main).** Its `Redacted` record is `0x80`; `ScanFrames` is structural and checks no type, and redaction pads so a log never gets shorter. Its journal is `<root>/redaction.journal` and its swap temp `<hex>.log.redact`, and fsck walks only `threads/**.log`. A test writes a `0x80` frame and a journal and asserts fsck exits 0, so the tool cannot turn either into corruption whichever PR lands first.

### Tests
As groomed: `TestClassify`, `TestFsckReportsDriftAndExitsThree`, `TestFsckReportsAQuarantinedThread` (CatchUp over the same directory quarantines the same IDs), `TestFsckCorruptionOutranksDrift`, `TestFsckTornTailIsNotDrift`, `TestFsckOpensTheDatabaseReadOnly` (plus `TestOpenReadOnly` in package db: a write fails, a missing path is refused and not created), `TestFsckWithoutADatabase` (replaces `TestFsckSaysTheDriftCheckDidNotRun`), `TestFsckBesideALiveProjection`, and `TestREADMERecoveryProcedure` unchanged. Added: `TestFsckIgnoresARedactionMarkerAndJournal`, `TestFsckWithAnUnreadableDatabase`.

### Out of scope
- Repairing drift. `ketju serve` catch-up and `ketju reindex` own that.
- Reporting quarantined thread IDs over HTTP. `/healthz` reports only the word, by design (`QuarantinedCount`'s comment).
- Search-index drift. The FTS index is TKT-01M29CRJ3B (phase-3).

## Notes

**agent:claude-code/phase2-run-a054tr** at 2026-09-30T02:47:59Z

Terva review 1536 (Actions run #1248, request ready-review-5c89242) on PR #196, head 5c8924291ad7df19775550ddf5a98d16ec6fb43b, base a827aefa25f5e346a80c0aa89c1482fe5fab5c48. One medium finding: 'Do not report every projection as current when threads are quarantined or damaged' (cmd/ketju/fsck.go:341). Accepted and fixed: with no drift, the text now says 'projection current for every thread' only when nothing is quarantined or damaged, and otherwise 'no projection drift found; the QUARANT and DAMAGED threads above are not current'. TestFsckDoesNotCallCorruptThreadsCurrent covers a row with no log, a damaged log, and a clean directory. A fresh review of the new head follows.

**agent:claude-code/phase2-run-a054tr** at 2026-09-30T03:20:16Z

Terva review 1537 (Actions run #1250, request ready-review-b7b5603) on PR #196, head b7b5603d2e7feb3cd4e64f45812dcf1edec248b6, base e4253230957438c3b88ba8502a5d3553595eb416. It marked review 1536's finding resolved and raised one medium: 'Do not call an unfinished thread current' (cmd/ketju/fsck.go:344). Accepted and fixed: fsck now counts unfinished_threads, prints one line saying how many there are and what they are, and with no drift says 'every thread but the unfinished ones is current' instead of claiming every thread current. The unfinished-thread case in TestFsckDoesNotCallCorruptThreadsCurrent covers it. An unfinished thread is still not an error (exit 0), as catch-up treats it.

**agent:claude-code/phase2-run-a054tr** at 2026-09-30T03:42:39Z

PR #196 (head a2d338e7de40b962d5a3ba586fceddee9c6abe21, clean Terva review in Actions run #1266, CI run 1265) is not merged. Main moved to 5c2ba25 with the hard-redaction tool (#192), whose cmd/ketju/redact_test.go declares mapsEqual, and fsck_drift_test.go declared the same helper, so cmd/ketju's tests would not compile on main. Following the run brief's item 10, the final tree is carried onto 5c2ba25 as one commit on run/a054tr-fsck-drift-2, using maps.Equal in place of the helper. A new PR replaces #196 and gets a fresh review. The full race suite passes on the carried tree.

## Summary

ketju fsck now checks projection drift. When <root>/db/ketju.db exists it opens it with the new db.OpenReadOnly (mode=ro plus query_only, the read pool only, nothing created, no checkpoint on close) and reads projection_state before it lists or sizes any log. Each log that walked clean is classified on its whole-frame length by the new projection.Classify, which CatchUp now decides through at all three of its points. Drift (including a log never projected) is named with drift_bytes and exits 3. A log shorter than its projection, or a row with no log, is named as QUARANT, counted in quarantined_threads, and exits 1, and CatchUp over the same directory quarantines exactly those IDs. Corruption outranks drift. A database that is missing or unreadable leaves drift_checked false with a drift_note and is not an error.

One deviation from the groomed plan, recorded in the plan: Classify(logLen, projected int64) takes projection.Missing for either side absent, instead of the three-argument form, because the three-argument form would have classified a zero-length row with no log as current. Four mutations were tried against the tests, and each was caught (see the commit body). Redaction's 0x80 record, its redaction.journal and its <log>.redact temp file are tested not to read as corruption (TKT-01M29CXJZ9, PR #191, not yet merged).
