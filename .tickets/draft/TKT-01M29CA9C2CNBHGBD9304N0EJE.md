---
schema: 3
id: TKT-01M29CA9C2CNBHGBD9304N0EJE
title: Implement single post, replies, and history endpoints
type: task
status: draft
status_reason: null
priority: high
due_on: null
labels:
  - area:api
  - effort:M
assignees: []
milestone: phase-1
parent: TKT-01M29BKBMCNDSYHYF9WXAPDNRN
origin: null
dependencies:
  - TKT-01M29C0WDJ9FETR8EFZ7ED5JYF
  - TKT-01M29C801M9WBEAVCCC6AXNZ4Z
blocks_on: none
references:
  - ref: design:post-anatomy
    path: docs/06-thread-views.md
claim: null
archive: null
created_at: 2026-09-11T23:19:48Z
updated_at: 2026-09-11T23:19:48Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:terva/mieli
  name: Mieli
extensions: {}
---

## Description

`GET /threads/{id}/posts/{ordinal}` for popovers and permalinks, `…/replies` for the inline expander, and `…/history` for the revision list.

The replies endpoint is answered from the index's reply_to column when the thread is open, and from `post_meta` when it is not. Both paths must agree, and the test that proves it is worth more than either implementation.

## Acceptance criteria

- [ ] A single post fetch serves the popover for a post on another page
- [ ] Direct replies are returned in ordinal order and match between the index path and the post_meta path
- [ ] History lists revisions with actor and time, gated by the public_edit_history setting for members
- [ ] A tombstoned post returns its tombstone from these endpoints too, not its content
