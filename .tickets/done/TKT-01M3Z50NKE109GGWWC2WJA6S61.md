---
schema: 3
id: TKT-01M3Z50NKE109GGWWC2WJA6S61
title: Stop TestRunRetriesAFailedCatchUp cancelling before the catch-up ends
type: bug
status: done
status_reason: null
priority: low
due_on: null
labels:
  - area:search
assignees: []
milestone: phase-4
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-02T20:31:13Z
updated_at: 2026-10-03T02:24:22Z
created_by:
  id: agent:claude-code/phase4-groom-moving-posts
  name: ""
updated_by:
  id: agent:claude-code/phase4-run-z50nke
  name: ""
extensions: {}
---

## Description

`TestRunRetriesAFailedCatchUp` (`internal/events/search/sweep_test.go`) failed in CI run 2469 (PR #371, which changes only docs and tickets) under `go test -race`:

```
--- FAIL: TestRunRetriesAFailedCatchUp (0.30s)
    sweep_test.go:112: Indexing is still true after a catch-up finished
```

### Why it can fail
The test waits until `f.hits("found") == 1`, which becomes true as soon as `x.index` has written the row. It then cancels the context and waits for `Run` to return before reading `f.x.Indexing()`.

In the catch-up (`indexer.go` around line 418), `x.index(ctx, targets)` is followed by `if err := ctx.Err(); err != nil { return err }`, and only after that comes `x.indexing.Store(false)`. So when the cancel lands between the row becoming visible and that check, the catch-up returns early, and the flag stays true. That flag is correct for a catch-up that was cancelled. Under race-detector load, the window is wide enough to hit.

### What to do
Make the test wait for the catch-up to finish before it cancels. For example, wait for the "search catch-up finished" log line, or poll `Indexing()` to false within the same deadline, and only then cancel. Do not change the indexer's ordering: setting the flag false before the rows are written would report a finished index early.

## Acceptance criteria

- [x] TestRunRetriesAFailedCatchUp waits for the catch-up to finish before cancelling, and passes 200 runs under -race

## Implementation plan

### Cause

The description has it right, and the code on main confirms it. In `Indexer.CatchUp` (`internal/events/search/indexer.go`), `x.index(ctx, targets)` writes the rows. A `ctx.Err()` check follows, and only after that does the catch-up store `indexing` false and log "search catch-up finished". The test waited only for the row (`f.hits("found") == 1`) and then cancelled. A cancel that lands between the write and the check makes the catch-up return early, so `Indexing` correctly stays true and the test fails.

### Change

Test only, in `internal/events/search/sweep_test.go`, `TestRunRetriesAFailedCatchUp`:
- wait, within the same 10 s deadline, until the row is found and `Indexing()` is false, and only then cancel. The fatal message names both;
- keep the `Indexing()` check after `Run` returns, reworded, because it still catches a flag set again;
- also assert the "search catch-up finished" log line, which `CatchUp` writes before it returns.

The indexer's ordering does not change. Clearing the flag before the rows are written would report a finished index early.

### Verification

- 200 runs under `-race`.
- Local only: a 50 ms sleep added after `x.index` widens the window. With it, the old test failed 49 of 50 runs, and the new one passes 200 of 200.

## Notes

**agent:claude-code/phase4-run-orchestrator** at 2026-10-03T00:30:42Z

Placed in Phase 4 by the run (orchestrator, 2026-10-02; the maintainer may move it back out), for the same reason as TKT-01M3ZE2211: it fails CI on unrelated changes (PR #371, a docs-only change), and the exit check TKT-01M3YMG0NX needs reliable green CI. docs/14's run placed TKT-01M3TSZHSW the same way.

**agent:claude-code/phase4-run-z50nke** at 2026-10-03T02:24:14Z

Reproduced and fixed. CI did not fail here: 0 of 200 runs failed under -race on this machine, even beside other load. A local 50 ms sleep after x.index in CatchUp (not committed) widens the gap between the row being written and Indexing being cleared. With it, the old test failed 49 of 50 runs with CI's message ('Indexing is still true after a catch-up finished'). The new test, which waits for the row and for Indexing to go false before it cancels, passed 200 of 200 with the sleep and 200 of 200 without it, all under -race.

## Summary

TestRunRetriesAFailedCatchUp now waits, within its 10 s deadline, until the catch-up's row is found and Indexing() is false, and only then cancels Run. It also checks the 'search catch-up finished' log line. The indexer is unchanged. The flaw was in the test: cancelling between the row write and the flag clear correctly leaves the flag set. With a 50 ms sleep widening that gap locally, the old test failed 49 of 50 runs and the new one passed 200 of 200. Without the sleep, it passed 200 of 200 under -race.
