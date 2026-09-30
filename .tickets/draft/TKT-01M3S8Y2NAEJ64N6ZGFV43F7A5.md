---
schema: 3
id: TKT-01M3S8Y2NAEJ64N6ZGFV43F7A5
title: Stop ApplicationDetail.test.tsx failing in full vitest runs
type: bug
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:test
  - area:client
  - effort:S
assignees: []
milestone: phase-2
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-30T13:44:15Z
updated_at: 2026-09-30T13:44:29Z
created_by:
  id: agent:claude-code/phase2-run-orchestrator
  name: ""
updated_by:
  id: agent:claude-code/phase2-run-orchestrator
  name: ""
extensions: {}
---

## Description

`web/client/src/screens/admin/applications/ApplicationDetail.test.tsx` failed in 2 of 6 full vitest runs on the new-thread-screen lane (TKT-01M3NS16VH, local branch at 6b5dc9a, 2026-09-30). It passes when run alone, and that lane does not touch it. So some state leaks in from other test files, or the test depends on timing that a full parallel run disturbs.

### What to do

Reproduce the failure with the full suite (`npx vitest run`, repeated). Name the failing assertion and the cause, whether that is shared module state, a fake timer, an unawaited promise, or a fetch mock another file leaves behind. Fix the cause, not the symptom: no retries, no larger timeouts, no skipped test. Show the file passing 20 full runs in a row.

Reported by the Phase 2 run orchestrator on 2026-09-30.
