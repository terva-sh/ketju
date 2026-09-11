---
schema: 3
id: TKT-01M29BW9NWKJGM3ZQ2DJH8WPWH
title: Decide the Phase 1 UI conventions
type: spike
status: draft
status_reason: null
priority: high
due_on: null
labels:
  - area:client
  - effort:S
  - decision
assignees: []
milestone: phase-1
parent: TKT-01M29BN367F3YQZESM63VN26GP
origin: null
dependencies: []
blocks_on: none
references:
  - ref: design:open-questions-clients
    path: docs/10-open-questions.md
claim: null
archive: null
created_at: 2026-09-11T23:12:10Z
updated_at: 2026-09-11T23:12:10Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:terva/mieli
  name: Mieli
extensions: {}
---

## Description

Questions 21 and 13 in docs/10, both due at the start of Phase 1 UI work.

CSS approach: hand-written CSS with custom properties, or a utility framework. The lean is hand-written. The design already commits to no component library and a bundle under 60 KB gzipped for the thread view, and the mockups in `docs/mockups/` are the reference for what the result looks like.

Page size default: 25, 20, or 30 posts per page. Cosmetic, but it is baked into URLs people share, so pick it once rather than drifting. The user setting range is 10 to 50 either way.

## Acceptance criteria

- [ ] Both decisions are recorded in docs/07 and docs/06
- [ ] The CSS decision is checked against the 60 KB gzipped budget before it is adopted, not after
