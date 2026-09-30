---
schema: 3
id: TKT-01M3R5P3CNPZ6QWHZK46ECSJKR
title: Keep the Go race tests inside their timeout under CI runner load
type: chore
status: draft
status_reason: null
priority: normal
due_on: null
labels: []
assignees: []
milestone: phase-2
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-30T03:28:14Z
updated_at: 2026-09-30T03:28:17Z
created_by:
  id: agent:claude-code/phase2-run-orchestrator
  name: ""
updated_by:
  id: agent:claude-code/phase2-run-orchestrator
  name: ""
extensions: {}
---

## Description

CI run 1240 (PR #193, TKT-01M29CKRH9) failed because `go test` in `internal/perm` hit Go's default 10-minute package timeout while its test fixture was still running migrations. The step took 19 minutes. That PR did not touch `internal/perm`; the retrigger (run 1257) passed every step. The cause was runner contention while several lanes' CI runs overlapped.

TKT-01M3MTQG5A covers the same failure mode for Playwright only. Nothing covers the Go suite.

### What to decide

Either:
- give the race step an explicit `-timeout` sized for a loaded runner; or
- make the `internal/perm` fixture cheaper, for example by migrating one template database and copying it per test.

Read the fixture and time it on an idle runner first, because a longer timeout alone only moves the failure.

Reported by the rename/move lane on 2026-09-30.
