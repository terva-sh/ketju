---
schema: 3
id: TKT-01M29C5Y2SV692CHEM91Z4YYNS
title: Add the session sweeper and blob garbage collector
type: task
status: done
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
  - ref: pr:110
    path: null
claim: null
archive: null
created_at: 2026-09-11T23:17:26Z
updated_at: 2026-09-28T09:26:38Z
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

- [x] Expired sessions are deleted hourly and an in-flight request holding one is not affected mid-request
- [x] A blob at refcount zero is deleted only after seven days, and a refcount returning above zero cancels that
- [x] Both jobs log what they removed, with counts
- [x] Both jobs survive a failure without stopping the scheduler

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

## Notes

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T09:08:25Z

Gaps settled at claim, written into docs/03 "Blob garbage collection" in the same PR (docs/12 "Gaps the design leaves open": internals only, the checks only ever keep more):

- Soft-deleted posts keep their blobs. This is read from the design, not chosen: docs/03 says a Delete leaves the Post bytes intact and Restore clears the flag, the blobs table counts "the posts referring to it", and Edit has no attachments field. So no design path lowers a refcount except the offline hard deletion. Drafts have no attachment column. As a guard against refcount drift, the GC also refuses to delete any blob whose hash appears in any log record body (hex, either case), or that an avatar or a draft source names.
- Alternatives considered for the log check: (a) trust refcount alone, as the pre-groom plan did. It lost because the refcount maintainer does not exist yet (TKT-01M29CXK0, Phase 3), and that draft's AC ("same bytes uploaded twice ... two refcounts") already reads refcount differently from docs/04. (b) Decode Post bodies and match attachments[] exactly. It lost because the attachment element shape is "the least settled thing" in records.go, so a raw hex search is shape-independent. (c) Also search for the raw 32-byte form. It lost because that is an O(n) map lookup per byte, and the frozen format stores attachments as strings.
- Ordering contract for CXK0: AddBlobRef before the Post append, and refuse the attach when it changes no row. The upload writes the file and row inside one db.Write. Thumbnail naming is left to CXK0.
- A quarantined thread (projection LastCatchUp().Corrupt), an unreadable log, an unknown body encoding or a missing threads/ holds the whole pass, which logs the reason at error. A permanently quarantined thread therefore stops blob GC until an operator repairs it. That is deliberate.
- Pass bound: 10,000 candidates, oldest first; the rest wait a day.
- The first run of each job is jittered within [0, min(interval, 1 min)) after start.
- No CLI form was added; the ticket does not ask for one.

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T09:10:43Z

PR #109 (branch run/c5y2s-sweeper-gc) was closed unmerged: healthz (PR #107) merged while it was open and conflicted on serve_test's step list. The run does not force-push, so the rebased head went to run/c5y2s-sweeper-gc-v2 as PR #110. After the rebase the collector's hold reads projection.Writer.QuarantinedCount, which healthz added.

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T09:15:28Z

Review 1156 (run e7f5c1c8-7f24-47f1-838b-f6feb8822091, Actions run #625) on head 084c52bcec142bbe871e940ff0be14da5891bd22, base 21fa204c8f10ca65a4c2cc1601581ad34e7fdbcf: two findings, both accepted and fixed in code in c583b46. (1) high, blobgc.go:173: unlinking inside the transaction before commit could leave a row without bytes. Fixed: rename to <hash>.gc in the transaction, undo on a failed commit, remove after commit, and each pass settles leftovers. Tests: TestACommitThatFailsPutsTheBytesBack, TestAnInterruptedRemovalIsSettled. (2) medium, blobgc.go:110: a fixed oldest-10,000 window could starve later candidates. Fixed: keyset paging over (unreferenced_since, hash) until exhausted. Test: TestKeptCandidatesDoNotStarveLaterOnes. This supersedes the 'Pass bound: 10,000 candidates' line of the earlier gaps note.

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T09:18:00Z

Review 1157 (run ea46019e-cff0-4b8a-bca8-ae0d4582bf4c, Actions run #627) on head 9e143311e5c61397888f1d0355ec0db4c614ae43, base 21fa204c8f10ca65a4c2cc1601581ad34e7fdbcf. It marks both review 1156 findings resolved and raises one new medium finding (moderation.sql:96): the draft guard used case-sensitive instr, so an upper-case hash in a draft did not keep the blob. Accepted and fixed in code in c05f5c6: both the avatar and draft guards now compare with lower(), and the test covers upper-case references in both.

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T09:18:04Z

Correction to the previous note: the review 1157 fix is commit e0a8f90, not c05f5c6.

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T09:23:01Z

Review 1158 (run for request ready-review-3feb393) on head 3feb3936ca79b55f1f5f59fe9bf004c905afb867, base 21fa204c8f10ca65a4c2cc1601581ad34e7fdbcf. It marks the review 1157 finding resolved and raises three new findings, all accepted and fixed in code in 12f0f2e. (1) high, blobgc.go:302: a threads/ that is a regular file walked as empty. Fixed: the pass holds unless threads/ is a directory. (2) medium, blobgc.go:165: deletions from earlier pages happened before later pages were scanned. Fixed: two phases, scan every page and then delete, capped at 100,000 collectable a pass. (3) medium, scheduler.go:103: the database was closed under a job that outlasts the bounded Stop. Fixed: Scheduler.Done, and serve leaves the database open (WAL replayed at the next start) while a job is still out; the scan checks cancellation at every frame. This is the third review round on PR #110.

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T09:26:38Z

Review 1159 (run 413ac0d3-06d1-466f-8b64-51bd258518f8, Actions run #631) on head 5a3c9ad8ad1c2daf4dd86bb1362eb270c43a24ab, base 21fa204c8f10ca65a4c2cc1601581ad34e7fdbcf. It marks all three review 1158 findings resolved and raises one new high finding (blobgc.go:302): a symlinked threads/ passed the directory check but walked as empty. Accepted and fixed in code in aea2059: the root is resolved with EvalSymlinks, and any symlink or non-regular entry below it holds the pass. Test: TestSymlinksAreFollowedOrHeld. The next review is round five on PR #110, the brief's limit: if it finds anything at medium or above, the PR stays open for a person.

## Summary

New internal/jobs: a Scheduler (per-job goroutine, jittered first run, recover, bounded Stop) plus two jobs that serve starts after catch-up and stops before the stores close. The hourly session sweep is session.Store.Sweep, the exact complement of LookupSession's liveness test on the store's clock. The daily blob GC deletes a blob only when all of these hold: its refcount has been zero for 7 days (re-checked in the deleting statement), no avatar or draft names it, and no thread log record mentions its hash. An unreadable log, an unknown encoding or a quarantined thread holds the pass. Row delete and unlink share one db.Write. internal/store/blob has the path helper. docs/03 'Blob garbage collection' records the checks and the attach/upload ordering contract for TKT-01M29CXK0.
