---
schema: 3
id: TKT-01M29C5Y2SV692CHEM91Z4YYNS
title: Add the session sweeper and blob garbage collector
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:db
  - area:ops
  - effort:S
assignees: []
milestone: phase-1
parent: TKT-01M29BKBKV6HH8W0DN6QXQNZ0R
origin: null
dependencies:
  - TKT-01M29BYYH73ZBC9WTS7HN03PKJ
  - TKT-01M29BYYJ0WJ3C7RRZQMDRP5ZB
blocks_on: none
references:
  - ref: design:background-work
    path: docs/02-architecture.md
claim: null
archive: null
created_at: 2026-09-11T23:17:26Z
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

Two of the background jobs from the docs/02 table: an hourly sweep deleting expired sessions, and a daily pass deleting blobs whose refcount is zero and which are older than seven days.

The seven-day delay is the safety margin. A blob dropping to zero refcount during an edit that is then undone must not have had its bytes deleted in the meantime.

## Acceptance criteria

- [ ] Expired sessions are deleted hourly and an in-flight request holding one is not affected mid-request
- [ ] A blob at refcount zero is deleted only after seven days, and a refcount returning above zero cancels that
- [ ] Both jobs log what they removed, with counts
- [ ] Both jobs survive a failure without stopping the scheduler

## Implementation plan

Pre-groom plan (2026-09-24). Revise at claim, after reading the code as it stands then.

### Approach
1. New package `internal/jobs`: `Scheduler.Every(name string, interval time.Duration, fn func(ctx) (Result, error))`. Each job runs on its own goroutine with a ticker and a jittered first run. Every run is wrapped in `recover`. An error or panic logs at error with the job name and waits for the next tick. The scheduler stops on context cancel. `Result` is `{Removed int; Detail []any}` and is logged at info as "job finished" with counts.
2. Session sweep, hourly. A sqlc query in `queries/identity.sql` deletes rows where `expires_at <= now` OR `last_used_at <= now - idle`. The idle and hard durations (30 d and 90 d, docs/05) are exported constants owned by the session store (TKT-01M29C5Y3E), and the sweep imports them rather than restating them.
3. The in-flight request AC: the session middleware resolves the session once, at request start, into the context. The sweep deleting the row does not reach into that request. The test proves it at the store boundary.
4. Blob GC, daily. It uses BYYJ0's `blobs.unreferenced_since`. It selects candidates with `refcount = 0 AND unreferenced_since <= now - 7d`. For each one, it runs `DELETE FROM blobs WHERE hash = ? AND refcount = 0 AND unreferenced_since <= ?` inside a write transaction, re-checking the conditions so a concurrent increment wins. It unlinks the file (and any thumbnail) only if a row was deleted. If the unlink fails, the error is logged and the orphan file is left for the next pass to find. A refcount rising above zero clears `unreferenced_since`, which is the blob store's increment query.
5. The blob path helper: the blob store is TKT-01M29CXK0 (Phase 3). Define `blobPath(root, hash)` here following docs/03 "On-disk layout" (`blobs/sha256/xx/<hash>`), and let CXK0 reuse it rather than redefine it.
6. Serve (CCBYX) registers both jobs after the DB is open and stops the scheduler before closing the DB.

### Files and packages
New: `internal/jobs/{scheduler.go,scheduler_test.go}`, `internal/jobs/sessionsweep.go`, `internal/jobs/blobgc.go` (or `internal/store/blob/gc.go`, since docs/02 lists `store/blob/`; choose at claim), and queries added to `queries/identity.sql` and `queries/moderation.sql`. Reuses `db.DB.Write` and `logging`.

### Tests
- AC1: `TestSweepDeletesExpiredOnly` uses a fake clock with sessions just before and just after both expiries. `TestSweepDoesNotAffectResolvedRequest` resolves a session through the store (or a fixture resolver until C5Y3E lands), sweeps, and then has the handler read the viewer from the context. Also check that the scheduler's interval for the sweep is one hour.
- AC2: `TestBlobGCSevenDayWindow` checks that a blob at zero for 6 d 23 h is kept and one at zero for 7 d is deleted, with both file and row gone. `TestRefcountReturningCancels` goes zero, then an increment, then 8 d pass, and the blob is kept. Add a race test: an increment committed between candidate selection and delete keeps the blob.
- AC3: capture the slog output with a test handler and assert the "removed" counts on both jobs.
- AC4: `TestSchedulerSurvivesErrorAndPanic` makes a job that errors, then panics, then succeeds. The third run happens and the other job keeps ticking.

### Design
docs/02-architecture.md "Background work". docs/05-identity-and-accounts.md "Sessions and CSRF". docs/03-thread-storage.md "Attachments" and "On-disk layout". docs/04-data-model.md "sessions" and "blobs".

### Interactions
Consumes BYYH7 (sessions) and BYYJ0 (blobs with `unreferenced_since`), plus the session constants from TKT-01M29C5Y3E. Provides `jobs.Scheduler` to CCBYX and to later background jobs. Coordinates the blob path helper with TKT-01M29CXK0.

### Open items
docs/02 says "refcount 0 and older than 7 days" without saying older than what. This plan reads it as seven days at zero, which is the ticket's rationale, and BYYJ0 adds the column to support it. Blob GC lands in Phase 1 but has no blobs to delete until Phase 3.

### Settled in grooming (2026-09-24)

- Blob GC needs `blobs.unreferenced_since`. Add it in BYYJ0 and in docs/04. Blobs arrive in Phase 3, so GC runs over an empty table until then; test it with fixture rows.
