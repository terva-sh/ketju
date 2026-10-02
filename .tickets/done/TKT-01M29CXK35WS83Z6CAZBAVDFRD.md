---
schema: 3
id: TKT-01M29CXK35WS83Z6CAZBAVDFRD
title: Ship ketju backup and the backup documentation
type: task
status: done
status_reason: null
priority: high
due_on: null
labels:
  - area:ops
  - effort:M
assignees: []
milestone: phase-4
parent: TKT-01M29BN36Q9D32WQGYPFS336J0
origin: null
dependencies:
  - TKT-01M29BW9PB11V2AHE52C0F49JG
  - TKT-01M29CCBYX4V6ZP7N9HF8H8NCZ
blocks_on: none
references:
  - ref: design:open-questions-ops
    path: docs/10-open-questions.md
  - ref: design:architecture-ops
    path: docs/02-architecture.md
  - ref: pr:358
    path: null
claim: null
archive: null
created_at: 2026-09-11T23:30:21Z
updated_at: 2026-10-02T20:51:06Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase4-run-cxk35
  name: ""
extensions: {}
---

## Description

A command that takes a consistent snapshot: pause the thread actors, checkpoint the WAL, `VACUUM INTO` the database, and copy the data directory. Plus the documented `rsync` procedure for operators who prefer their own tooling.

The claim in docs/00 is that backups are `rsync`. That is true for the logs and blobs, which are immutable once written, and not quite true for the database, which is why the command exists.

A backup nobody has restored is a guess. The restore procedure is part of this ticket, tested end to end.

## Acceptance criteria

- [x] Against a running serve that takes posts throughout, `ketju backup --root DIR DEST` writes a backup. Copied into an empty data directory, it passes `ketju fsck` with exit 0 and serves every thread it holds (TestBackupRestoresAndPassesFsck)
- [x] The same holds for a stopped data directory whose logs are ahead of their projection, and the source is never written (TestBackupOfAStoppedDataDirectory)
- [x] Appends are held only while serve catches the projection up, checkpoints the WAL and runs VACUUM INTO. Reads go on meanwhile, held appends complete afterwards, and the pause is capped (TestBackupPausesAppendsOnlyForTheSnapshot)
- [x] The control socket is mode 0600, answers only a peer with serve's uid, and takes one snapshot at a time (TestBackupSocketAnswersOnlyItsOwner). Every thread append goes through posts.Writer (TestEveryAppendGoesThroughTheWriter)
- [x] A backup leaves out .idx files, thumbnails, blobs/tmp, secrets/ and config.toml, and holds every blob that its logs name (TestBackupLeavesOutIndexesThumbnailsStagingAndSecrets)
- [x] The README documents ketju backup, its restore, and the rsync procedure with its order and consistency caveats. The backup-and-restore block runs as a test (TestREADMEBackupProcedure)
- [x] The README's database-only recovery takes the database from a ketju backup and the content from reindex, and runs as a test (TestREADMERecoveryProcedure)
- [x] If an append stalls, a pending pause gives up within its cap and lets the queued appends go (TestHoldGivesUpOnAStalledAppend)

## Implementation plan

Groomed 2026-10-02 for Phase 4 (TKT-01M3YMG0M1) against main at ed82f15e.

### Settled in grooming

- **The command ships in Phase 4, and it pauses the thread actors, checkpoints the WAL, and snapshots** (docs/10 Q23, decided by the maintainer 2026-09-24, TKT-01M29BW9PB; docs/02 "Backups"). Until it lands, docs/02 and the README document rsync plus `VACUUM INTO`.
- **A backup restores** (docs/15 "Invariants, added"). A snapshot that `ketju backup` produces passes `ketju fsck` after it is restored. This plan reads "passes" as exit 0: no damage, no drift, and no quarantined thread.
- **Content comes from the logs and never from a database backup** (docs/00 principle 2, docs/03 "Durability and recovery", README "Recovering a lost or damaged database"). The database-only recovery stays `reindex --projections --search` over a restored database file.
- **How the command reaches a running server, and what a backup holds.** These are run decisions, pending maintainer review, recorded in docs/02 under "Backups" in this grooming PR. The command uses a Unix socket, `data/ketju.sock`, mode 0600, which answers only a peer with serve's uid. Appends are held at `posts.Writer` only while serve catches the projection up, runs a passive checkpoint, and runs `VACUUM INTO` a staging file inside the data directory. Each log is copied up to the length the snapshot projected it to. A backup holds `db/`, `threads/` without `.idx`, and `blobs/` without `.thumb` files or `tmp/`. It never holds `config.toml` or `secrets/`. With no serve running, the command takes the lock and needs no pause.
- **House style for an offline tool** (`cmd/ketju/fsck.go`, `reindex.go`, `redact.go`):
  - `flag.NewFlagSet` with a usage text that lists the exit codes;
  - `--root DIR` and `--json`;
  - the report on stdout and progress on stderr;
  - the exit codes in `exit.go`;
  - a `backupMain(ctx, out, errOut, args, backupOpts)` with seams for tests.
