---
schema: 3
id: TKT-01M29CA9A5PGMCFDSVWQAHT87G
title: Implement the forums endpoints
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
  - TKT-01M29C801M9WBEAVCCC6AXNZ4Z
  - TKT-01M29C5Y2EVNCBADSQYW12F01D
blocks_on: none
references:
  - ref: design:api-resources
    path: docs/07-api-and-clients.md
claim: null
archive: null
created_at: 2026-09-11T23:19:48Z
updated_at: 2026-09-25T02:29:21Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase1-run-415af6aa
  name: ""
extensions: {}
---

## Description

`GET /forums` returning the forum tree with per-forum unread counts, filtered by the viewer's read permission.

Visibility is the subtle part. A `restricted` forum the viewer cannot read must be absent from the tree rather than present and forbidden, because a list of forum names is itself information in a small community.

## Acceptance criteria

- [x] The tree honors one level of nesting and the configured position ordering
- [x] A forum the viewer cannot read is absent rather than returned with a flag
- [x] Unread counts per forum come from read_positions and mark_all_read_at with no per-post rows
- [x] An anonymous viewer sees public forums only

## Implementation plan

Revised at claim (2026-09-25), after reading the code.

### Approach
1. Add `internal/service/forums` with `Service{DB, Perm, Log}` and `Tree(ctx, viewer) (dto.ForumTree, error)`.
2. `ListForums` already returns roots and children in position order. `perm.CanEach(read)` decides every forum in one batch. A forum the viewer cannot read is dropped, and so are its children (settled). Anonymous viewers are evaluated as `everyone`, so only public forums remain (AC4).
3. `buildTree(rows, readable)` is a pure function:
   - Roots come first, and children go under their root in list order.
   - A row whose parent is itself a child is logged and skipped. The depth triggers in migration 0003 already refuse one, so the check is defensive.
   - Children are `[]`, not null.
4. Thread count comes from `forum_thread_counts`: threads with a post, kept by triggers. That matches what the unread count counts from.
5. Unread is a new sqlc query, `ForumUnread`. It is `MeCounts` without notifications and participation:
   - It uses `forum_thread_counts`, or `threads_forum_active` after a mark.
   - It subtracts the viewer's `read_positions` and mutes (`watches`).
   - It never reads `post_meta` or any per-post row (AC3).
   - The unit is threads with unread posts (settled).
   - A test holds its per-forum sum equal to `/me`'s `unread.threads` over the same fixture, so the badge and the index agree.
   - Anonymous viewers get 0 without the query.
6. Last activity (settled) is a new query, `ForumLastPosts`:
   - For each listed forum, the thread with the latest `last_post_at` that has a post. That is one seek per forum on `threads_forum_activity`.
   - The last poster comes from `UsersByIDs`.
   - The DTO gets `Forum.LastPost *ForumLastPost{Thread ThreadLink{ID, Slug, Title}, Author, At}`. It is null when the forum has no threads.
7. `GET /api/v1/forums` goes in internal/web/api/forums.go. The body depends on the viewer and on every post, so `Cache-Control: no-store` for now, as for `/me`.
8. Update docs/07's row, dto.ts and dto-contract, and fix the fixture. forums.anon.json currently lists a `members` child, which AC4 forbids.

### Files
- internal/service/forums/tree.go and its test.
- internal/store/db/queries/forums_groups.sql and viewer_state.sql, with sqlc regenerated.
- internal/web/api/forums.go, api.go and forums_test.go.
- cmd/ketju/serve.go.
- internal/dto (dto.go, fixtures and goldens).
- web/client/src/api/dto.ts and dto-contract.ts.
- docs/07.

### Tests
- AC1: the pure builder is fed out-of-order positions and a grandchild row. An HTTP test with nested forums checks the order.
- AC2: a restricted forum with no grant is absent from the raw body by slug, as is a readable child under it. Granting read brings both back.
- AC3: a query-plan assertion that ForumUnread reads no per-post table. Correctness cases:
  - no row, last post before the mark: 0;
  - no row, after the mark: counted;
  - a row behind the last post: counted;
  - a muted thread: not counted.
  The `/me` agreement test runs over a fixture.
