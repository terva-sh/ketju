---
schema: 3
id: TKT-01M3A054TRQFDTGWEXH99D3RYB
title: Report projection drift and quarantined threads in ketju fsck
type: task
status: ready
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
references: []
claim: null
archive: null
created_at: 2026-09-24T15:22:13Z
updated_at: 2026-09-29T07:18:12Z
created_by:
  id: agent:claude-code/phase1-run-415af6aa
  name: ""
updated_by:
  id: agent:claude-code/phase2-run-orchestrator
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

- [ ] With a database present, ketju fsck names each thread whose log is longer than its projection_state row, reports its drift_bytes, sets drift_checked true, and exits 3 (TestFsckReportsDriftAndExitsThree)
- [ ] A log shorter than its projection, and a projection row with no log, are each named, counted in quarantined_threads, and make fsck exit 1; CatchUp quarantines the same thread IDs on the same directory (TestFsckReportsAQuarantinedThread)
- [ ] Corruption outranks drift in the exit code, and a torn final frame is not reported as drift (TestFsckCorruptionOutranksDrift, TestFsckTornTailIsNotDrift)
- [ ] fsck opens the database read-only: ketju.db's size, mtime and SHA-256 are unchanged by a run, and a directory without a database gets no db/ created and says drift was not checked (TestFsckOpensTheDatabaseReadOnly, TestFsckWithoutADatabase)
- [ ] Beside a concurrent append and projection, fsck never reports that thread as quarantined, and its text output says drift beside a live serve may be lag (TestFsckBesideALiveProjection)
- [ ] CatchUp's decisions come from one shared Classify function, and every existing catchup_test.go test passes unchanged

## Implementation plan

Groomed 2026-09-29 against main at 7af2c5a (TKT-01M3M9RR26, Phase 2 grooming).

