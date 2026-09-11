---
schema: 3
id: TKT-01M29BYYHQ7PTYNYA3VBBD4PDB
title: "Add the viewer-state schema: reads, watches, notifications, drafts"
type: task
status: draft
status_reason: null
priority: high
due_on: null
labels:
  - area:db
  - effort:M
assignees: []
milestone: phase-1
parent: TKT-01M29BKBKV6HH8W0DN6QXQNZ0R
origin: null
dependencies:
  - TKT-01M29BW9Q17KEBMHJ91G6QHW36
blocks_on: none
references:
  - ref: design:data-model-viewer
    path: docs/04-data-model.md
claim: null
archive: null
created_at: 2026-09-11T23:13:37Z
updated_at: 2026-09-11T23:32:24Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:terva/mieli
  name: Mieli
extensions: {}
---

## Description

`read_positions` as one row per user and thread ever visited, `watches` over threads or forums at level watch or mute, `notifications` with the six kinds from docs/04, and `drafts` keyed on user and thread.

Read tracking is one integer per pair, not a row per post. Combined with `threads.post_count` that yields unread counts, and `users.mark_all_read_at` covers every thread the user never opened so those need no rows at all. That decision is why unread tracking stays cheap at 1000 users and 10000 threads, and it is worth stating in the schema comments so nobody later adds a per-post table.

## Acceptance criteria

- [ ] Read positions are one row per (user, thread) with a primary key that enforces it
- [ ] A drafts row is unique per (user, thread), with a null thread meaning a new-thread draft under a forum
- [ ] Notification kinds are constrained to the documented set
- [ ] Unread counts for a user with no read_positions rows resolve correctly from mark_all_read_at alone