- **Layering.** `cmd/ketju` does not open a SQLite driver of its own (`TestREADMERecoveryProcedure` says why). `VACUUM INTO` is a method in `internal/store/db`.

### Approach

1. **The snapshot in `internal/store/db`.** Add `(*DB).SnapshotInto(ctx, dest string) error`. It runs `VACUUM INTO ?` on a connection opened for it alone, outside any transaction.
   - It does not use the write connection. That would make every writer wait for the length of the copy.
   - It does not use a `query_only` read connection, which may refuse the statement.
   - The connection registers FTS5 through `openPool`, as the `driver.go` comment already anticipates.
   - `dest` must not exist. The method fsyncs the file and its directory.
   - Add `(*DB).CheckpointPassive(ctx)` (`wal_checkpoint(PASSIVE)`). It never waits on a reader. Because `VACUUM INTO` reads through the WAL, a passive checkpoint that stops short costs nothing.
2. **The append gate in `internal/service/posts`.** It must be a gate that can be abandoned, not a `sync.RWMutex`. A pending `RWMutex.Lock` cannot be cancelled, and while it waits it blocks every new reader, so one stalled append could hold all appends past the cap (Terva review 1943). `Writer` gains a small gate:
   - **State.** A mutex guards three fields: `active`, the count of appends in flight; `held`, a bool; and `open`, a channel that is closed whenever `held` is false.
   - **`Append`.** While `held` is true, it waits on `open` without holding the mutex. Then it increments `active`, runs `th.Actor.Append`, and decrements `active`. The decrement signals the holder when `active` reaches 0.
   - **`(*Writer).Hold(ctx) (release func(), err error)`.**
     1. It sets `held` and makes a fresh `open`. New appends now wait.
     2. It waits until `active` is 0 or until `ctx` is done.
     3. If `ctx` is done first, it clears `held`, closes `open`, and returns `ctx.Err()`. The waiting appends then go on, and the stalled one finishes whenever its actor does.
     4. `release` clears `held` and closes `open`.
   - Reads never touch the gate.

   serve builds exactly one `Writer` (`threadSvc.Writer`), and the jobs and services reach it through `threadSvc`. Add `TestEveryAppendGoesThroughTheWriter`. It scans the non-test Go files under `internal/` and `cmd/` for `.Actor.Append(` and fails on any caller outside an allowlist. The allowlist is `posts.Writer`, `cmd/ketju/e2e.go` (seeding, not served) and `threadlog` itself. This follows the session chokepoint test's pattern.
3. **The serve side in a new `internal/backup` package.**
   - `Snapshot(ctx, Deps)` runs the five steps in order:
     1. `Hold(ctx)`;
     2. `proj.CatchUp(ctx, reopen)`, the same sweep serve's startup runs;
     3. `CheckpointPassive`;
     4. `SnapshotInto(data/backup.staging-<random>/ketju.db)`;
     5. `release`.

     It returns the staging path and the pause in milliseconds. One context, with a deadline of `PauseCap` (60 s) from the call, runs all five steps, so the cap counts from the moment appends start waiting. When the cap passes, the step in progress is abandoned:
     - `Hold` gives up as in step 2;
     - `CatchUp` and `VACUUM INTO` are cancelled (SQLite interrupt).

     Then the pause is released, the staging directory is removed, and the request fails.
   - `Listen(dataDir, uid, snap)` creates `ketju.sock`, chmods it to 0600, and accepts connections.
   - The peer's uid is read with `golang.org/x/sys/unix`:
     - `GetsockoptUcred` on Linux;
     - `GetsockoptXucred` on darwin and the BSDs.

     That promotes `golang.org/x/sys` from indirect to direct, with no new module. A peer whose uid is not serve's is answered `forbidden` and nothing is paused. On a platform with no peer-credential call, serve does not open the socket, logs that only offline backups work, and goes on.
   - The protocol is one JSON line each way:
     - the request is `{"v":1,"op":"snapshot"}`;
     - the answer is `{"ok":true,"staging":"backup.staging-…/ketju.db","paused_ms":N}` or `{"ok":false,"error":"…"}`.

     A second request while one runs answers `busy`. The protocol is internal to one binary version. An unknown `v` is refused, so a CLI and a serve from different versions fail plainly.
   - A socket path longer than the platform limit (104 bytes on darwin, 108 on Linux) is not opened. serve logs why, and the CLI says the same.
