---
schema: 3
id: TKT-01M393S6P7NK3PVXM76TVHJ16M
title: Order the group-commit fsync test so its first append is in flight
type: bug
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area:storage
  - area:test
  - effort:S
assignees: []
milestone: null
parent: TKT-01M29BKBKP8C66GXXQK4HPEZ35
origin: null
dependencies: []
blocks_on: none
references:
  - ref: pr:124
    path: null
claim: null
archive: null
created_at: 2026-09-24T07:06:22Z
updated_at: 2026-09-28T18:28:18Z
created_by:
  id: agent:claude-code/phase1-run-415af6aa
  name: ""
updated_by:
  id: agent:claude-code/phase1-run-438e26c1
  name: ""
extensions: {}
---

## Description

`internal/store/threadlog/actor_test.go`, `TestQueuedAppendsShareOneFsync`, still fails intermittently after 858d6fa ("Make the group-commit fsync test deterministic"). It failed once in a full `go test -race ./...` run on 2026-09-24. Under `go test -race -count=200 -cpu 1,2,12 -run TestQueuedAppendsShareOneFsync ./internal/store/threadlog/` it failed 10 times in 600:

    actor_test.go:242: 17 appends cost 1 log fsyncs, want 2 (the one in flight, then the batch)

### Cause

The test spawns the first append and the 16 queued appends together, then calls `synctest.Wait()` once. It assumes the first append is already inside the gated `Sync` when the others arrive. Nothing orders that. The 16 senders can park on the actor's channel before the actor has reached `Sync` for the first record. The actor's drain then takes all 17 into one commit, which gives one fsync. That is correct group-commit behaviour; the test's premise is what fails.

### Fix

Start the first append alone, call `synctest.Wait()` so it is durably blocked on the gate inside `Sync`, then spawn the 16 and call `synctest.Wait()` again before closing the gate. The count of 2 then holds by construction. Verify with the `-count=200 -cpu 1,2,12` command above, which should report no failures.

This was found during TKT-01M29BW9Q1 and left out of that PR, because a run PR stays inside its ticket's scope (docs/12). It can fail CI on any PR at about 1–2%.

## Implementation plan

Test-only change in `internal/store/threadlog/actor_test.go`. No production code changes: the actor's `run` loop and `commit` keep every write and fsync exactly as they are, and no hook is added.

1. In `TestQueuedAppendsShareOneFsync`, start the first append alone and call `synctest.Wait()`. When it returns, the actor goroutine is durably blocked on the bubble's gate channel inside `fakeLog.Sync`, and the first caller is blocked on its reply channel. The first commit therefore holds exactly one record, because the drain ran before `Sync` and found the queue empty.
2. Then spawn the 16 queued appends and call `synctest.Wait()` again. It returns only when all 16 are parked sending on the actor's unbuffered `reqs` channel. A goroutine waiting on `a.mu` is not durably blocked, so `Wait` does not return early.
3. Close the gate. The actor finishes commit 1, receives one queued request, and drains the other 15, which gives commit 2. The count of 2 holds by construction, and the assertion stays exact.
4. Update the comment above the test, which currently claims a single `Wait` makes the order deterministic.

Verification: before the fix, `go test -race -count=200 -cpu 1,2,12 -run TestQueuedAppendsShareOneFsync ./internal/store/threadlog/` failed 12 times in 600. After the fix it must report no failures, and so must the `-count=200` command from the prompt. Mutation checks go in the commit body.

## Notes

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T12:22:19Z

Seen again in CI run 236 (PR #62, head 9895ef9, 2026-09-24): TestQueuedAppendsShareOneFsync failed under -race in internal/store/threadlog. The PR changed nothing in threadlog; the next push reran CI.

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T15:14:09Z

Recurred in CI: run 277 (PR #66, head 3aa0366), go test (race), with the same message: actor_test.go:242: 17 appends cost 1 log fsyncs, want 2. That is the second time it has failed a run PR in CI. Still a draft; not promoted by the run.

## Summary

Fixed in `internal/store/threadlog/actor_test.go` only. `TestQueuedAppendsShareOneFsync` now reaches its state in two steps. It starts the first append alone and calls `synctest.Wait()`, which returns once the actor is blocked on the gate inside `Sync` with a batch of one. Then it spawns the 16 and calls `synctest.Wait()` again, which returns once all 16 are parked on the unbuffered `reqs` channel. Only then does it close the gate. The exact assertion of 2 fsyncs is unchanged. No production code changed: no fsync was removed or reordered, and no hook was added.

Evidence, `go test -race -count=200 -cpu 1,2,12 -run TestQueuedAppendsShareOneFsync ./internal/store/threadlog/`: 12 failures in 600 before the fix, 0 in 600 after. At `-count=2000 -cpu 1,2,12` it gave 0 in 6000, and at `-count=200` 0 in 200.

Mutations, all caught:
- dropping the first Wait gave 8 failures in 600, the original race;
- dropping the second Wait gave 194 failures in 600;
- disabling the SyncGroup drain in `actor.go` gave 17 fsyncs against the 2 wanted;
- skipping the log fsync in `commit` gave 0 fsyncs, and the write/sync parity check also failed.
