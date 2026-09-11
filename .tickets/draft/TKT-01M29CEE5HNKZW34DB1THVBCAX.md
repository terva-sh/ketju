---
schema: 3
id: TKT-01M29CEE5HNKZW34DB1THVBCAX
title: Build the fetch layer and signals state
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
  - TKT-01M29CCC02K8F14P0SKAYFMSE6
  - TKT-01M29CA99RTPEA1B3HG24B2KNJ
blocks_on: none
references:
  - ref: design:preact-client
    path: docs/07-api-and-clients.md
claim: null
archive: null
created_at: 2026-09-11T23:22:04Z
updated_at: 2026-09-11T23:22:04Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:terva/mieli
  name: Mieli
extensions: {}
---

## Description

A small SWR-style fetch wrapper that honors ETags, plus `@preact/signals` for the viewer, notifications, and per-thread caches keyed by log length.

Keying the thread cache on log length rather than on time is what makes it correct for free: a cached page is valid exactly as long as the thread has not changed, and the server's own cache uses the same key.

Problem-details errors need one handling path here, mapping a stable type code to a message rather than each screen inventing its own.

## Acceptance criteria

- [ ] Conditional requests send If-None-Match and a 304 serves the cached body
- [ ] Per-thread client caches are keyed on log length and a new length replaces rather than merges
- [ ] Problem-details responses map to messages in one place
- [ ] A failed request has a retry path that does not leave a screen in a permanent spinner
