---
schema: 3
id: TKT-01M29CGBZDVGWNDTKHJFHHSGY5
title: Add the page picker and lens sheets on phones
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:client
  - area:mobile
  - effort:M
assignees: []
milestone: phase-3
parent: TKT-01M29BN367F3YQZESM63VN26GP
origin: null
dependencies:
  - TKT-01M29CEE6V358M5KCBJGHD58HM
blocks_on: none
references:
  - ref: design:mobile-pages
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

A "Page 3 of 9" control with previous and next, where tapping the label opens a picker sheet with page numbers plus jump-to-unread and jump-to-date. Lenses open from a sheet as well.

The sheet is how a phone gets the lens switcher that sits in the desktop thread header, and it is what keeps "every lens is reachable at both sizes" true rather than aspirational.

## Acceptance criteria

- [ ] The pager label opens a sheet with page numbers, jump to unread, and jump to date
- [ ] Every lens available on desktop is reachable from the lens sheet
- [ ] Sheet controls meet the 44 px target
- [ ] Choosing a page or a lens updates the URL so the result is shareable
