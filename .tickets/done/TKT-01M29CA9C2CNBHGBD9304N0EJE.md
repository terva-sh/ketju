---
schema: 3
id: TKT-01M29CA9C2CNBHGBD9304N0EJE
title: Implement single post, replies, and history endpoints
type: task
status: done
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
  - TKT-01M29BYYJ0WJ3C7RRZQMDRP5ZB
blocks_on: none
references:
  - ref: design:post-anatomy
    path: docs/06-thread-views.md
  - ref: pr:58
    path: null
claim: null
archive: null
created_at: 2026-09-11T23:19:48Z
updated_at: 2026-09-24T10:22:57Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase1-run-415af6aa
  name: ""
extensions: {}
---

## Description

`GET /threads/{id}/posts/{ordinal}` for popovers and permalinks, `…/replies` for the inline expander, and `…/history` for the revision list.

The replies endpoint is answered from the index's reply_to column when the thread is open, and from `post_meta` when it is not. Both paths must agree, and the test that proves it is worth more than either implementation.

## Acceptance criteria

- [x] A single post fetch serves the popover for a post on another page
- [x] Direct replies are returned in ordinal order and match between the index path and the post_meta path
- [x] History lists revisions with actor and time, gated by the public_edit_history setting for members
- [x] A tombstoned post returns its tombstone from these endpoints too, not its content

## Implementation plan

Revised at claim, 2026-09-24, after reading the code as it stands (CA9AX and BYYJ0 merged).

1. **Shared access check.** Pull View's access ladder into `threads.Service.access(ctx, v, id)`, which returns the thread row, forum, class, can_moderate and can_reply. The ladder is: no row 404, Hidden 404, anon Deny 401, member Deny 403. All four endpoints then refuse the same viewers in the same way.
2. **Shared post assembler.** `posts()` already renders a snapshot's entries with tombstones, moderator content, authors and reply counts. Single post and replies build a snapshot of the entries they need inside `Actor.Stable` and go through it, so a tombstone reaches a member from these endpoints exactly as from the page (AC4).
3. **ETags.** All four responses use the body-hash ETag (`"<id>-<log_length>-r<renderer>-<digest>"`) with 304 on If-None-Match. `Result` becomes a generic `Served[T]{ETag, NotModified, Body}`.
4. **Single post:** `GET /api/v1/threads/{id}/posts/{ordinal}` returns `dto.Post`. An ordinal past the end is not-found (AC1).
5. **Replies:** `GET …/replies` returns `dto.PostReplies{ordinal, posts}` in ordinal order (AC2).
   - Index path: `threadcache.Cache.Peek(id)` holds a thread only if it is already open, and then `Index.Replies`.
   - post_meta path: otherwise, the new `RepliesTo(thread_id, reply_to)` query over `post_meta_reply_to`.
   - The post_meta path is used only when the projection has caught up with the length being served (`threads.log_length_projected >= snapshot length`); otherwise it answers from the index. A lagging projection would drop a new reply.
   - The bodies are read through the thread either way, since rendering needs them.
6. **History:** `GET …/history` returns `dto.PostHistory{ordinal, deleted, revisions: [{revision, actor, at, reason, source}]}`, per the grooming decision that the client diffs (AC3).
   - `threadlog.Reader.History(ordinal, upTo)` walks the log with `ScanFramesFrom` up to the snapshot length, keeping Post and Edit frames for the ordinal. The actor comes from the frame header, the time from `TimestampMS`, and the source and reason from the body. It is O(log size), with no revision chain in the format.
   - Gate: a moderator always; a member when `settings.PublicEditHistory` is on; an anonymous viewer is unauthenticated; otherwise forbidden.
   - A tombstoned post returns `deleted:true` with no revisions to anyone but a moderator, or the delete would leak through the old revisions (AC4).
