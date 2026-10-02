---
schema: 3
id: TKT-01M3YME9DMH3PMJGD8A4A7ZMNN
title: Keep API handlers off the stores, and test the layering
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:api
assignees: []
milestone: phase-4
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-02T15:41:33Z
updated_at: 2026-10-02T15:41:33Z
created_by:
  id: agent:claude-code/phase4-run-orchestrator
  name: ""
updated_by:
  id: agent:claude-code/phase4-run-orchestrator
  name: ""
extensions: {}
---

## Description

TKT-01M29BKBMC (JSON API and service layer) says: "No handler in internal/web/api reads from a store package directly". Three handlers in `internal/web/api/settings.go` call `session.Store` themselves (lines 83, 112 and 136: `ListForUser`, `RevokeOwn`, `RevokeUser`), and those run SQL. `handle.go:111` passes `u.DB` into a service function. No test fences the package. The only layering test of this kind is `internal/dto/layering_test.go`.

Found on 2026-10-02 by the check of that epic's criterion 1.

### What to do
- Move the session-list and revoke calls behind a service, as the other handlers are.
- Add a layering test that fails when `internal/web/api` imports a store or session-store package, other than the parsing and enum packages it needs (`threadid`, `views`).
