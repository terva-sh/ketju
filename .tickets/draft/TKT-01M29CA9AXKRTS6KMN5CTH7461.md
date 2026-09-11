---
schema: 3
id: TKT-01M29CA9AXKRTS6KMN5CTH7461
title: Implement the thread page endpoint
type: task
status: draft
status_reason: null
priority: urgent
due_on: null
labels:
  - area:api
  - effort:L
assignees: []
milestone: phase-1
parent: TKT-01M29BKBMCNDSYHYF9WXAPDNRN
origin: null
dependencies:
  - TKT-01M29C801M9WBEAVCCC6AXNZ4Z
  - TKT-01M29C0WDJ9FETR8EFZ7ED5JYF
  - TKT-01M29C0WF04HEBYMB9DXCB9WCB
  - TKT-01M29C5Y2EVNCBADSQYW12F01D
blocks_on: none
references:
  - ref: design:api-thread-shape
    path: docs/07-api-and-clients.md
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

The load-bearing endpoint: `GET /threads/{id}?page=&lens=&after_log_length=` returning the thread header, a page of rendered posts, lens metadata, and the viewer's read position.

The flow from docs/02: resolve the viewer, check permissions, open the thread, apply the lens to the in-memory index, fetch and render the selected posts through the post cache, then layer on viewer data. The viewer block is the only per-user part, and keeping it the only one is what makes the response cacheable across members.

`ETag` is `(thread_id, log_length, renderer_version)` with `Vary: Cookie`, and `If-None-Match` is honored. The log length is the thread's version number, available without a database query, which is what makes this cheap.

## Acceptance criteria

- [ ] The response matches the documented shape in docs/07, with a golden test per viewer class
- [ ] ETag is derived from thread id, log length, and renderer version, and a matching If-None-Match returns 304
- [ ] A permalink to an ordinal resolves to the page containing it
- [ ] `after_log_length` returns only posts past that point, which is what the SSE client uses to reconcile
- [ ] Deleted posts return tombstones for members and original content for moderators
