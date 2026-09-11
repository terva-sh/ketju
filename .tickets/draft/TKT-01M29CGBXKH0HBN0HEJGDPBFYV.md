---
schema: 3
id: TKT-01M29CGBXKH0HBN0HEJGDPBFYV
title: Run the accessibility pass on the client
type: task
status: draft
status_reason: null
priority: high
due_on: null
labels:
  - area:client
  - area:mobile
  - effort:M
assignees: []
milestone: phase-1
parent: TKT-01M29BN367F3YQZESM63VN26GP
origin: null
dependencies:
  - TKT-01M29CEE4KNQBFP49FVSCRNZA4
  - TKT-01M29CEE6V358M5KCBJGHD58HM
blocks_on: none
references:
  - ref: design:mobile-targets
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

Semantic landmarks, focus management on page change, `prefers-reduced-motion`, and 44 px minimum touch targets on the phone layout.

The 44 px rule has a carve-out that needs implementing correctly: inline links inside text, such as author names, ordinals, and tag chips, keep their visual size and get an enlarged hit area through padding. Everything standalone gets the full target.

No action is swipe-only. A swipe may be a shortcut, never the only route to a feature.

## Acceptance criteria

- [ ] Every standalone control on the phone layout is at least 44 px on its shorter side, checked automatically
- [ ] Focus moves to the main heading on route change and is never lost to a removed element
- [ ] Animations respect prefers-reduced-motion
- [ ] No feature requires a swipe
- [ ] An automated accessibility scan passes on the forum index, thread list, and thread page
