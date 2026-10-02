---
schema: 3
id: TKT-01M3YME9KGJ1XW7RHCRFZ621A0
title: Answer method-not-allowed for a wrong verb on an API route
type: bug
status: draft
status_reason: null
priority: low
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

The problem catalogue (docs/07) has `method-not-allowed`, "Right route, wrong verb". A wrong verb on a real `/api/v1` route answers `not-found` instead. `render_test.go:250` and `users_test.go:100` accept either answer, so nothing pins the behaviour.

docs/07 around line 681 also still shows the thread page's ETag without the digest that line 701 and the code (`view.go:515-522`) add.

Both found on 2026-10-02 by the check of TKT-01M29BKBMC.

### What to do
- Answer `method-not-allowed` with an `Allow` header for a known route and an unknown verb, and make the two tests require it.
- Correct the docs/07 example.
