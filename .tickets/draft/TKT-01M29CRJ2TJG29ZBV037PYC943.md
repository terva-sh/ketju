---
schema: 3
id: TKT-01M29CRJ2TJG29ZBV037PYC943
title: Build the notification generator
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:views
  - effort:M
assignees: []
milestone: phase-3
parent: TKT-01M29BN36D34YVKVRPNQD3PHDV
origin: null
dependencies:
  - TKT-01M29C0WF9GFGVG7QSAYRAKAA4
  - TKT-01M29BYYHQ7PTYNYA3VBBD4PDB
blocks_on: none
references:
  - ref: design:data-model-viewer
    path: docs/04-data-model.md
claim: null
archive: null
created_at: 2026-09-11T23:27:36Z
updated_at: 2026-09-11T23:27:36Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:terva/mieli
  name: Mieli
extensions: {}
---

## Description

The event bus subscriber that writes `notifications` rows: reply_to_you, mention, quoted, and thread_you_watch.

One post can trigger several kinds for the same person. A reply that also quotes and also mentions you should not arrive as three separate alerts, so collapse by post and viewer, keeping the strongest kind.

Notifying yourself for your own post is the other obvious case, and it needs suppressing at generation rather than filtering at display.

## Acceptance criteria

- [ ] One post produces at most one notification per recipient, keeping the most specific kind
- [ ] A user is never notified about their own post
- [ ] A muted thread or forum produces no notifications
- [ ] Generation is idempotent under event replay, so catch-up does not duplicate alerts
