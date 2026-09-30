---
schema: 3
id: TKT-01M3S3ZS0H5485CG25KMRCT5N8
title: Show the last-read time beside the unread divider's ordinal
type: task
status: draft
status_reason: null
priority: low
due_on: null
labels:
  - area:client
assignees: []
milestone: null
parent: TKT-01M29BN367F3YQZESM63VN26GP
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-30T12:17:48Z
updated_at: 2026-09-30T12:17:48Z
created_by:
  id: agent:claude-code/phase2-run-39wbn4q
  name: ""
updated_by:
  id: agent:claude-code/phase2-run-39wbn4q
  name: ""
extensions: {}
---

## Description

Found comparing the thread screenshots with `docs/mockups/Thread{Light,Dark}.dc.html` (TKT-01M39WBN4Q, divergence T5).

The mockup's divider reads "last read #63 · Tue 14:10". The client's reads "last read #53" (`web/client/src/components/thread/UnreadDivider.tsx`). docs/06 asks only for "N new posts below", so this is the mockup adding detail rather than a defect. The database keeps `read_positions.last_read_at` (migration 0005), but the thread page's viewer DTO carries only `last_read_ordinal`.

### What to do
Carry `last_read_at` in the thread page's viewer DTO and draw it after the ordinal, in the format TKT-01M3S3ZF48 (Decide between relative and absolute post times) settles.

## Acceptance criteria

- [ ] The divider reads last read #N · TIME when the viewer has a read position
