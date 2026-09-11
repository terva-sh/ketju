---
schema: 3
id: TKT-01M29CGBX4MC2X3D11JRX52WJY
title: Add the desktop keyboard shortcuts
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:client
  - effort:S
assignees: []
milestone: phase-1
parent: TKT-01M29BN367F3YQZESM63VN26GP
origin: null
dependencies:
  - TKT-01M29CEE6V358M5KCBJGHD58HM
blocks_on: none
references:
  - ref: design:canonical-view
    path: docs/06-thread-views.md
claim: null
archive: null
created_at: 2026-09-11T23:23:08Z
updated_at: 2026-09-11T23:23:08Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:terva/mieli
  name: Mieli
extensions: {}
---

## Description

`j` and `k` to move between posts, `g` then a number to jump to an ordinal, `u` to jump to first unread, and `n` for the next unread thread in the forum.

Shortcuts are absent on the phone layout by design rather than by omission. Nothing here may be the only way to reach a feature, since a keyboard-only path would be a desktop-only feature and docs/06 forbids those.

## Acceptance criteria

- [ ] Every documented shortcut works and none fires while focus is in the composer
- [ ] Each shortcut has an equivalent visible control
- [ ] A discoverable help overlay lists them
- [ ] Shortcuts are inert on the phone layout
