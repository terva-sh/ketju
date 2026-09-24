---
schema: 3
id: TKT-01M38WTKSQHX60XJ8WZFJH4S71
title: Verify the Phase 1 client on real iOS and Android devices
type: task
status: draft
status_reason: null
priority: high
due_on: null
labels:
  - area:mobile
  - effort:S
  - human-only
assignees: []
milestone: phase-1
parent: TKT-01M29BN367F3YQZESM63VN26GP
origin: null
dependencies:
  - TKT-01M29CGBY16MK20W1QATC5N7WE
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-24T05:04:48Z
updated_at: 2026-09-24T05:04:53Z
created_by:
  id: agent:claude/phase1-groom
  name: ""
updated_by:
  id: agent:claude/phase1-groom
  name: ""
extensions: {}
---

## Description

Some Phase 1 acceptance criteria need a person holding a real phone. An agent
cannot tick them, and an agent working unattended must not be the thing that
decides they are met. This ticket collects them in one place, so the
autonomous run leaves them unticked with a pointer here instead of stalling on
them or ticking them unearned.

The items, each named by the ticket it came from:

- TKT-01M29BW9P4 (landing and phone navigation): last-read landing, the
  five-item tab bar, and the composer sheet, confirmed against the mockups on
  a real iOS device and a real Android device.
- TKT-01M29CCC0F (CSS system): safe-area padding verified on a device with a
  home indicator.
- TKT-01M29CGBVN (mobile composer sheet and reply bar): sheet and sticky
  reply bar behaviour under a real virtual keyboard on iOS Safari and Android
  Chrome.
- TKT-01M29BN367 (Preact client epic): no feature reachable on desktop but
  absent on phone, walked through by hand at both widths.

Grooming rewrote each source criterion into what an agent can check: emulated
viewports in Playwright and the presence of `env(safe-area-inset-*)` padding.
This ticket holds the half that needs hardware.

Run it once the Playwright gate TKT-01M29CGBY1 is green, since that is when
the client is complete enough to walk through.

## Acceptance criteria

- [ ] Every item listed in the description is checked on a real iOS device and a real Android device, and the result is noted per item
- [ ] Each defect found is filed as its own ticket linked from here
