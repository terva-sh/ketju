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
updated_at: 2026-09-24T05:06:25Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude/phase1-groom
  name: ""
extensions: {}
---

## Description

Custom properties for color, spacing, and type, a light and a dark theme, and the three breakpoints from docs/06: phone below 720 px, tablet 720 to 1100 px, desktop above 1100 px.

The mockups in `docs/mockups/` are the reference, including their type choices, and `generate.py` regenerates them. Match the mockups rather than approximating them, because they are the only place the intended result exists.

Safe-area handling belongs here: `viewport-fit=cover` with `env(safe-area-inset-*)` padding on the tab bar and reply bar, or the bottom bar sits under the home indicator on an iPhone.

## Acceptance criteria

- [ ] Light and dark themes are driven by custom properties and follow prefers-color-scheme with a user override
- [ ] The three breakpoints match docs/06 and layout changes are CSS rather than component swaps, except the navigation shell
- [ ] Safe-area insets are applied to the bottom bars through env(safe-area-inset-*) with viewport-fit=cover, and a Playwright check at 390 px confirms the padding rules apply; real-device verification is in TKT-01M38WTKSQ (Verify the Phase 1 client on real iOS and Android devices)
- [ ] Playwright screenshots at 390 px and 1440 px are committed beside the tests and compared against the matching file in docs/mockups/; every visible divergence is listed in the pull request description

## Notes

**agent:claude/phase1-groom** at 2026-09-24T05:06:25Z

Acceptance criteria rewritten during Phase 1 grooming (2026-09-24), agreed with the user as part of preparing the autonomous run. Rewritten criteria move to the end of the list, so their numbers changed. An agent cannot check against a device with a home indicator, so that check moved to TKT-01M38WTKSQ; the criterion here is what an emulated viewport can prove. 'Matches the mockups' now names its method: committed screenshots compared against docs/mockups/, with every divergence listed in the PR.
