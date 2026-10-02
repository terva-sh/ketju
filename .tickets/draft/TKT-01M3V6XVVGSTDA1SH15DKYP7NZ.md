---
schema: 3
id: TKT-01M3V6XVVGSTDA1SH15DKYP7NZ
title: Let a member edit their display name, bio and avatar
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:api
  - area:client
assignees: []
milestone: phase-4
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-01T07:47:40Z
updated_at: 2026-10-02T15:42:10Z
created_by:
  id: agent:claude-code/phase3-groom-pages
  name: ""
updated_by:
  id: agent:claude-code/phase4-run-orchestrator
  name: ""
extensions: {}
---

## Description

The profile shows `bio_html`, a display name and an avatar (docs/07 `GET /users/{handle}`), but no endpoint or screen lets a member set any of them. `users.bio_source` and `display_name` exist (docs/04). An avatar needs the blob store, TKT-01M29CXK0K.

Found while grooming TKT-01M3V3CFMA (Build the member profile screen and their post list). The profile renders whatever is stored, so every bio stays empty until this lands.

This needs a design line first. The open questions are the endpoint, the bio's length limit, whether a display name passes the handle's character rules, and whether a change is audited.
