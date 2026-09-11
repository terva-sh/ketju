---
schema: 3
id: TKT-01M29CTY7K2YTY0V54YKJTKX1W
title: Build the cross-forum unread, new, participated, and watching lists
type: task
status: draft
status_reason: null
priority: high
due_on: null
labels:
  - area:views
  - effort:L
assignees: []
milestone: phase-3
parent: TKT-01M29BN36D34YVKVRPNQD3PHDV
origin: null
dependencies:
  - TKT-01M29BYYHQ7PTYNYA3VBBD4PDB
  - TKT-01M29CRJ29H6TZPN0X42SEW5RN
blocks_on: none
references:
  - ref: design:thread-lists
    path: docs/06-thread-views.md
claim: null
archive: null
created_at: 2026-09-11T23:28:54Z
updated_at: 2026-09-11T23:28:54Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:terva/mieli
  name: Mieli
extensions: {}
---

## Description

`/unread`, `/new`, `/participated`, and `/watching`: the "what's new" surfaces that live in the top navigation on desktop and in the bottom tab bar on phones.

These are the queries most likely to get slow. Unread across all forums combines read_positions, mark_all_read_at, thread post counts, and permission filtering, and it runs on every visit from every member. Get the query shape right here rather than discovering it at a thousand threads.

## Acceptance criteria

- [ ] Each list is permission-filtered and excludes forums the viewer cannot read
- [ ] Unread is correct for a user with no read_positions rows, relying on mark_all_read_at
- [ ] Query cost is bounded and measured at ten thousand threads, with the numbers recorded
- [ ] Mark all read at forum and site level works and is reflected immediately
