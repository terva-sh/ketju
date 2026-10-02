---
schema: 3
id: TKT-01M3YT04ST85SM0PYWVV4MC06T
title: Let staff reset a member's display name and bio
type: task
status: draft
status_reason: null
priority: low
due_on: null
labels:
  - area:api
  - area:client
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-02T17:18:41Z
updated_at: 2026-10-02T17:18:41Z
created_by:
  id: agent:claude-code/phase4-groom-client-account
  name: ""
updated_by:
  id: agent:claude-code/phase4-groom-client-account
  name: ""
extensions: {}
---

## Description

Found in Phase 4 grooming of TKT-01M3V6XVVG (Let a member edit their display name, bio and avatar) on 2026-10-02, against main at ed82f15.

Once a member can edit their display name and bio (docs/05 "Display name and bio", decided by the run 2026-10-02), they can write an abusive or impersonating one. Staff have no way to clear it. An admin can rename a handle (`POST /admin/users/{handle}/rename`), and anonymizing clears the display name and bio, but anonymizing is irreversible and far too strong for this.

### What to do
Decide whether moderators or only admins may reset a member's display name and bio, and whether the reset is audited (it would be a moderation action, unlike the member's own edit). Then add the route and a control on the admin user detail. The text itself should not go into `audit_log`, which is append-only and out of reach of a hard redaction (docs/04).

No Phase 4 deliverable needs this, so it has no milestone (docs/15 "Scope").
