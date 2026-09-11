---
schema: 3
id: TKT-01M29CA9CECTE02DFZGBGN2HC2
title: "Implement the viewer-state endpoints: read, watch, flags, drafts"
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
  - TKT-01M29BYYHQ7PTYNYA3VBBD4PDB
  - TKT-01M29C801M9WBEAVCCC6AXNZ4Z
blocks_on: none
references:
  - ref: design:read-tracking
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

`PUT /threads/{id}/read` and its `DELETE`, `POST /threads/{id}/watch`, `POST /threads/{id}/flags`, and the draft endpoints.

Read position has one rule that is easy to get wrong: the server only ever moves the position forward, unless the user explicitly marks the thread unread. A beacon arriving late, out of order, or from a second device must not drag somebody back to post 12.

Every one of these is also reachable as a form post for the future HTML client, which is why `?mark_read=` on a page load has to be part of the design rather than an afterthought.

## Acceptance criteria

- [ ] A read beacon never moves the position backwards, tested with out-of-order arrivals
- [ ] An explicit mark-unread is the only way the position decreases
- [ ] A draft is upserted per user and thread and survives a reconnect from another device
- [ ] Each endpoint has an equivalent form-post shape documented for the HTML client