4. **Wiring in `cmd/ketju/serve.go`.**
   - Under the lock and before the database opens, remove any stale `ketju.sock` and `backup.staging-*`, beside `health.RemoveStale`.
   - A new step, "control socket", after "jobs" and before "listener", starts `backup.Listen`.
   - Its closer stops accepting, waits for a snapshot in progress up to `busDrain`, and removes the socket. The closer runs before the bus and the stores close.
   - `serveOpts` gains a `snapshotHold func()` seam, run while appends are held, for the pause test.
5. **The command, in `cmd/ketju/backup.go`.** The usage is `ketju backup [--root DIR] [--json] DEST`.
   1. DEST must not exist (exit 2). Its parent must exist. Everything is written into a sibling, `DEST.partial-<random>`, which is renamed to DEST only at the end, and the parent is then fsynced.
   2. A pending `redaction.journal` refuses with exit 4.
   3. `lock.Acquire(root, "backup")`:
      - **Taken (offline).** `db.OpenReadOnly` and `SnapshotInto` the partial directory. Each log is copied whole, to its last whole frame. Hold the lock to the end, so `serve`, `reindex` and `redact` cannot start midway.
      - **`HeldError` whose holder's purpose is `serve` (online).** Send the snapshot request over the socket. Move the staged database into the partial directory (rename, or copy and fsync across filesystems) and remove the staging directory. Copy each log up to the length that the snapshot's `projection_state` gives it (`ListProjectionLengths` on the copy). Logs only grow, so that prefix is exactly what the snapshot projected. A log with no row is left out: it is a thread created after the snapshot, or one that was never finished.
      - **Held by anything else.** Exit 4, naming the holder from `HeldError`.
   4. Copy `blobs/` after the logs, leaving out `tmp/` and `*.thumb`. A blob file is written before the post that names it, so every blob a copied log names is already on disk. Copy no `.idx` file, no `secrets/` and no `config.toml`. fsync every file and directory.
   5. Run the projection's catch-up over the copy (`projection.New` plus `CatchUp` with a `threadcache` over the partial directory, as serve does at startup). Online it finds nothing, and the test asserts that. Offline it closes any drift the source had, so the backup's logs and projection agree. Only the copy is written: the source is never written in either mode.
   6. Verify the copy with fsck's walk. Factor `fsckMain`'s body into `runFsckWalk(root) fsckSummary` so both commands share it. Rename the partial directory to DEST only if the walk finds no damage, no drift and no quarantined thread.
   7. Print the report (text, or `--json`):
      - the mode;
      - threads, bytes and blobs;
      - `paused_ms` (online);
      - elapsed time.

   Exit codes:
   - **0:** written and verified.
   - **1:** the verification found a damaged log or a log shorter than its projection. That comes from the source, and the report names each thread and says to run `ketju fsck` on the source. The partial directory is kept and named.
   - **2:** usage, DEST exists, or there is no `threads/`.
   - **4:** the data directory is in use by something other than serve, a redaction is pending, the socket is unreachable or refused the request, or an I/O failure. The partial directory is removed.

   Add the command to `main.go`'s usage and switch.
6. **The README.**
   - **A new section, "Backing up and restoring".** Taking a backup with `ketju backup`, with or without serve running, and what it leaves out. Restoring it, as one `sh` block: copy DEST into an empty data directory, run `ketju fsck --root`, and start serve with the operator's own `config.toml` and `secrets/`.
   - **The rsync procedure, with its caveats:**
     - take `VACUUM INTO` first, then rsync `threads/` excluding `*.idx`, then rsync `blobs/` excluding `tmp/` and `*.thumb`, in that order;
     - the copy then shows drift, which serve's startup catch-up closes, so `ketju fsck` exits 3 until serve has run;
     - the `sqlite3` CLI used for `VACUUM INTO` needs FTS5;
     - with serve stopped, a plain rsync of the data directory is consistent.
   - **The existing "Recovering a lost or damaged database" section** names a `ketju backup`'s `db/ketju.db` as `$KETJU_BACKUP`, so the database-only case keeps taking its content from reindex.
   - **docs/02 "Backups"** loses its "until then" sentence once the command lands.
   - **`ketju backup -h`** lists the exit codes.

