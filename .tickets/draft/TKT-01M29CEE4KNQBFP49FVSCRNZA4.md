---
schema: 3
id: TKT-01M29CEE4KNQBFP49FVSCRNZA4
title: Build the app shell with both navigation layouts
type: task
status: draft
status_reason: null
priority: urgent
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
  - TKT-01M29CCC0FVTZ2YXNXB2EK4R4Q
  - TKT-01M29CCC02K8F14P0SKAYFMSE6
  - TKT-01M29BW9P4PXMFKF4VAP8K9QPD
blocks_on: none
references:
  - ref: design:mobile-nav
    path: docs/06-thread-views.md
claim: null
archive: null
created_at: 2026-09-11T23:22:04Z
updated_at: 2026-09-24T05:05:27Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude/phase1-groom
  name: ""
extensions: {}
---

## Description

The navigation shell: a top bar on desktop and a five-item bottom tab bar on phones, carrying Forums, Unread, New, Alerts, and Me with the same badge counts at both sizes.

This is the one place where layout is a component choice rather than CSS, because a tab bar and a top bar are different markup. Everything else reflows. Keeping that list at one item is what stops the phone layout from forking into a second product.

Phone screens also get a 52 px top bar with a back control, a truncating context title, search, and a "more" menu holding the thread actions that live in the desktop toolbar.

## Acceptance criteria

- [ ] The shell swaps between top bar and tab bar at the 720 px breakpoint with no route change
- [ ] Badge counts on Unread and New match between layouts
- [ ] The phone top bar truncates a long forum name rather than wrapping or pushing controls off screen
- [ ] Every desktop toolbar action is reachable from the phone more menu
