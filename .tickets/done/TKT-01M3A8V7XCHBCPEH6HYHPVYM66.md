---
schema: 3
id: TKT-01M3A8V7XCHBCPEH6HYHPVYM66
title: Publish quarantine before an interrupted live sweep returns
type: bug
status: done
status_reason: null
priority: high
due_on: null
labels: []
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-24T17:54:06Z
updated_at: 2026-09-28T18:48:12Z
created_by:
  id: agent:claude-code/phase1-run-415af6aa
  name: ""
updated_by:
  id: agent:claude-code/phase1-run-438e26c1
  name: ""
extensions: {}
---

## Description

Review 468 on PR #67 (TKT-01M29C5Y1S, head 1979894, base 60f582f) raised a high that was deferred by the user's decision, not fixed. PR #67 was merged with it open.

### The finding
`Writer.CatchUp`, run as the live sweep (a batch reporting dropped events), collects corrupt threads in a local map and publishes them to `w.quarantined` only after the whole scan. Meanwhile its quarantine closure drops each thread from the behind set straight away. A sweep whose context is cancelled between those two points returns early. The thread is then neither held nor quarantined, so its endpoints serve a projection longer than its log.

It does not arise at startup. There a cancelled catch-up fails serve before the listener opens.

### Fix
Publish each quarantine as it is found, under `mu`: add to `w.quarantined` in the closure. At the end, replace the set only when the scan completed, so a completed scan still lets a repaired thread out.

Test: cancel the context from inside a Recover after a corrupt thread has been seen, then assert `Quarantined` is true and the thread is not in Lag.

## Implementation plan

The ticket had no plan section and nothing settled in grooming. Its Fix and Test paragraphs are review 468's own remedy and are taken as given.

### Change (internal/events/projection/catchup.go)
- The quarantine closure in `CatchUp` adds the thread to `w.quarantined` under `mu`, in the same critical section as `drop`, so no reader can see a thread that is out of the behind set and not yet quarantined. The local `corrupt` map stays for `Report.Corrupt`.
- At the end, `w.quarantined = corrupt` still replaces the set, but only on the path where the scan completed. An interrupted sweep (ctx cancelled, returned at the loop's `ctx.Err()` check) leaves the published set as it is: the old quarantine plus whatever this sweep found. A thread repaired since the last completed scan leaves quarantine at the next completed scan and not before. That is the conservative direction, because quarantine only withholds reads.
- Update the doc comments on `CatchUp`, `Writer.quarantined` and `Quarantined` to say "last completed catch-up plus anything found since".

### Unchanged, checked
- Startup: serve.go fails on any CatchUp error before the listener opens, so a partial set published at startup is never served.
- `/healthz` (`QuarantinedCount`, PR #107) and blob GC's Hold (PR #110) read the same map under `mu`. Before the fix, an interrupted live sweep could clear a held corrupt thread, and GC would then run and could delete blobs that only the short or missing log named. After the fix both consumers see the thread as soon as the sweep finds it.
- `Retry`'s `quarantine` method and `Reindex`'s reset do not change.

### Tests (catchup_test.go)
- `TestInterruptedSweepKeepsTheQuarantine`: thread A is held (a failing reopen on a torn tail), then cut below its projection. B has a gap and C comes after it. A catch-up cancels its context from inside B's Recover. Assert the error is `context.Canceled`, `Quarantined(A)` is true, A is not in `behind`, and `Lag` is only B's hold.
- Ordering: from inside B's Recover, on another goroutine, `Quarantined(A)` and `QuarantinedCount()` already report A while the sweep is still running.
- `TestInterruptedSweepDoesNotReleaseTheQuarantine`: a thread quarantined by a completed scan, then repaired. An interrupted scan leaves it quarantined, and the next completed scan releases it.
- Mutation checks are recorded in the commit body.

## Notes

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T18:48:04Z

### Consumers and paths checked (2026-09-28)
- Startup (cmd/ketju/serve.go): `proj.CatchUp(ctx, reopen)` failing returns `fail("catching up the projection", err)` before the listener or the jobs start. A partial set published by a cancelled startup catch-up is never served. Unchanged.
- `/healthz` (PR #107) reads `QuarantinedCount` under `mu`. It now reports `degraded` as soon as a sweep finds a thread corrupt, rather than after the sweep ends, and an interrupted sweep no longer hides the thread.
- Blob GC's Hold (PR #110) reads the same count. Before this fix, an interrupted live sweep dropped a held corrupt thread from both the behind set and the quarantine. With the count then at zero, GC could run a pass and delete a blob that only the short or missing log named. The thread now stays counted, so GC holds.
- `Retry`'s `quarantine` method and `Reindex`'s reset are unchanged.

### Decision: an interrupted sweep releases nothing
A thread repaired since the last complete catch-up stays quarantined through an interrupted one and is released at the next complete one. The alternative was to release a thread that the partial scan had seen fine. It lost: the one direction that can serve bad rows is a quarantine released early, while a quarantine released late only withholds reads until the next sweep. The ticket's Fix also says so.

### Mutation checks (all caught)
- No publication in the closure (the old behaviour): both new tests fail.
- Publication only on the interrupted return, not as each thread is found: the mid-sweep ordering check in TestInterruptedSweepKeepsTheQuarantine fails.
- Replacing the set on the interrupted return: TestInterruptedSweepDoesNotReleaseTheQuarantine fails.
- Resetting the set when a sweep starts: TestInterruptedSweepDoesNotReleaseTheQuarantine fails.
- No final replacement: TestLogShorterThanProjectionIsCorruption and TestInterruptedSweepDoesNotReleaseTheQuarantine fail.
- No `drop` in the closure: TestCatchUpQuarantinesAHeldThread and TestInterruptedSweepKeepsTheQuarantine fail.
- Not tested: splitting the publish and the drop into two critical sections. Only a reader running between the two could see the gap, and no hook can place one there.

## Summary

CatchUp's quarantine closure now adds each corrupt thread to w.quarantined under mu, in the same critical section as the drop from the behind set. Only a scan that completes replaces the set, so an interrupted live sweep returns with the thread quarantined, and a repaired thread leaves at the next complete sweep. Covered by TestInterruptedSweepKeepsTheQuarantine, which cancels inside a later thread's Recover and also checks from another goroutine mid-sweep, and by TestInterruptedSweepDoesNotReleaseTheQuarantine. Startup, /healthz and blob GC's hold were checked and need no change. Closes review 468's deferred high from PR #67.