### Tests

- `cmd/ketju` `TestBackupRestoresAndPassesFsck` (docs/15's invariant):
  - Setup: serve runs in-process through `serve()`, and a goroutine creates threads and replies through the threads service throughout.
  - Action: `backupMain` writes DEST, then DEST is copied into an empty directory.
  - Checks: `fsckMain` returns nil (exit 0) with no drift, torn tail or quarantined thread. serve starts on the copy, and every thread the backup holds dumps the same records as the source did, up to the backup's lengths. The `users` and `settings` rows equal the source's at the snapshot.
- `TestBackupOfAStoppedDataDirectory`:
  - Setup: a source whose log was appended past its projection, as a crash leaves it.
  - Checks: the backup holds those records, and the copy passes fsck with exit 0. The source's database and logs are byte-identical afterwards.
- `TestBackupPausesAppendsOnlyForTheSnapshot`:
  - Setup: the `snapshotHold` seam holds the pause open.
  - Checks:
    - an append started during the pause returns only after the release, and succeeds;
    - a thread page read during the pause is served;
    - appends before and after the pause are not delayed;
    - a `PauseCap` seam shorter than the snapshot fails the request, releases the pause, and leaves no `backup.staging-*`.
- `internal/service/posts` `TestHoldGivesUpOnAStalledAppend` (Terva review 1943):
  - Setup: an actor seam blocks one append mid-write, and `Hold` runs with a short deadline.
  - Checks:
    - `Hold` returns `context.DeadlineExceeded` within the deadline plus a margin;
    - appends that queued behind the pending hold complete once it gives up;
    - the stalled append completes when it is released;
    - a second `Hold` then succeeds.
  - `TestHoldWaitsForInFlightAppends`: an append in flight when `Hold` is called finishes before `Hold` returns, and no append starts between `Hold` and `release`. Run under `-race`.
- `TestBackupSocketAnswersOnlyItsOwner`:
  - Setup: a peer-credential seam reports another uid.
  - Checks:
    - the socket is mode 0600;
    - the request with the other uid is answered `forbidden`, and appends are never held;
    - a second request during a snapshot answers `busy`;
    - an unknown protocol version is refused.
- `TestBackupRefuses`, which checks four cases:
  - DEST exists: exit 2;
  - the lock is held by `reindex`: exit 4, naming it;
  - `redaction.journal` is present: exit 4;
  - serve holds the lock but the socket is gone: exit 4.

  In every case nothing is left beside DEST.
- `TestBackupLeavesOutIndexesThumbnailsStagingAndSecrets`. The copy has no `.idx`, `.thumb`, `blobs/tmp/` entry, `secrets/` or `config.toml`, and every blob a copied log names is present.
- `TestServeRemovesStaleBackupStaging`. A leftover socket and staging directory are removed at startup, under the lock.
- `TestEveryAppendGoesThroughTheWriter` (step 2).
- `internal/store/db` `TestSnapshotIntoBesideAWriter`. While another handle commits, `SnapshotInto` writes a file that opens on its own with no `-wal` file. The file passes `PRAGMA integrity_check` and holds the search tables.
- `TestREADMEBackupProcedure`. The new section's `sh` block runs as written, with `ketju` being the test binary, as `TestREADMERecoveryProcedure` runs its block.
- `TestREADMERecoveryProcedure`, extended: `$KETJU_BACKUP` is the `db/ketju.db` of a `ketju backup`.
- Mutations to try and record in the commit body:
  - copying logs whole online (fsck exit 3 must fail the restore test);
  - skipping `Hold`;
  - copying `.idx` files;
  - leaving the peer check out.

### Out of scope

- Scheduled backups, retention, encryption and offsite copies. These are the operator's cron and storage, and no ticket owns them.
- A backup metric on `/metrics`. Add it to TKT-01M29CEE7S at its claim if wanted.
- Restoring `config.toml` and `secrets/`. The README says the operator keeps them.
- The Phase 4 exit test that calls this command: TKT-01M3YMG0NX.

## Notes

**agent:claude-code/phase4-groom-ops** at 2026-10-02T17:18:02Z

run decision: pending maintainer review (2026-10-02, Phase 4 grooming). Recorded in docs/02 under "Backups".

- `ketju backup` reaches a running serve through a Unix socket, `data/ketju.sock`. The socket is mode 0600 and answers only a peer with serve's uid.
- serve holds appends at `posts.Writer` only while it catches the projection up, runs a passive WAL checkpoint, and runs `VACUUM INTO` a staging file in the data directory.
- The command copies each log up to the length the snapshot projected it to.
- With no serve running, the command takes the lock instead and needs no pause.
- A backup holds `db/`, `threads/` without `.idx`, and `blobs/` without thumbnails or `tmp/`. It never holds `config.toml` or `secrets/`.

Alternatives:
- An admin API route lost, because it would give a web session power over the server's disk, which docs/05 gives no rule for.
- A signal lost, because it has no reply and a stale pid could hit another process.
- Offline only lost, because docs/10 Q23 decided the command pauses a running server's actors.
- No pause lost, because the backup's logs and database would disagree.

Flagged for the orchestrator: the socket is a new local control channel. The run reads it as within the design, since it grants nothing beyond the data directory's own file permissions. If the maintainer reads it as stop class 1, the offline path in step 5 still stands on its own.

**agent:claude-code/phase4-run-cxk35** at 2026-10-02T20:45:50Z

Where the build departs from the plan, and why:

- **The control socket answers `forbidden` before reading the request.** The peer's uid is checked as soon as the connection is accepted, so a stranger's bytes are never parsed. `backup.Ask` tolerates the write failing when the server has already answered and closed.
- **A socket that cannot be bound is a warning, not a refusal to start**, when the platform has no peer-credential call or the path is longer than a Unix socket allows. `ketju backup` then reports serve as unreachable (exit 4) and the README's own-tools procedure still applies. Any other bind failure stops the start, as every other step does.
- **Exit codes.** A missing DEST parent is exit 2 (usage). Offline with no database is exit 4. Verification finding a log ahead of its projection on a copy at this binary's schema counts as exit 1, with the copy kept and named, because the catch-up has just closed it and drift then means the copy is wrong.
- **The copy's indexes are removed after its catch-up**, which opens each log through the thread cache and so writes `.idx` files; a backup holds none.
- **The copy's empty `-wal` and `-shm` are dropped**, so the backup's database is one file.
- **A database at another schema is copied at it** (added after the plan). `catchUpCopy` asks `db.Pending` and neither migrates nor catches up a copy whose schema is older or newer than the binary's. Migrating it would have made the backup an upgrade takes one the old binary refuses (`TestNewerSchemaRefused`). Such a copy keeps whatever drift its source had, which is reported in the summary (`schema`, `drift_threads`) and not a failure; serve closes it when it next starts. TKT-01M29CXK3S's upgrade procedure relies on this. Tested by `TestBackupLeavesAnotherSchemaAlone` (older and newer) and `TestPendingCountsWithoutWriting`.

## Summary

Landed `ketju backup --root DIR DEST`, which writes a verified, consistent backup whether serve is running or not. docs/15's invariant is `TestBackupRestoresAndPassesFsck`: a snapshot `ketju backup` makes passes `ketju fsck` after restore.

How it works with serve running:
- serve takes snapshots through a control socket, `ketju.sock` (internal/backup). The socket is mode 0600 and checks the peer's uid; it runs one snapshot at a time.
- A snapshot holds appends through `posts.Writer.Hold`, capped at 60 s, then catches up, checkpoints, and runs `VACUUM INTO`.
- Each log is copied to its projected length, so the logs and the database agree.

How it works with serve stopped:
- backup takes the lock, copies the database and the whole logs, and catches the copy up.
- A database at another schema is copied as it is and is not migrated (`db.Pending`), so the binary that wrote it can still restore it.

In both cases:
- blobs are copied; indexes, thumbnails, staging, secrets/ and config.toml are left out.
- The copy is verified with fsck's walk, synced, and renamed into place.

The README covers backup, restore, the rsync procedure, and the database-only recovery from a backup. Each runs as a test. docs/02 is updated.

The deviations from the plan are in the notes. There is no migration. golang.org/x/sys is now a direct dependency.
