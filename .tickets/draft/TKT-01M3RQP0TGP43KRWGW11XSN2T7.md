---
schema: 3
id: TKT-01M3RQP0TGP43KRWGW11XSN2T7
title: Let forum moderators by grant lock, pin, move and set slow mode
type: task
status: draft
status_reason: null
priority: high
due_on: null
labels:
  - area:moderation
  - effort:S
assignees: []
milestone: phase-2
parent: TKT-01M29BN36JH4M1W5819D06PGPP
origin: null
dependencies: []
blocks_on: none
references:
  - ref: design:moderation
    path: docs/08-moderation.md
claim: null
archive: null
created_at: 2026-09-30T08:42:45Z
updated_at: 2026-09-30T08:42:52Z
created_by:
  id: agent:claude-code/phase2-run-orchestrator
  name: ""
updated_by:
  id: agent:claude-code/phase2-run-orchestrator
  name: ""
extensions: {}
---

## Description

The maintainer decided on 2026-09-30 that thread moderation follows the forum grant, not the role (docs/08, "Decided 2026-09-30"). A member who holds `moderate` in a forum through a group already works that forum's queue (`moderatesGate`, TKT-01M29CRJ4E) and hides and restores posts. The role-based `staffGate` still refuses them these routes:

- `POST /api/v1/admin/threads/{id}/lock`, `/unlock`, `/pin` and `/unpin` (`internal/web/api/moderate.go`, TKT-01M29CKRGS)
- `POST /api/v1/admin/threads/{id}/move` (TKT-01M29CKRH9)
- `PUT /api/v1/admin/threads/{id}/slow-mode` (`internal/web/api/slowmode.go`, TKT-01M29CRJ50)

Each service already requires `moderate` in the thread's forum, and move requires it in both forums. So the change is mostly the route gate, plus the docs/07 wording ("staff's by role") and tests.

Admin screens and account actions keep their role gates: users, applications, suspension, and the audit log.

This blocks the thread-screen moderator controls. Those controls must be offered to exactly the viewers the server allows, which is `can_moderate` on the thread page.

Filed by the Phase 2 run orchestrator on 2026-09-30.
