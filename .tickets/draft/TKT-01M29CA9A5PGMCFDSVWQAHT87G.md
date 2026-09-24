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
updated_at: 2026-09-24T05:21:03Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude/phase1-groom
  name: ""
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

## Implementation plan

Pre-groom plan (2026-09-24). Revise at claim, after reading the code as it stands then.

### Approach
1. `service/forums.Tree(ctx, viewer) (dto.ForumTree, error)`. Load every forum row with the sqlc `ListForums`, ordered by `(parent_id NULLS FIRST, position, id)`. The count is small: a community has tens of forums.
2. Filter each forum with `perm.Evaluator.Can(ctx, viewer, forumID, "read")` from TKT-01M29C5Y2E. A forum that fails is dropped from the result, not flagged. Anonymous viewers are evaluated as the `everyone` group, so `members` and `restricted` forums fall out without a special case.
3. Build the tree: roots, then children under their parent, one level only (docs/04). A child whose `parent_id` points at a child is a data error, logged and skipped. The migration or admin UI should prevent it, not this read.
4. Per-forum unread: one hand-written grouped query over `threads` LEFT JOIN `read_positions` for the viewer, using `users.mark_all_read_at` for threads with no row (docs/06 rule), `GROUP BY forum_id`. The count is threads with unread posts. Anonymous viewers get 0 without a query. `thread_count` comes from the same grouped query.
5. `GET /api/v1/forums` in internal/web/api/forums.go returns `dto.ForumTree` unchanged.

### Files
internal/service/forums/tree.go, internal/web/api/forums.go, and a sqlc query file for forums. Reuses `dto.Forum` and `dto.ForumTree` and the goldens forums.anon.json and forums.member.json.

### Tests
- AC1: service test with nested forums inserted out of order; the output follows `position`, children sit under parents, and a grandchild is refused.
- AC2: a `restricted` forum without a read grant is absent from the JSON. Compare with a viewer who has the grant (a `restricted` child under a readable parent included). The test asserts on the absence of the slug in the raw body, not only in the decoded struct.
- AC3: a query-log assertion that the unread query touches only `threads`, `read_positions`, and `users`. Plus correctness cases: no row and last_post_at before mark_all_read_at → 0; no row and after → counted; a row behind post_count → counted.
- AC4: an anonymous viewer sees only `public` forums, as a golden case (forums.anon.json already exists) and a handler test.

### Design
Reread docs/04 "forums", "permissions", and "read_positions", docs/06 "Read tracking", and docs/07 "Resources" (/forums).

### Interactions
Consumes TKT-01M29C5Y2E (`perm.Evaluator`), TKT-01M29BYYHG (forums, permissions), TKT-01M29BYYHQ (read_positions), and TKT-01M29BYYJ8 (threads). TKT-01M29CA99R reuses the unread rule; share it as one SQL fragment or function in internal/service/unread.
Open: the unit of `dto.Forum.Unread` (threads with unread posts, or unread posts) is not stated in docs. The plan uses threads.
Open: whether a readable child under an unreadable parent is hidden or lifted to the root. The plan hides it.
Open: archived forums are listed; docs/04 has the flag but no rule.

### Settled in grooming (2026-09-24)

- The forum unread count counts threads with unread posts.
- A child forum under an unreadable parent is hidden.
- Add last-activity fields to dto.Forum; the mockups show them.
