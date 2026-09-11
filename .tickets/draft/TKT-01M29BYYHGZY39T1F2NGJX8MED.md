---
schema: 3
id: TKT-01M29BYYHGZY39T1F2NGJX8MED
title: Add the forums, groups, and permissions schema
type: task
status: draft
status_reason: null
priority: urgent
due_on: null
labels:
  - area:db
  - effort:M
assignees: []
milestone: phase-1
parent: TKT-01M29BKBKV6HH8W0DN6QXQNZ0R
origin: null
dependencies:
  - TKT-01M29BW9Q17KEBMHJ91G6QHW36
blocks_on: none
references:
  - ref: design:data-model-perms
    path: docs/04-data-model.md
claim: null
archive: null
created_at: 2026-09-11T23:13:37Z
updated_at: 2026-09-11T23:13:37Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:terva/mieli
  name: Mieli
extensions: {}
---

## Description

`forums` with at most one level of nesting and a visibility of public, members, or restricted. `groups` and `group_members` with the system groups everyone, members, moderators, and admins seeded. `permissions` as (forum_id nullable, group_id, permission) rows over read, create_thread, reply, upload, and moderate.

Deliberately phpBB-shaped and small. A nullable forum_id means a global row, and evaluation is the union of the viewer's groups for that forum falling back to global. Group membership carries a source so an IdP-mapped membership can be re-evaluated at login without clobbering one an admin set by hand.

## Acceptance criteria

- [ ] The four system groups are seeded by migration and cannot be deleted
- [ ] Forum nesting deeper than one level is refused at write time
- [ ] A group_members row records whether it came from an admin or from an IdP claim mapping
- [ ] The permission set is exactly the five in docs/04, enforced rather than free text
