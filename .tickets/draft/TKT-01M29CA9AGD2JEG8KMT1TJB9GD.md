---
schema: 3
id: TKT-01M29CA9AGD2JEG8KMT1TJB9GD
title: Implement the thread list endpoint
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
  - TKT-01M29C801M9WBEAVCCC6AXNZ4Z
  - TKT-01M29C2WW5N8TR3V76VEB3TGK7
blocks_on: none
references:
  - ref: design:thread-lists
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

`GET /forums/{slug}/threads` with cursor paging: pinned threads first, then by `last_post_at` descending, each row carrying title, tags, author, reply count, last poster, last post time, and the viewer's unread badge count.

Cursor rather than offset paging, because the order is time and a thread bumping between page requests would otherwise duplicate or skip rows. `?sort=created` stays available for announcement forums.

## Acceptance criteria

- [ ] Pinned threads sort ahead of the rest and respect a pin expiry
- [ ] A cursor is stable across a thread being bumped mid-pagination, with no duplicate or skipped row
- [ ] Unread badge counts are per viewer and correct for a thread with no read_positions row
- [ ] Rows come from the threads projection with no thread log opened