### Settled in grooming
- A log longer than its projection is drift; a log shorter than its projection, or a projection with no log, is corruption and is the thread serve quarantines (docs/03 "Durability and recovery", decided 2026-09-24 in TKT-01M29C5Y1S). docs/03 says a quarantined thread is "reported by `/healthz` and `ketju fsck`". /healthz already does (TKT-01M29CCBZ8, `internal/web/health/health.go`); this ticket is the fsck half.
- fsck stays safe against a live data directory and takes no lock (its help text, `cmd/ketju/fsck.go`, and this ticket's open item). Drift seen beside a running serve can be lag in flight; fsck says so in its output rather than locking.
- Exit codes are the ones `cmd/ketju/exit.go` already defines: 1 for corruption, 3 for drift. Corruption wins when both are found.

### Approach
1. **Read-only database open.** `db.Open` (`internal/store/db/db.go:103`) creates the directory, the file and the WAL, fsyncs the directory, and `Close` checkpoints, so fsck must not use it. Add `db.OpenReadOnly(ctx, path) (*DB, error)` in `internal/store/db/db.go`: the read pool only, DSN `mode=ro` plus the existing `query_only(1)`, no `makeDirDurable`, no `ensureWAL`, and a `Close` that does not checkpoint. `Write` on such a DB returns an error. It refuses a path that does not exist rather than creating it. SQLite reads a WAL database read-only only when its `-shm` and `-wal` files exist or can be created. If `mode=ro` cannot open a stopped directory's database without them, letting SQLite create those two files is acceptable, because they hold no data. `ketju.db` itself must not change. Alternative that lost: the `needDatabase` stat followed by `openDatabase(..., migrate=false)` that `setting get` uses (`cmd/ketju/admin.go:72-86`). It still goes through `db.Open`, which writes the WAL and checkpoints on close, and fsck's contract is that it writes nothing without `--rebuild`.
2. **Factor the classification out of catch-up.** `CatchUp` (`internal/events/projection/catchup.go:87-226`) classifies inline and mutates writer state. Add `internal/events/projection/classify.go` with a pure `Classify(logLen int64, projected int64, hasProjection bool) Class`, where `Class` is `ClassCurrent`, `ClassUnfinished` (header-only log, no projection), `ClassDrift` (log longer), `ClassCorrupt` (log shorter, or projection without log). `CatchUp` calls it at the points it now decides inline (lines 146-205), and its existing tests (`TestLogShorterThanProjectionIsCorruption`, `TestCatchUpEdges`, `TestHeaderOnlyLogIsUnfinished` and the rest of `catchup_test.go`) must pass unchanged. A table test `TestClassify` covers every row. The walk stays where it is: fsck already has its own walk (`findLogs`) and scans every frame, which catch-up does not. Alternative that lost: constructing a `projection.Writer` inside fsck. `projection.New` needs a writable DB and a `Sweep`, and catch-up repairs torn tails through `reopen`, which fsck must not do.
3. **fsck reads the projection before it scans.** In `cmd/ketju/fsck.go`, when `<root>/db/ketju.db` exists, open it with `OpenReadOnly` and read `ListProjectionLengths` (`internal/store/db/queries/projection.sql:13`) into a map *before* the log walk. Logs only grow, so this order can turn a live append into apparent drift but never into apparent corruption. The opposite order could report a healthy thread as corrupt, which is the worse error. Settled here as an engineering choice.
4. **Per-thread verdict.** For each log, classify the scanned good length (the length to the last whole frame that `checkThread` already computes, so a torn tail is not counted as drift) against the projection. Fill `threadReport.DriftBytes` for drift. Add `threadReport.Projection string` (JSON `projection`): `current`, `drift`, `corrupt`, `unfinished`, `unprojected` (a log with frames and no row; drift of the whole log). A projection row whose log is missing gets its own `threadReport` with `Path` empty and `Projection: "corrupt"`, so it is named by ID.
5. **Summary.** Set `fsckSummary.DriftChecked = true` when the database was read. Add `DriftThreads int` (`drift_threads`), `DriftBytes int64` (`drift_bytes`) and `Quarantined int` (`quarantined_threads`). Replace the text line at `fsck.go:167` with: `projection drift not checked: no database at <path>` when there is none, or with a `DRIFT   ` line per drifting thread and a `QUARANT ` line per corrupt one, then totals. When drift is found, the text adds one line: drift found beside a running `ketju serve` may be lag that catch-up is closing, so rerun with serve stopped to confirm.
6. **Exit.** `errCorrupt` when `Damaged > 0 || Quarantined > 0`; otherwise `errDrift` when `DriftThreads > 0`. `exitCodeFor` already ranks corrupt before drift.
7. **A database fsck cannot read.** A database that exists and fails to open, or has no `projection_state` table because its schema is behind the binary, is reported as `drift not checked: <reason>` with `DriftChecked` false, and is not an error on its own. The log check is fsck's first job and must not depend on the database.
8. Help text: drop the "no database yet" wording; list what drift and quarantine mean and that code 3 is drift. README (`README.md:64-90`) needs no change: after `ketju reindex` the projection is current, so fsck exits 0.

### Tests
- `TestClassify` (projection): the table in step 2, including a projection without a log and a header-only log.
- `TestFsckReportsDriftAndExitsThree`: a DB whose `projection_state` is behind one log. The thread is named, `drift_bytes` is the gap, exit 3.
- `TestFsckReportsAQuarantinedThread`: a log shorter than its projection, and a projection row with no log. Both are named, `quarantined_threads` is 2, exit 1. The same directory served by `projection.CatchUp` quarantines the same two IDs, which ties fsck's verdict to serve's.
- `TestFsckCorruptionOutranksDrift`: one damaged log and one drifting thread, exit 1.
- `TestFsckTornTailIsNotDrift`: a torn final frame whose whole-frame length equals the projection is `current`.
- `TestFsckOpensTheDatabaseReadOnly`: after a run, `db/ketju.db` has the same size, mtime and SHA-256 as before, and a write through the returned `*DB` fails.
- `TestFsckWithoutADatabase`: replaces `TestFsckSaysTheDriftCheckDidNotRun` (`fsck_test.go:351`); asserts `drift_checked` false, the new message, and that no `db/` directory was created.
- `TestFsckBesideALiveProjection`: appends and projects concurrently while fsck runs (as `TestFsckIsSafeWhileAThreadIsBeingAppendedTo` does for logs) and asserts fsck never reports `corrupt` for that thread.
- `TestREADMERecoveryProcedure` (`cmd/ketju/reindex_test.go:769`) still passes, which proves fsck exits 0 after reindex.

### Out of scope
- Repairing drift. `ketju serve` catch-up and `ketju reindex` own that.
- Reporting quarantined thread IDs over HTTP. `/healthz` reports only the word, by design (`QuarantinedCount`'s comment).
- Search-index drift. The FTS index is TKT-01M29CRJ3B (phase-3).
