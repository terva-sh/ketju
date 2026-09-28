---
schema: 3
id: TKT-01M3MAS0BQ3NGQZBPVZHZ9PYKG
title: Move TestHealthzP99's latency bound out of the race run
type: bug
status: ready
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
claim: null
archive: null
created_at: 2026-09-28T15:40:14Z
updated_at: 2026-09-28T15:40:24Z
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