7. **Parsing:** path ordinals and query parameters are presence-aware. `?page=` with an empty value is bad-request (review 394's deferred low finding).
8. **DTOs and docs:** the new types go in `internal/dto` with corpus goldens per viewer class, and docs/07 documents the three shapes. The "revisions with diffs" row becomes "revisions (actor, time, source)", per the grooming note.

### Tests
- AC1: a post on page 3 fetched singly equals its page rendering.
- AC2: a property test over random reply graphs; for every ordinal, the index path (thread open) and the post_meta path (a fresh cache, so not open) return the same ordinals in order. Also a lagging projection falling back to the index.
- AC3: three edits by two actors give four revisions with the right actor and time; with the setting off, a member gets 403, a moderator 200, and anonymous 401.
- AC4: after a delete, all three endpoints return the tombstone to a member.

## Notes

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T09:47:41Z

From review 394 of TKT-01M29CA9AX (low, deferred): internal/web/api's threadQuery treats an explicitly empty numeric parameter (?page=, ?ordinal=, ?after_log_length=) as absent, because URL.Query().Get cannot tell the two apart. Docs/07 says a malformed parameter is bad-request. These endpoints will add ordinal parsing to the same handler file, so fix the parser there: check r.URL.Query()[name] for presence, and reject an empty value. Also available from CA9AX: threads.Service.View's permission ladder (Hidden 404, anon Deny 401, member Deny 403), Reader.ReadEntries/HiddenEntry, Actor.Stable, render.Cache.PutDeps with handleMemo re-checking, and the body-hash ETag.

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T09:47:41Z

Added the dependency on TKT-01M29BYYJ0 'Add the flags, audit log, blobs, and settings schema': AC3 gates history on the public_edit_history setting, and the settings table does not exist before BYYJ0.

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T10:22:33Z

Built and verified. The approach followed the plan; what changed and why:

### Shape of the change
- `threads.Service.access` holds the access ladder for all four endpoints, pulled out of `View` unchanged. The page goldens did not move.
- `Result` became `Served[T]{ETag, NotModified, Body}`, with `finish` applying the body-hash ETag and If-None-Match, so all four endpoints validate the same way.
- `snapshot` now carries parallel `ordinals` and `entries`, filled by `take` inside `Actor.Stable`. `posts()` reads each run of consecutive ordinals with one `ReadEntries`: the page is one run, and a post's replies are usually several.
- New:
  - `threadcache.Cache.Peek`, which finds an already-open thread and holds a reference, and never opens.
  - `threadlog.Reader.History(ordinal, upTo)`, a log walk bounded at the snapshot length that keeps only Post and Edit frames.
  - The `RepliesTo` query over the existing `post_meta_reply_to` index.
  - `dto.PostReplies`, `dto.PostHistory` and `dto.PostRevision`, with corpus goldens.

### Decisions (alternatives considered)
- **The replies post_meta path is trusted only at the served length.** The DB read of `RepliesTo` and `log_length_projected` happens in one transaction before the thread opens, and the post_meta ordinals are used only if `projected == logLength` inside Stable. Otherwise the index answers. Rejected: always trusting post_meta when the thread is closed, which drops a reply the projection has not reached (TestRepliesPostMetaBehindUsesTheIndex).
- **The post_meta path saves only the index load**, since the bodies must be read from the log to render. That is as the plan's own open item said. It is kept because the ticket asks for it, and the agreement test is the part of it worth having.
- **History access.** A moderator always; a member when `public_edit_history` is on, else 403; anonymous 401 whatever the setting, because docs/08 makes history a members' and moderators' view. A deleted post's history is `deleted:true` with no revisions, never 410.
- **History returns sources, and the client diffs.** docs/07's row now says so.
- **Peek.** The `closed` and `loading` checks turned out redundant (Close empties the map; a loading entry has no thread), and mutation confirmed both were equivalent. The code relies on the two invariants, and a comment says so.
- **Review 394's deferred low finding is fixed here.** `threadQuery` is presence-aware, so `?page=`, `?ordinal=` and `?after_log_length=` are all bad-request. Path ordinals must be positive integers.

### Tests and mutation
- Tests:
  - AC1: TestSinglePostMatchesThePage, where page 3's posts fetched singly are byte-identical.
  - AC2: TestRepliesPathsAgree, six random reply graphs with edits that move or drop `reply_to` and with deletes, where every ordinal agrees across both paths and the trace shows each path ran n times. Also TestRepliesPostMetaBehindUsesTheIndex and TestRepliesInOrder.
  - AC3: TestHistory, where 3 edits by 2 actors give 4 revisions with actor, time, reason and source; with the setting off, member 403, moderator 200, anonymous 401.
  - AC4: TestTombstoneFromEveryEndpoint, covering the single post, a deleted reply in the replies list, and history, for a member and a moderator.
  - TestHistoryIsBoundedAndOrdered, TestPeekNeverOpens, and the empty-parameter cases in TestAccess.
- Mutation: 31 mutants (24 in the first pass, 7 rerun or added). Survivors that became tests: the history count including a delete frame, and Peek's reference. Two Peek checks were equivalent and have been removed. One malformed mutant was rewritten and rerun.

## Summary

GET /api/v1/threads/{id}/posts/{ordinal}, …/replies and …/history are implemented in internal/service/threads (posts.go, with the access ladder shared in access.go) and internal/web/api. They use the thread page's assembler, tombstones, and body-hash ETags. Replies come from the index when the thread is open and from post_meta otherwise, the latter only once the projection has reached the served length; a property test checks the two agree. History walks the log and is gated on public_edit_history for members. docs/07 documents all three, and the empty-query-parameter finding from review 394 is fixed.
