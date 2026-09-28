---
schema: 3
id: TKT-01M393S6P7NK3PVXM76TVHJ16M
title: Order the group-commit fsync test so its first append is in flight
type: bug
status: ready
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
references: []
claim: null
archive: null
created_at: 2026-09-24T07:06:22Z
updated_at: 2026-09-28T18:06:55Z
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

## Notes

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T12:22:19Z

Seen again in CI run 236 (PR #62, head 9895ef9, 2026-09-24): TestQueuedAppendsShareOneFsync failed under -race in internal/store/threadlog. The PR changed nothing in threadlog; the next push reran CI.

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T15:14:09Z

Recurred in CI: run 277 (PR #66, head 3aa0366), go test (race), with the same message: actor_test.go:242: 17 appends cost 1 log fsyncs, want 2. That is the second time it has failed a run PR in CI. Still a draft; not promoted by the run.
