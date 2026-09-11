---
schema: 3
id: TKT-01M29CXK1X0EF4CPPN11KVGC2Z
title: Build the admin users, groups, and permissions screens
type: task
status: draft
status_reason: null
priority: high
due_on: null
labels:
  - area:moderation
  - area:client
  - effort:L
assignees: []
milestone: phase-2
parent: TKT-01M29BN36JH4M1W5819D06PGPP
origin: null
dependencies:
  - TKT-01M29BYYHGZY39T1F2NGJX8MED
  - TKT-01M29CEE4KNQBFP49FVSCRNZA4
blocks_on: none
references:
  - ref: design:data-model-perms
    path: docs/04-data-model.md
claim: null
archive: null
created_at: 2026-09-11T23:30:21Z
updated_at: 2026-09-11T23:30:21Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:terva/mieli
  name: Mieli
extensions: {}
---

## Description

The user list with role and status, group membership editing, and the permission grid of groups against forums over read, create_thread, reply, upload, and moderate.

A permission grid is where an admin quietly locks everyone out of a forum, including themselves. The UI should show the effective result of a change for a sample member rather than only the rows being edited.

Memberships sourced from an IdP are shown as such and are not editable by hand, since the next login would undo the edit anyway.

## Acceptance criteria

- [ ] Group membership and per-forum permissions are editable and take effect within the cache lifetime
- [ ] IdP-sourced memberships are marked and cannot be edited manually
- [ ] The screen shows the effective permissions for a chosen member, not only the raw rows
- [ ] Every change is written to audit_log
