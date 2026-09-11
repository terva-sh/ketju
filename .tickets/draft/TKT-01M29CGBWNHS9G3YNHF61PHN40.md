---
schema: 3
id: TKT-01M29CGBWNHS9G3YNHF61PHN40
title: Implement the read-tracking beacon
type: task
status: draft
status_reason: null
priority: high
due_on: null
labels:
  - area:client
  - effort:M
assignees: []
milestone: phase-1
parent: TKT-01M29BN367F3YQZESM63VN26GP
origin: null
dependencies:
  - TKT-01M29CEE6V358M5KCBJGHD58HM
  - TKT-01M29CA9CECTE02DFZGBGN2HC2
blocks_on: none
references:
  - ref: design:read-tracking
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

A post counts as read once it has been on screen for at least a second. Positions batch every 30 seconds and flush on navigation and unload through `navigator.sendBeacon`.

The one-second dwell is what stops a fast scroll to the bottom from marking an entire thread read. The same beacon works on the phone layout, which is deliberate: read tracking is the same integer with the same semantics everywhere, including the coarser page-load marking the non-JS client will do.

## Acceptance criteria

- [ ] A post scrolled past faster than a second is not marked read
- [ ] Positions flush on navigation and on unload, tested with sendBeacon
- [ ] Batching never sends a position behind one already sent in the same session
- [ ] The behavior is identical on the phone layout
