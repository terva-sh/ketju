---
schema: 3
id: TKT-01M29CA9A5PGMCFDSVWQAHT87G
title: Implement the forums endpoints
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
  - TKT-01M29C5Y2EVNCBADSQYW12F01D
blocks_on: none
references:
  - ref: design:api-resources
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

`GET /forums` returning the forum tree with per-forum unread counts, filtered by the viewer's read permission.

Visibility is the subtle part. A `restricted` forum the viewer cannot read must be absent from the tree rather than present and forbidden, because a list of forum names is itself information in a small community.

## Acceptance criteria

- [ ] The tree honors one level of nesting and the configured position ordering
- [ ] A forum the viewer cannot read is absent rather than returned with a flag
- [ ] Unread counts per forum come from read_positions and mark_all_read_at with no per-post rows
- [ ] An anonymous viewer sees public forums only
