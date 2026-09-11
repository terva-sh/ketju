---
schema: 3
id: TKT-01M29CTY6EE6T8BWR1DGSG3WGV
title: Implement the since, mentions, and moderator lenses
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:views
  - effort:M
assignees: []
milestone: phase-3
parent: TKT-01M29BN36D34YVKVRPNQD3PHDV
origin: null
dependencies:
  - TKT-01M29CRJ1PY355KX3HX7A5N785
blocks_on: none
references:
  - ref: design:lenses
    path: docs/06-thread-views.md
claim: null
archive: null
created_at: 2026-09-11T23:28:54Z
updated_at: 2026-09-11T23:28:54Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:terva/mieli
  name: Mieli
extensions: {}
---

## Description

Three lenses that complete the set. `since:2026-09-01` filters on the index's created_at and backs jump-to-date. `mentions` uses the mentions table to show posts that mention or reply to the viewer. `mod` includes deleted, hidden, and moved posts inline with their tombstone reasons, for moderators only.

The moderator lens is the one with teeth: it is the only place a member-invisible body is rendered, so its role check has to be at the service layer rather than in the UI.

## Acceptance criteria

- [ ] The since lens runs from the index with no database query and accepts a date or a timestamp
- [ ] The mentions lens covers both mentions and replies to the viewer
- [ ] The moderator lens is refused for non-moderators at the service layer, tested by calling it directly
- [ ] All three are URL-addressable and shareable
