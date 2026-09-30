---
schema: 3
id: TKT-01M3RZXGBAN5GRYMZ7HA3A7VQG
title: Bound the axe scans in the admin forms and agents Playwright tests
type: chore
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:test
  - area:client
assignees: []
milestone: phase-2
parent: TKT-01M29BN367F3YQZESM63VN26GP
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-30T11:06:39Z
updated_at: 2026-09-30T11:06:39Z
created_by:
  id: agent:claude-code/phase2-run-mtqg5a
  name: ""
updated_by:
  id: agent:claude-code/phase2-run-mtqg5a
  name: ""
extensions: {}
---

## Description

The a11y timeouts of runs 728, 751 and 950 came from four axe scans sharing one 30 s test budget while another CI run loaded the runner (TKT-01M3MTQG5A, Stop Playwright checks timing out under CI runner contention). That ticket split `a11y.spec.ts` into one scan per test and left the other specs out of scope, because none of them has timed out.

Two admin specs carry more scans in one test than a11y.spec.ts ever did:
- `admin-forms.spec.ts:35`, "an admin edits the registration form and saves a new version": eight axe scans (four states, two themes). On green CI runs 1396, 1403 and 1407 it took 9.5-14.7s.
- `admin-agents.spec.ts:45`, "an admin provisions an agent and manages its token and limits": ten axe scans (five states, two themes). It took 7.1-12.1s on the same runs.

The a11y thread page took 5-9s on green runs and still reached 30 s under load. These two tests are already slower than that.

### To do
- Watch for a timeout in either test. If one comes, or before one does, bound the scans the same way TKT-01M3MTQG5A did. The flow is stateful, so the split cannot be one test per scan. Options are `test.step` with a per-scan budget, or a serial `describe` whose tests share one page.
- Keep the rule tags, the zero-violation assertions and every scanned state.
