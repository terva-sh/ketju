---
schema: 3
id: TKT-01M2HTD8SG0TXAKG57Z973FA20
title: Test the thread actor's close path against concurrent appends
type: task
status: done
status_reason: Merged as 560b01c on main; CI run 25 green on all twelve steps.
priority: high
due_on: null
labels:
  - area:storage
  - area:test
  - effort:S
assignees: []
milestone: phase-0
parent: TKT-01M29BKBKP8C66GXXQK4HPEZ35
origin: null
dependencies: []
blocks_on: none
references:
  - ref: commit:560b01c
    path: null
claim: null
archive: null
created_at: 2026-09-15T06:00:02Z
updated_at: 2026-09-15T06:01:13Z
created_by:
  id: agent:claude-code/storage
  name: ""
updated_by:
  id: agent:claude-code/storage
  name: ""
extensions: {}
---

## Description

`internal/store/threadlog/actor_test.go`: prove that `Close` is safe against
appends still in flight, and that the proof does not depend on the scheduler.

The actor originally signalled shutdown by closing its request channel. That
panics — a send on a closed channel panics rather than blocking, and selecting
on a quit channel alongside the send does not save you, because the send case
goes ready the instant the channel closes. The fix landed with the concurrent
read path in 8bd18b7: the request channel now stays open for the actor's whole
life, and admission is gated by a `closed` flag and an inflight `WaitGroup`.

What did not land was anything holding that fix in place. The only close test
appended, closed, and appended again, all on one goroutine. Measured against
the pre-fix actor restored from 5294ac3, that test passes 28 runs in 60: with
both the closed request channel and the closed `done` channel ready, Go picks
a select case at random. A 50/50 test of a deterministic bug is worse than no
test, because a passing run reads as evidence. It is why the panic survived
review and surfaced only when an unrelated mutex in the index shifted the
schedule.

## Acceptance criteria

- [x] Close landing at many different points among concurrent appenders neither panics nor hangs, under the race detector
- [x] Every append returns either an ordinal or ErrActorClosed, and never a third outcome
- [x] The appends told they succeeded are in the log and are the only things in it, so no caller is acknowledged then dropped, and no record is written for a caller told it was refused
- [x] Acknowledged ordinals stay gapless, so a refusal racing a commit does not consume a number
- [x] Concurrent Close calls agree on one answer and do not close the quit channel twice
- [x] The test fails against the pre-fix actor, demonstrated rather than asserted

## Notes

**agent:claude-code/storage** at 2026-09-15T06:01:13Z

draft to done: Merged as 560b01c on main; CI run 25 green on all twelve steps.

## Summary

Two tests in internal/store/threadlog/actor_test.go. TestCloseRacesAppendsWithoutLosingOrPanicking sweeps Close across 25 landing points among 24 concurrent appenders, checking each caller's answer against what the log holds; TestConcurrentClosesAllReturnTheSameAnswer races 16 closers. Validated by restoring the pre-fix actor from 5294ac3: the old test passed 28 of 60 runs, the new one failed 60 of 60, with and without -race. Merged as 560b01c.
