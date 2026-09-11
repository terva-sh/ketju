---
schema: 3
id: TKT-01M29CGBYYTJCDV43WDP2FE0Z5
title: Add the web app manifest and icons
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:client
  - area:mobile
  - effort:S
assignees: []
milestone: phase-3
parent: TKT-01M29BN367F3YQZESM63VN26GP
origin: null
dependencies:
  - TKT-01M29CEE4KNQBFP49FVSCRNZA4
blocks_on: none
references:
  - ref: design:mobile-installable
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

A manifest and icon set so the forum can be added to a home screen and open without browser chrome.

No service worker in v1, so no offline caching and no push. The installable web app is the whole mobile story, and docs/10 puts native apps out of scope unless push or share-sheet integration ever forces the question.

## Acceptance criteria

- [ ] The site is installable on iOS and Android and opens standalone
- [ ] Icons and theme colors are defined for both platforms and both themes
- [ ] No service worker is registered
- [ ] The installed app respects safe-area insets on the bottom bars
