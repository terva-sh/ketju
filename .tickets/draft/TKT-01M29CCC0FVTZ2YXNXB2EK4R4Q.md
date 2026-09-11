---
schema: 3
id: TKT-01M29CCC0FVTZ2YXNXB2EK4R4Q
title: Build the CSS system, themes, and breakpoints
type: task
status: draft
status_reason: null
priority: urgent
due_on: null
labels:
  - area:client
  - effort:M
assignees: []
milestone: phase-1
parent: TKT-01M29BN367F3YQZESM63VN26GP
origin: null
dependencies:
  - TKT-01M29BW9NWKJGM3ZQ2DJH8WPWH
blocks_on: none
references:
  - ref: design:mobile
    path: docs/06-thread-views.md
  - ref: design:mockups
    path: docs/mockups/generate.py
claim: null
archive: null
created_at: 2026-09-11T23:20:57Z
updated_at: 2026-09-11T23:20:57Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:terva/mieli
  name: Mieli
extensions: {}
---

## Description

Custom properties for color, spacing, and type, a light and a dark theme, and the three breakpoints from docs/06: phone below 720 px, tablet 720 to 1100 px, desktop above 1100 px.

The mockups in `docs/mockups/` are the reference, including their type choices, and `generate.py` regenerates them. Match the mockups rather than approximating them, because they are the only place the intended result exists.

Safe-area handling belongs here: `viewport-fit=cover` with `env(safe-area-inset-*)` padding on the tab bar and reply bar, or the bottom bar sits under the home indicator on an iPhone.

## Acceptance criteria

- [ ] Light and dark themes are driven by custom properties and follow prefers-color-scheme with a user override
- [ ] The three breakpoints match docs/06 and layout changes are CSS rather than component swaps, except the navigation shell
- [ ] Safe-area insets are applied to the bottom bars and verified on a device with a home indicator
- [ ] Rendered output matches the mockups at 390 px and 1440 px
