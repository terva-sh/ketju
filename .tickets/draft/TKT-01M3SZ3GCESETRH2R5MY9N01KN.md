---
schema: 3
id: TKT-01M3SZ3GCESETRH2R5MY9N01KN
title: Stop admin-applications.spec.ts timing out under CI load
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
created_at: 2026-09-30T20:11:42Z
updated_at: 2026-09-30T20:11:58Z
created_by:
  id: agent:claude-code/phase2-run-orchestrator
  name: ""
updated_by:
  id: agent:claude-code/phase2-run-orchestrator
  name: ""
extensions: {}
---

## Description

Found by the TKT-01M3QS0TMV lane (PR #245) on 2026-09-30: main's push run #1592 on 46c1081 failed in the Playwright step on the desktop project. `web/client/e2e/admin-applications.spec.ts:50` timed out after 5 s waiting for `[data-application-link="2"]` to appear, and the Playwright step took 9.4 min, against its usual length.

46c1081 changed only docs and the ticket store (PR #242), PR run #1595 passed on the next head, and the test passed in the lane's two local full runs, so this looks like a flake under runner load. It is the only failure seen: the other recent push runs on main were cancelled by later pushes rather than finishing.

### What to do
Reproduce under load (for example, the full suite with more workers than the runner has, or with CPU throttled), then name what the test waits on. The likely candidates are a 5 s default expect timeout against an admin list that first waits on the lazy admin chunk, or a row that appears only after a refetch. Fix the cause. No retries, no longer timeouts, no skips, the same rule as TKT-01M3S8Y2NA.
