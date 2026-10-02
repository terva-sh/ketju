---
schema: 3
id: TKT-01M3YFYYGVFJNW9XHX521C4K5F
title: Give CI's race test step a 20-minute package time limit
type: chore
status: done
status_reason: null
priority: high
due_on: null
labels:
  - area:ci
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-02T14:23:16Z
updated_at: 2026-10-02T15:18:09Z
created_by:
  id: agent:claude-code/phase3-run-orchestrator
  name: ""
updated_by:
  id: agent:claude-code/phase3-run-orchestrator
  name: ""
extensions: {}
---

## Description

CI's `go test (race)` step runs `go test -race ./...` under Go's default per-package limit of 10 minutes. On runner build-0, which is short of CPU (TKT-01M3VDYENN), `cmd/ketju` alone runs close to that limit:
- Run 2345 (PR #349, which changed no Go code) failed when `cmd/ketju` hit the 10-minute limit. The step ran 15m16s.
- Run 2349, the same code on the same runner, took 8m12s, with `cmd/ketju` at 304.750 s.
- Run 2359 (PR #351, which added the `TestPhase3Exit*` tests to `cmd/ketju`) took 10m11s for the step.

A PR can fail on how busy build-0 is rather than on its own change, and the package only grows. The Phase 3 run listed this as an open question for the maintainer (TKT-01M3V3BVPY's summary). docs/12 "Changing CI" lets a run add CI steps but not change one.

### What to do
Give the race step an explicit `-timeout 20m`. A real deadlock still fails the step, 10 minutes later than now. Leave the other `go test` steps as they are: each runs a narrow subset, far below the limit.

## Acceptance criteria

- [x] The go test (race) step in .forgejo/workflows/ci.yml runs with -timeout 20m, with a comment saying why
- [ ] The PR's CI run passes and its step log shows the race step ran with the new flag

## Implementation plan

Change the race step's command to go test -race -timeout 20m ./... and extend its comment with the reason and the runs (2345, 2349, 2359). No other step changes. Verify on the PR's CI step log.

## Notes

**agent:claude-code/phase3-run-orchestrator** at 2026-10-02T14:23:27Z

Maintainer request, 2026-10-02. After the Phase 3 run closed, the maintainer was given its open items in order, the first being "Go race-test time limit … Tell me if you want it". They answered: "Let's start tackling this in order". That request is this ticket's promotion, and it authorizes this one change to an existing CI step. docs/12 "Changing CI" would not allow the change to a run on its own authority.

The 20 minutes is twice Go's default, and run 2345's slowest step took 15m16s. Two alternatives lost:
- **A bare `-timeout 0`.** It would let a real deadlock hang the job until Forgejo's own job limit stops it.
- **Splitting `cmd/ketju`.** That is a code change to fix a runner capacity problem.

**agent:claude-code/phase3-run-orchestrator** at 2026-10-02T15:18:09Z

Merged in #354 as 1495275d. Review clean on that head. The first CI run (2373) died with build-1, which went offline at 14:29:34 UTC for the maintainer's memory upgrade. The re-run 2379 ran on build-0 and passed: the race step took 11m18s, and cmd/ketju took 490.133 s. Criterion 2 is unticked. Forgejo's step log prints test output but does not echo the command, so the log cannot show the flag. The run used the PR head's ci.yml, which carries it, and cmd/ketju finished inside the old limit anyway. A later run where cmd/ketju takes over 600 s and still passes would prove the flag; none has yet.

## Summary

CI's go test (race) step now runs with -timeout 20m (#354, 1495275d), and its comment says why: on a busy build-0, cmd/ketju under -race runs close to Go's 10-minute default, and run 2345 failed at that limit. Criterion 2 is unticked because the step log does not echo the command.
