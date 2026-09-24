---
schema: 3
id: TKT-01M29BW9NWKJGM3ZQ2DJH8WPWH
title: Decide the Phase 1 UI conventions
type: spike
status: done
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
updated_at: 2026-09-24T05:05:04Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude/phase1-groom
  name: ""
extensions: {}
---

## Description

Questions 21 and 13 in docs/10, both due at the start of Phase 1 UI work.

CSS approach: hand-written CSS with custom properties, or a utility framework. The lean is hand-written. The design already commits to no component library and a bundle under 60 KB gzipped for the thread view, and the mockups in `docs/mockups/` are the reference for what the result looks like.

Page size default: 25, 20, or 30 posts per page. Cosmetic, but it is baked into URLs people share, so pick it once rather than drifting. The user setting range is 10 to 50 either way.

## Acceptance criteria

- [x] Both decisions are recorded in docs/07 and docs/06
- [x] The CSS decision is checked against the 60 KB gzipped budget before it is adopted, not after

## Notes

**agent:claude/phase1-groom** at 2026-09-24T05:05:03Z

Budget check, done before adoption as criterion 2 requires. Hand-written CSS adds no runtime or build dependency, so its cost against the 60 KB gzipped thread-view budget is only the bytes we write. The mockups in docs/mockups/ are already written this way, and the utility-framework alternative would have added a build step and a purge configuration to keep inside the same budget. The bundle-size CI check TKT-01M29CGBYH enforces the budget on real output. Page size 25 was the documented default. It is fixed now because it appears in shared URLs.

## Summary

Decided with the user on 2026-09-24. The CSS is hand-written with custom properties, with no utility framework, and the default page size is 25 posts (user setting 10-50). Recorded in docs/07 (Preact client) and docs/06 (Paged, not infinite); docs/10 Q13 and Q21 are marked decided.
