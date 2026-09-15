---
schema: 3
id: TKT-01M2JZB012DANZM5QPHAGZXQ38
title: Make the group-commit fsync test deterministic with synctest
type: bug
status: done
status_reason: null
priority: high
due_on: null
labels:
  - area:storage
  - effort:S
assignees: []
milestone: phase-1
parent: TKT-01M29BKBKP8C66GXXQK4HPEZ35
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-15T16:45:24Z
updated_at: 2026-09-15T16:45:50Z
created_by:
  id: agent:claude-code/events
  name: ""
updated_by:
  id: agent:claude-code/events
  name: ""
extensions: {}
---

## Description

`TestQueuedAppendsShareOneFsync` claimed to form its batch deterministically and did not. It spawned seventeen goroutines and closed the gate immediately, without any way to know the other sixteen had reached their sends. When they had not, each append committed alone and the test failed with seventeen fsyncs against a bound of sixteen.

It was found while adding `internal/events`: the new package's tests raised the scheduler pressure in a full `go test ./...` run enough to lose the race about two runs in five. The bound was previously never crossed on this machine, so the test had been passing for the wrong reason since it was written.

A batching test that passes only when the scheduler cooperates is worse than no test. It reads as evidence that group commit works, and the one time it reports otherwise the reader has no way to tell a broken actor from a busy machine.

`testing/synctest` supplies what the test was missing. `synctest.Wait` returns when every other goroutine in the bubble is durably blocked, which here is exactly the state the batch is meant to collapse: one goroutine inside the gated `Sync`, sixteen parked on the actor's unbuffered channel. Reaching that state on purpose also lets the fsync count be asserted exactly — two, the one already in flight plus the batch — instead of bounded by a number that only a total failure exceeds.

## Acceptance criteria

- [x] The batch is formed by a mechanism, not by winning a race
- [x] The fsync count is asserted exactly rather than bounded
- [x] The test passes under a loaded machine and repeated runs

## Notes

**agent:claude-code/events** at 2026-09-15T16:45:44Z

Verified before and after. On the pre-fix test, a full `go test ./...` with internal/events present failed 2 of 5 runs, always at the maximum possible count (17 fsyncs for 17 appends), which is the signature of no batching at all rather than partial batching. After the fix: 10 consecutive runs of the two tests, a full -race suite, and six concurrent copies of the test racing each other on a 32-thread machine, all green.

The exact count is the part worth keeping. The old bound (`syncs > queued`) only fires when batching fails completely; an actor that collapsed 17 appends into 9 commits would have passed it. The new assertion is 2, and 2 is derivable: the first append is already inside its fsync when the rest arrive, so no scheme can pull them into a sync that has started, and the sixteen that queue behind it share the second commit.

Split the SyncAlways control into its own test (TestSyncAlwaysFsyncsEveryAppend). It was never part of the racy half and does not belong inside a synctest bubble.

## Summary

TestQueuedAppendsShareOneFsync now forms its batch with testing/synctest instead of a scheduler race, and asserts exactly 2 fsyncs for 17 appends rather than a bound only total failure could cross. The SyncAlways control moved to its own test. No production code changed; the actor was always correct and the test was not.
