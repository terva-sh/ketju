---
schema: 3
id: TKT-01M3SK2HFEFAAT4K5J9V3HD3XS
title: Restore the registration form after a failed admin-forms e2e test
type: bug
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:test
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
created_at: 2026-09-30T16:41:27Z
updated_at: 2026-09-30T16:41:45Z
created_by:
  id: agent:claude-code/phase2-run-orchestrator
  name: ""
updated_by:
  id: agent:claude-code/phase2-run-orchestrator
  name: ""
extensions: {}
---

## Description

If a test in `web/client/e2e/admin-forms.spec.ts` fails, the registration form it edited is left changed on the e2e server. Later tests and specs on the same server then see that form and fail in turn, which hides the real first failure. The TKT-01M3RZXGBA lane (PR #232) saw this both in main's load run and in a deliberate mutation run. The groomed plan for that ticket kept it out of scope.

### What to do

Restore the form whatever the outcome. Use an `afterAll` that puts back the version the spec started from through the admin API, or seed a separate form the spec owns. Prove it with a run where one admin-forms test is broken on purpose: only that test should fail, not the specs after it.

Reported by the Phase 2 run orchestrator on 2026-09-30.
