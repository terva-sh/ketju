---
schema: 3
id: TKT-01M3MAS0BQ3NGQZBPVZHZ9PYKG
title: Move TestHealthzP99's latency bound out of the race run
type: bug
status: in-progress
status_reason: null
priority: normal
due_on: null
labels:
  - area:test
  - effort:S
assignees: []
milestone: phase-1
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim:
  actor: agent:claude-code/phase1-run-438e26c1
  branch: run/0bq-healthz-p99-no-race
  worktree: /home/sothr/workspace/git.local.sothr.com/terva-sh/ketju/.claude/worktrees/agent-a1c6f82128bb092be
  commit: 2b1c6205fe1ea763bc9e1fc01edd5c2086fdddc1
  session: null
  claimed_at: 2026-09-28T16:09:05Z
  expires_at: null
archive: null
created_at: 2026-09-28T15:40:14Z
updated_at: 2026-09-28T16:09:16Z
created_by:
  id: agent:claude-code/phase1-run-438e26c1
  name: ""
updated_by:
  id: agent:claude-code/phase1-run-438e26c1
  name: ""
extensions: {}
---

## Description

`TestHealthzP99` (internal/web/health, PR #107) asserts a wall-clock p99 under 5 ms for 1000 in-process /healthz requests, taking the best of three runs. It runs in CI's `go test (race)` step. On 2026-09-28, PR #117, which changes only tickets and docs, failed it in run 678, at best 7.1 ms with runs of 7.1, 10.9 and 13.2 ms. That step took 5m50s against the usual ~1m20s, so the runner was loaded. A timing assertion under `-race` on a shared runner measures the runner, not the handler.

### Fix
Keep the criterion ("p99 under 5 ms") measured where it means something:
- move the timing assertion out of the race run: a `//go:build !race` file, or a skip when the race detector is on;
- or run it in CI's existing no-race step beside the projection catch-up bound.

Under `-race`, keep a check that /healthz still answers with one query per request, which the ticket's other tests already hold, without a latency bound. `BenchmarkHealthz` stays.

## Acceptance criteria

- [ ] The p99 bound is asserted only without the race detector, in a CI step that runs
- [ ] go test -race no longer measures healthz latency

## Implementation plan

Follow the precedent of internal/events/projection/catchup_bound_test.go.

1. Move TestHealthzP99 and its helper measureP99 out of
   internal/web/health/health_test.go into a new
   internal/web/health/p99_test.go behind `//go:build !race`. Go sets the
   `race` build tag under -race, so `go test -race ./...` no longer compiles
   the latency assertion at all. The 5 ms bound and the best-of-three stay
   as they are.
2. TestHealthzOneStatementPerRequest and BenchmarkHealthz stay in
   health_test.go, so the race run still holds one statement per request.
3. No CI step runs internal/web/health without -race today. Extend the
   existing no-race step "projection catch-up bound over ten thousand
   threads (no -race)" in .forgejo/workflows/ci.yml: add ./internal/web/health/
   to its package list and widen -run to
   '^(TestCatchUpTenThousandUnderBound|TestHealthzP99)$'. -run applies per
   package, so each package runs only its own bound test. The step keeps its
   name (the required check is matched on the job, but renaming anything is
   out of bounds for this run), its -count=1 and -v, so the measured p99
   appears in the step log. No new step or job, no trigger or permission
   change (docs/12 "Changing CI").
4. Verify locally: `go test -race ./internal/web/health/ -run P99 -v`
   reports no test to run; `go test ./internal/web/health/ -run
   '^TestHealthzP99$' -v` runs it; the widened CI command runs both.