- AC4: an anonymous viewer gets only public forums, with unread 0, through the handler.

### Not done
Post totals per forum (the mockup's "3,912 posts") would need a per-forum post sum. Computing it on read scans every thread, so it belongs in `forum_thread_counts` as a migration. Left out, and a draft will be filed.

### Settled in grooming (2026-09-24)

- The forum unread count counts threads with unread posts.
- A child forum under an unreadable parent is hidden.
- Add last-activity fields to dto.Forum; the mockups show them.

## Notes

**agent:claude-code/phase1-run-415af6aa** at 2026-09-25T02:29:20Z

### Built
- `internal/service/forums.Tree` reads `ListForums`, then one `perm.CanEach(read)` batch, then in one read: `ForumThreadCounts`, `ForumUnread` (members only), `ForumLastPosts`, and `UsersByIDs`.
- `GET /api/v1/forums` answers with `no-store`.
- `api.Deps.Forums` is required, and `TestNewRequiresEveryService` covers it.

### Decisions
- **Unread is a new query, not a reuse of MeCounts.** `ForumUnread` is MeCounts's threads count with no participation. MeCounts reads `post_meta` for "participated", and AC3 asks for no per-post rows.
  - Agreement: `TestThreadRuleMatchesPostRule` (service/users) now holds `ForumUnread` equal to both the per-post `UnreadCount` rule and MeCounts, over 60 random seeds. The badge and the index therefore cannot disagree.
  - Mutes are not counted, as in `/me`.
- **thread_count is threads with a post**, from `forum_thread_counts`, so it is the set that unread counts from. A thread with no post counts nowhere.
- **Last post** (settled): `dto.Forum.last_post` is `{thread{id,slug,title}, author, at}`, or null. The per-forum seek is on `threads_forum_activity`. Muting does not affect it: the column says where the forum's activity is.
- **Tree building** takes two passes, so it does not rely on ListForums putting a root before its children. A grandchild, or a child of a missing forum, is logged and left out. The migration 0003 triggers already refuse those.
- **Fixture**: forums.anon.json listed a `members` child, which AC4 forbids. The anonymous fixture is now public-only. It also gained an empty forum, so the null `last_post` is exercised.
- **Post totals** (the mockup's "3,912 posts") are left out: a read-time sum scans threads. Filed as TKT-01M3B6A68B "Add per-forum post totals to the forum index".

### Evidence
- AC1: `TestListed` (service/forums) feeds rows out of order, a grandchild, and a missing parent. `TestForumTreeShapeAndVisibility` inserts out of position order and checks the order of the answer.
- AC2: in the same test, restricted `mods` and `g-secret`, and public `mods-public` under `mods`, are absent from alice's raw body. carol, through a group grant, sees them.
- AC3:
  - `TestForumIndexQueriesScanNoTable` checks that the SQL names no `post_meta` or `posts`, and that the plan scans only CTEs and `forum_thread_counts`.
  - `TestForumTreeCounts` covers: before the mark with no row (read), after it with no row (unread), a row behind (unread), a row at the end (read), muted (not counted), a thread with no post, and a child's threads kept apart.
- AC4: the anonymous viewer case, with unread 0 everywhere.
- Mutants: 18 hand mutants, all killed. They covered the visibility filters, the grandchild check, the member gate, each field, the empty-array cases, Cache-Control, and five SQL edits (mute, the ordinal comparison, the mark filter, the zero-row filter, the last-post order, and the last-post ordinal>0 check).
- `go test -race ./...` passes. vitest: 630 pass. `go generate ./internal/web/spa` leaves `dist` unchanged, because only types changed.

## Summary

GET /api/v1/forums is served by internal/service/forums.Tree. It returns the forums the viewer may read: roots in position order with one level of children. Anything unreadable is dropped along with its children. Each forum carries thread_count (threads with a post), the viewer's unread threads, and last_post. Unread uses the new ForumUnread query, which is held equal to /me's count and to the per-post rule, and reads no per-post row. Post totals are deferred to TKT-01M3B6A68B.
