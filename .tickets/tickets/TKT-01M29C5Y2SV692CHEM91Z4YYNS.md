---
schema: 3
id: TKT-01M29C5Y2SV692CHEM91Z4YYNS
title: Add the session sweeper and blob garbage collector
type: task
status: in-progress
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
claim:
  actor: agent:claude-code/phase1-run-438e26c1
  branch: run/c5y2s-sweeper-gc
  worktree: /home/sothr/workspace/git.local.sothr.com/terva-sh/ketju/.claude/worktrees/agent-a31bdf7fea30584cf
  commit: 89c7cb2b7bef955b25428f7976a3fc47e90971f5
  session: null
  claimed_at: 2026-09-28T09:00:08Z
  expires_at: null
archive: null
created_at: 2026-09-11T23:17:26Z
updated_at: 2026-09-28T09:00:28Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase1-run-438e26c1
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

Revised at claim (2026-09-28) from the code on main at 89c7cb2.

### Approach
1. New package `internal/jobs`: a `Scheduler` that runs named jobs on an interval, each on its own goroutine, first run after a short jittered delay. Every run is wrapped in `recover`; an error or panic is logged at error with the job name and the scheduler waits for the next tick. A finished run logs "job finished" at info with the job name and the count removed. `Stop` cancels and waits, bounded.
2. Session sweep, hourly: `session.Store.Sweep` deletes the rows that `ExpiredBefore(now)` calls expired, with one new sqlc query (`SweepSessions`) that is the exact complement of `LookupSession`'s liveness test. It uses the store's own clock, so the sweep and the lookup cannot disagree. Pre-login state is a sealed cookie, not a row, so the sweep never sees it; `pre_user` rows follow the same expiry rules as user rows. A request that resolved its session keeps it in the context, so a sweep mid-request changes nothing for that request; `Touch` already refuses to revive an expired row.
3. Blob GC, daily, in `internal/jobs/blobgc.go` with the path helper in `internal/store/blob` (docs/02 names `store/blob/`). Conservative by construction; a blob is deleted only when all of these hold:
   - `refcount = 0` and `unreferenced_since <= now - 7 d` (docs/02, docs/04), re-checked in the write transaction that deletes it, so an `AddBlobRef` committed first wins;
   - no `users.avatar_blob` names it and no draft's source mentions its hash (cheap guards; nothing writes either reference yet);
   - no frame body in any thread log under `threads/` contains its hash, in hex or raw, whatever the record type, deleted or not. The logs are the source of truth, so this holds even if refcounts drift. Any log that cannot be read cleanly, or any thread the last catch-up quarantined, holds the whole pass: nothing is deleted and the job logs why.
   The row delete and the file unlink run inside one `db.Write`, so they are serialised with every other writer. A failed unlink rolls the row delete back.
4. Serve registers both jobs after the projection has caught up and stops the scheduler before the thread store and database close.
5. Document in docs/03 "Attachments" what the GC checks and the ordering contract the Phase 3 upload and attach paths must keep: attach raises the refcount in the database before the log append, and upload writes the file and its row within one `db.Write`.

### Reading of the design (why this is not a block)
- A soft-deleted post keeps its blobs. docs/03 says a `Delete` leaves the `Post` bytes intact and `Restore` clears the flag; the blobs table counts "the posts referring to it", and nothing in the design lowers that count on a soft delete. The GC also checks the logs directly, so even a wrong refcount cannot free a blob a soft-deleted post names.
- Edits cannot drop an attachment: `Edit` has no attachments field (docs/03 record table, `EditBody`), so a post's attachments are fixed by its `Post` record for as long as the log holds it.
- Drafts cannot reference uploads: the `drafts` table has `source` and `reply_to` only, and the API refuses attachments ("nothing uploads yet"). The draft-source guard covers a pasted hash anyway.
- The seven days at zero is the grace period the design names; an upload starts at zero with `unreferenced_since` set to the upload time, so a fresh upload is never a candidate.

### Tests
- AC1: sweep deletes expired rows of both kinds at both boundaries and keeps live ones, including a session issued through `IssueUserWith`; a request resolved before a sweep still sees its viewer after the sweep; hourly interval registered in serve.
- AC2: six days twenty-three hours kept, seven days deleted (file and row); refcount returning above zero cancels; an `AddBlobRef` between candidate selection and the delete keeps the blob; a log reference keeps a refcount-zero blob (including a soft-deleted post's); an unreadable log or quarantine holds the pass; avatar and draft guards.
- AC3: captured slog output names the counts for both jobs.
- AC4: a job that errors, then panics, then succeeds runs a third time, and a second job keeps ticking throughout.

### Settled in grooming (2026-09-24)

- Blob GC needs `blobs.unreferenced_since`. Add it in BYYJ0 and in docs/04. Blobs arrive in Phase 3, so GC runs over an empty table until then; test it with fixture rows.
