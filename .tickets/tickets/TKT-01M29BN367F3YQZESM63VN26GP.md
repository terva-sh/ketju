---
schema: 3
id: TKT-01M29BN367F3YQZESM63VN26GP
title: "Preact client: shell, thread UI, and both layouts"
type: epic
status: ready
status_reason: null
priority: urgent
due_on: null
labels:
  - area:client
  - effort:XL
assignees: []
milestone: phase-1
parent: null
origin: null
dependencies:
  - TKT-01M29BKBMCNDSYHYF9WXAPDNRN
blocks_on: children
references:
  - ref: design:clients
    path: docs/07-api-and-clients.md
  - ref: design:thread-views
    path: docs/06-thread-views.md
  - ref: design:mockups
    path: docs/mockups/generate.py
claim: null
archive: null
created_at: 2026-09-11T23:08:14Z
updated_at: 2026-09-24T05:21:19Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude/phase1-groom
  name: ""
extensions: {}
---

## Description

`web/client` built with Vite and embedded into the binary through `internal/web/spa`: Preact 10.x, signals, preact-iso routing, TypeScript, and a hand-written CSS system.

One bundle serves both layouts. Below 720 px is the phone layout with a bottom tab bar, 720 to 1100 px is single-column with desktop navigation, and above 1100 px is the desktop layout in `docs/mockups/`. The navigation shell swaps between a top bar and a tab bar as a component choice and everything else reflows in CSS.

Phone is not a reduction of desktop. docs/09 lists "phone layout bolted on late" as a named risk and makes both layouts a Phase 1 exit criterion, which is why the mobile composer sheet and the sticky reply bar are Phase 1 tickets rather than polish.

The canonical thread page carries the single most valuable piece of UI in a linear forum: the "N new posts below" divider between read and unread posts. It ships in the first milestone.

## Acceptance criteria

- [ ] Playwright smoke tests pass at a 390 px phone viewport and a 1440 px desktop viewport, and run in CI on every change
- [ ] The thread view bundle is under 60 KB gzipped
- [ ] Every standalone control on the phone layout is at least 44 px on its shorter side
- [ ] `go build` alone produces a complete binary when the client is unchanged, with `go generate` running the client build
- [ ] Every screen and action reached in the 1440 px Playwright run is also reached in the 390 px run; a hand walk-through on real devices is in TKT-01M38WTKSQ (Verify the Phase 1 client on real iOS and Android devices)

## Notes

**agent:claude/phase1-groom** at 2026-09-24T05:06:25Z

Acceptance criteria rewritten during Phase 1 grooming (2026-09-24), agreed with the user as part of preparing the autonomous run. Rewritten criteria move to the end of the list, so their numbers changed. 'No feature is reachable on desktop but absent on phone' had no method. It is now checked by making the 390 px Playwright run cover everything the 1440 px run reaches; a hand walk-through on real devices is in TKT-01M38WTKSQ.
