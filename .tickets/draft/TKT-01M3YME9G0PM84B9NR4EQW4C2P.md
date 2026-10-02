---
schema: 3
id: TKT-01M3YME9G0PM84B9NR4EQW4C2P
title: Golden-test what every API endpoint actually serves
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

TKT-01M29BKBMC says: "Golden JSON tests cover every endpoint in docs/07 and fail on any shape change". `internal/dto/golden_test.go` covers every DTO type, but builds them from hand-written fixtures. Goldens of what the server actually serves exist only for the thread page, edit, lock and notifications. No test maps the route table (`api.go`, plus about 50 admin routes) to a golden. So a handler that changes what it serves fails no test.

Found on 2026-10-02 by the check of that epic's criterion 2.

### What to do
Add a test that walks the registered API routes and fails for any route without a served-response golden. Then fill the goldens in.
