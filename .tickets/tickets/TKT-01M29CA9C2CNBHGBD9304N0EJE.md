---
schema: 3
id: TKT-01M29CA9C2CNBHGBD9304N0EJE
title: Implement single post, replies, and history endpoints
type: task
status: ready
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
updated_at: 2026-09-24T05:21:20Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude/phase1-groom
  name: ""
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

## Implementation plan

Pre-groom plan (2026-09-24). Revise at claim, after reading the code as it stands then.

### Approach
1. `GET /api/v1/threads/{id}/posts/{ordinal}` → `dto.Post`. It runs the same permission check and shared post assembler as the thread page (TKT-01M29CA9AX), for one ordinal. A post on another page needs no paging context. An ordinal past `Index.Len()` is `not-found` (`threadlog.ErrNoSuchOrdinal` already maps).
2. `GET …/replies` → a list of `dto.Post` in ordinal order, with two paths behind one function:
   - Index path: the thread is already in the open-thread cache, so use `Index.Replies(ordinal)`. This needs a non-opening `threadcache.Cache.Peek(id)`; today `Acquire` always opens.
   - post_meta path: otherwise, use the sqlc `RepliesTo(thread_id, reply_to)` over the `(thread_id, reply_to)` index. Then fetch the bodies through the reader, because the expander renders the posts.
3. `GET …/history` → a new `dto.PostHistory{ordinal, revisions: [{revision, actor, at, reason}]}`. Revisions are not chained in the index, so walk the log with `threadlog.ScanFrames` and keep the `TypePost` and `TypeEdit` frames for the ordinal. The actor comes from `FrameHeader.Actor` and the time from `TimestampMS`, with actors resolved in one `UsersByIDs`. Gate: moderators always see it; members only when the `settings` key `public_edit_history` is true (default on, docs/08); otherwise `forbidden`.
4. Tombstones: all three endpoints return a tombstone for a deleted post to non-moderators, meaning `deleted:true` and no HTML, never 410 (the ticket says tombstone). History of a deleted post is refused to members as well, or the delete leaks through old revisions.

### Files
internal/service/posts/{single.go,replies.go,history.go}, internal/web/api/posts_read.go, internal/store/threadcache/cache.go (`Peek`), internal/dto (`PostHistory`, `PostList` + goldens per viewer class). Add the shapes to docs/07 in the same PR.

### Tests
- AC1: seed 60 posts, request ordinal 3 while "on" page 3; the result equals the page-1 rendering of ordinal 3.
- AC2: a property test builds random reply graphs, projects them to post_meta (or seeds post_meta directly), and asserts the index path and the post_meta path return identical ordinal lists, in order, for every ordinal. A second run evicts the thread to force path B.
- AC3: after 3 edits by two actors, 4 revisions come back with the right actor and time. Setting off: a member gets `forbidden` and a moderator gets 200.
- AC4: after deleting a post, all three endpoints return the tombstone to a member.

### Design
Reread docs/07 "Resources" (the three rows), docs/06 "Post anatomy" (popover and expander), docs/08 "Post history" and "Tombstones", and docs/04 "post_meta".

### Interactions
Consumes TKT-01M29C0WDJ (read path), TKT-01M29CA9AX (assembler and permission check), TKT-01M29BYYJ8 and TKT-01M29C2WW5 (post_meta), and TKT-01M29BYYJ0 (settings). TKT-01M29CP2Q3 (history view with diffs) and TKT-01M29CGBW6 (inline expander) consume these.
Open: docs/07 says history returns "revisions with diffs", but this ticket asks for actor and time only. The plan returns no diffs and leaves them to TKT-01M29CP2Q3.
Open: history is a full log scan per request (O(log size)). No revision chain exists in the format.
Open: rendering replies needs the log anyway, so the post_meta path saves only the index load.

### Settled in grooming (2026-09-24)

- History returns each revision's actor, time and source. The client diffs them. If that falls short of docs/07's 'revisions with diffs', fix the wording in docs/07.
