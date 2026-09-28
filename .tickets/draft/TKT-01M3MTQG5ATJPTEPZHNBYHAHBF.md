---
schema: 3
id: TKT-01M3MTQG5ATJPTEPZHNBYHAHBF
title: Stop Playwright checks timing out under CI runner contention
type: bug
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:test
  - area:client
assignees: []
milestone: phase-2
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-28T20:19:02Z
updated_at: 2026-09-28T22:29:24Z
created_by:
  id: agent:claude-code/phase1-run-438e26c1
  name: ""
updated_by:
  id: agent:claude-code/phase1-run-438e26c1
  name: ""
extensions: {}
---

## Description

Twice during the hardening sweep on 2026-09-28, a Playwright check timed out on a PR whose code it does not touch, and passed on rerun. Both times another lane's CI run was on the same runner:
- run 728 (PR #125): the phone forum-index axe scan;
- run 751 (PR #128): the desktop thread-page a11y check, waiting for the reply expander.

Each costs a rerun, and under the docs/12 gate a red run blocks a merge. Two occurrences are not yet a pattern worth changing timeouts for blind.

### To do
- Collect the failing step logs of both runs, and any later ones, and name the waits that timed out.
- Decide between: waiting on the condition the test needs instead of a fixed timeout; raising the per-test timeout under CI only; and serialising Playwright on the shared runner.
- Don't loosen an assertion to make it pass.
