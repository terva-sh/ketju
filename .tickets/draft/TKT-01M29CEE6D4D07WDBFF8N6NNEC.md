---
schema: 3
id: TKT-01M29CEE6D4D07WDBFF8N6NNEC
title: Build the thread list screen
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
  - TKT-01M29CA9AGD2JEG8KMT1TJB9GD
blocks_on: none
references:
  - ref: design:thread-lists
    path: docs/06-thread-views.md
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

A forum's threads: pinned first, then by last post, each row with title, tags, author, reply count, last poster, last post time, and an unread badge.

The badge is a control, not an ornament. Clicking it jumps to the first unread post; clicking the title goes to page 1. That distinction is small to build and it is most of what makes a forum feel fast to a returning reader.

## Acceptance criteria

- [ ] Clicking the unread badge lands on the first unread post and clicking the title lands on page 1
- [ ] Pinned threads are visually distinct and sort first
- [ ] Rows stay readable at 390 px with a long title and several tags
- [ ] Paging through the list preserves scroll position on back navigation
