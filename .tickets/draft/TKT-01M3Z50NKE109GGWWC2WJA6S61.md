---
schema: 3
id: TKT-01M3Z50NKE109GGWWC2WJA6S61
title: Stop TestRunRetriesAFailedCatchUp cancelling before the catch-up ends
type: bug
status: draft
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
updated_at: 2026-10-03T00:30:42Z
created_by:
  id: agent:claude-code/phase4-groom-moving-posts
  name: ""
updated_by:
  id: agent:claude-code/phase4-run-orchestrator
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

- [ ] TestRunRetriesAFailedCatchUp waits for the catch-up to finish before cancelling, and passes 200 runs under -race

## Notes

**agent:claude-code/phase4-run-orchestrator** at 2026-10-03T00:30:42Z

Placed in Phase 4 by the run (orchestrator, 2026-10-02; the maintainer may move it back out), for the same reason as TKT-01M3ZE2211: it fails CI on unrelated changes (PR #371, a docs-only change), and the exit check TKT-01M3YMG0NX needs reliable green CI. docs/14's run placed TKT-01M3TSZHSW the same way.
