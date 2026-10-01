---
schema: 3
id: TKT-01M29CTY7K2YTY0V54YKJTKX1W
title: Build the cross-forum unread, new, participated, and watching lists
type: task
status: done
status_reason: null
priority: high
due_on: null
labels:
  - area:views
  - effort:L
assignees: []
milestone: phase-3
parent: TKT-01M29BN36D34YVKVRPNQD3PHDV
origin: null
dependencies:
  - TKT-01M29BYYHQ7PTYNYA3VBBD4PDB
  - TKT-01M29CRJ29H6TZPN0X42SEW5RN
blocks_on: none
references:
  - ref: design:thread-lists
    path: docs/06-thread-views.md
  - ref: pr:294
    path: null
  - ref: pr:324
    path: null
claim: null
archive: null
created_at: 2026-09-11T23:28:54Z
updated_at: 2026-10-01T15:32:48Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase3-cty7k
  name: ""
extensions: {}
---

## Description

`/unread`, `/new`, `/participated`, and `/watching`: the "what's new" surfaces that live in the top navigation on desktop and in the bottom tab bar on phones.

These are the queries most likely to get slow. Unread across all forums combines read_positions, mark_all_read_at, thread post counts, and permission filtering, and it runs on every visit from every member. Get the query shape right here rather than discovering it at a thousand threads.

## Acceptance criteria

- [x] Each list is permission-filtered when served: a forum the viewer cannot read, a thread moved into one, a forum made restricted and a group grant removed from a moderator each drop rows on the next request (TestCrossListsArePermissionFiltered)
- [x] No list row carries a post body or a quarantined thread, and a thread with a removed or redacted last post shows only what its thread page shows (TestListRowsCarryNoHiddenPost)
- [x] Unread is correct for a user with no read_positions rows, relying on mark_all_read_at (TestUnreadWithNoReadPositions)
- [x] Query cost is bounded and measured at ten thousand threads, with the numbers recorded in docs/04 'Sizing sanity check' (BenchmarkCrossLists10k, TestCrossListsScanNoTable)
- [x] Mark all read at forum and site level works and is reflected in /me and the lists on the next request, never moving a position back (TestMarkAllRead)
- [x] Every /me unread badge, the new count included, equals its list's matching rows (TestBadgesMatchLists); TestMeCountsScansNoTable and TestMeBudget still pass
- [x] The six routes refuse non-members, page by cursor, and are documented in docs/07 with form-post rows for the two mark-read routes (TestCrossListRefusals, TestCrossListCursor, TestEveryViewerStateRouteHasAFormPost)

## Implementation plan

### Settled in grooming

- **What each list holds** (docs/06 "Thread list views", run decision 2026-10-01):
  - *Unread*: threads with an unread post by `/me`'s rule, most recent activity first.
  - *New*: threads the viewer has never opened (no read position) that were started after their site-level mark, newest started first.
  - *Participated*: threads where the viewer has a post that was not moved, read or unread, by activity.
  - *Watching*: threads with a thread-level `watch`, by activity.

  A thread whose effective level is `mute` is in none of them. Forum-level watches drive notifications only.
- **Badges agree with lists.** The TKT-01M29CA99R note of 2026-09-24 says the lists use `MeCounts`'s rule, or change `MeCounts` in the same PR. Here `MeCounts` gains a `new` count for the New badge that docs/06 "Mobile" asks for. Its `mine` CTE also takes `moved = 0`, so Participated means one thing in the badge and the list (review 1729).
- **Mark all read** (docs/06 "Read tracking", run decision 2026-10-01). The site level sets `users.mark_all_read_at`, forward only. The forum level moves each of the forum's thread positions to the thread's last post, forward only, in one statement.
- **Mute and precedence** come from TKT-01M29CRJ29 (docs/04 `watches`). This ticket depends on it.
- **Response shape.** `dto.ThreadList` already says it is the cross-forum surfaces' response with `forum: null` (`internal/dto/dto.go`). Paging is a cursor keyset, as for the forum list (docs/07).
- **The screens, the New badge and the "Mark all read" buttons** are TKT-01M3V6Y4JP (Build the unread, new, participated and watching screens), split out in grooming.

### Binding requirements carried from notes

- Review 1751, finding 1: the New count starts from threads with a post (`last_post_ordinal > 0`), in the count after the mark and in the subtraction. `TestBadgesMatchLists` has a thread with no post and no read position, in neither `/new` nor `unread.new`.
- Review 1751, finding 2: the docs/06 "Read tracking" bullet that has the forum level set `users.mark_all_read_at` is rewritten to the settled rule, keeping the dated decision as its record.
- From the unread lens grooming (TKT-01M29CTY4J "Out of scope"): mark all read moves the landing page, the divider, first unread and the unread lens together.

### Approach (revised at claim, 2026-10-01, against main at 04224e4)

1. **Migration 0023** (`threads_activity (last_post_at)`, `threads_created (created_at)`, `threads_forum_fresh (forum_id, created_at, last_post_ordinal)`). Measured: a cross-forum list in activity order cannot seek a per-forum index without sorting every qualifying thread, so Unread and New walk a site-wide index. The walk is bounded below by the viewer's mark, bound as a scalar subquery so SQLite uses it as the index range, and above by the cursor. `threads_forum_fresh` makes MeCounts's `new` count after a mark read index entries only.
2. **Views** `internal/store/db/views/crosslist.go`: `CrossList(ctx, tx, CrossParams{List, UserID, Forums, After, Limit})`. It reuses `rowColumns`, `scanRows` and `Key`, with `Forums` as one `json_each(?)` parameter. Every list needs `last_post_ordinal > 0`, a forum in `Forums`, and an effective level other than `mute`. Unread: `last_post_at > mark` and `last_post_ordinal > rp`. New: no `read_positions` row and `created_at > mark`. Participated: `t.id IN (post_meta by author, moved = 0)`. Watching: `t.id IN (watches by user, level watch)`, through `watches_thread`. `Memberships(ids)` says which lists given threads are in, for /me's quarantine correction.
3. **Read boundary.** The thread page, its bare unread lens, `PUT/DELETE /threads/{id}/read` answers and every list row's `last_read_ordinal` use one integer, `dbq.ReadBoundary`: the stored position moved to just before the first post past it written after the mark, or to the last post when there is none. `rowColumns` computes the same column; `TestUnreadMatchesUnreadCount` holds the two together. When the mark moved the boundary, `viewer.last_read_at` is the mark. Run decision, recorded in docs/06.
4. **Service** `threads.Service.CrossList(ctx, v, list, cursor)`: `member.Require`, `perm.ReadableForums`, a cursor `base64url(list|at|id)` (another list's or a forum's is `bad-request`), quarantined rows dropped after the cursor is taken, `listDetails` and a new `ForumsByIDs` for each row's forum. `MarkAllRead` (`SetMarkAllRead`, forward only) and `MarkForumRead` (`readForum`, then `MarkForumRead`, one upsert that moves only rows forward and keeps the time on rows it does not move; threads already read by the mark get no row).
5. **DTO**: `ThreadRow.forum`, `UnreadCounts.new`, `MarkedRead{at}`. Client `dto.ts` and `dto-contract.ts` type-only; the dist is unchanged.
6. **/me**: `MeCounts` gains `new` and `moved = 0` in `mine`. `users.Me` sums `new`, and takes quarantined threads (`projection.Writer.QuarantinedIDs`) out of every count through `views.Memberships`, a statement only when a thread is quarantined.
7. **Routes**: `GET /api/v1/unread`, `/new`, `/participated`, `/watching` with `?cursor=`; `PUT /api/v1/me/read`, `PUT /api/v1/forums/{slug}/read`. All `no-store`; the two writes join `unchargedPaths`.
8. **Docs**: 04 (indexes, sizing numbers), 06 (mark bullet, boundary decision), 07 (rows for the six routes, `/me`'s `unread.new`, `ThreadRow.forum`, form-post rows).

### Tests

- `TestCrossListsArePermissionFiltered` (api), across all four lists: a restricted forum without a grant, a thread moved into one, a forum made restricted, a moderator by group grant who loses the group.
- `TestListRowsCarryNoHiddenPost`: no row carries a post body; a hidden or redacted last post shows what the forum list shows; a quarantined thread is absent.
- `TestUnreadWithNoReadPositions`.
- `TestBadgesMatchLists`: read, unread, never opened, participated, a moved-only post, watched, thread-muted, forum-muted with a thread watch, restricted, a thread with no post, and a quarantined thread; every list counted page by page equals its badge.
- `TestMarkAllRead`: site and forum level clear the matching rows from /me and the lists, the forum level leaves other forums, neither moves back, a hidden forum is not-found; after each, the bare unread lens, the divider, the landing page and the read answers agree.
- `TestCrossListCursor`, `TestCrossListRefusals`, `TestCrossListsStayOutOfTheResponseCache`.
- `TestCrossListsScanNoTable`, `TestCrossListRules`, `BenchmarkCrossLists10k` (views); `TestUnreadMatchesUnreadCount` extended to the boundary; `TestMeCountsScansNoTable`, `TestMeBudget`, `TestEveryViewerStateRouteHasAFormPost`.

### Out of scope

- The four screens, the New badge, and the "Mark all read" controls: TKT-01M3V6Y4JP.
- Watch and mute routes and the precedence: TKT-01M29CRJ29.
- Live refresh of the lists over SSE: TKT-01M29CRJ3X and TKT-01M29CTYA.
- Tag filtering of agent threads out of Unread (docs/06 "Live updates" mentions tags): thread tags are Phase 4.

## Notes

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T11:59:38Z

From TKT-01M29CA99R (Implement GET /me): the unread badges in /me come from the MeCounts query (internal/store/db/queries/viewer_state.sql). A thread is unread when its newest post is past the read position and after mark_all_read_at. participated means the viewer has a post in the thread. watching means a thread-level 'watch'. A thread-level mute removes the thread from all three. Forum-level watches are not counted. The lists this ticket builds should use the same rule, or share the query, so a badge never disagrees with its list. If the lists settle the forum-watch question differently, change MeCounts in the same PR.

**agent:claude-code/phase3-groom-unread** at 2026-10-01T07:54:18Z

run decision: pending maintainer review. Two decisions bind this ticket, recorded in docs/06 on 2026-10-01. (1) The four lists, under 'Thread list views'. New means threads you have never opened that were started after your site-level mark, and /me gains unread.new for its badge. Participated means threads with a post of yours that was not moved, read or unread. Watching means thread-level watches only. A muted thread is in no list. (2) Mark all read, under 'Read tracking'. Only the site level sets users.mark_all_read_at. The forum level moves that forum's read positions to each thread's last post, forward only. docs/06 read literally had the forum level set the site-wide mark, which contradicts 'forum level'; this is the safer reading. The screens and the Mark all read buttons were split out to TKT-01M3V6Y4JP (Build the unread, new, participated and watching screens).

**agent:claude-code/phase3-groom-unread** at 2026-10-01T10:03:57Z

Review 1729 (head 1c66ce7, base f3538e6), medium 'Keep the Participated list and badge on the same moved-post rule', fixed in the plan. MeCounts's mine CTE takes moved = 0, the list's predicate, and TestBadgesMatchLists covers a thread whose only post by the viewer was moved. This supersedes the 2026-09-24 note's 'participated means the viewer has a post in the thread': a moved post no longer counts. Round 4 of 5.

**agent:claude-code/phase3-groom-unread** at 2026-10-01T11:52:55Z

Requirement carried from review 1751 (2026-10-01; PR #294, head 728a981, base 0da0411). Finding 1 (medium, not security): 'Exclude empty threads from the New badge count'. The /new list requires t.last_post_ordinal > 0, but the plan's /me new count starts from forum_thread_counts.threads, or from a created-at count after the mark, and subtracts only opened and muted threads. What the implementation must do: the new count's starting set must apply the list's exact predicate, threads with last_post_ordinal > 0. forum_thread_counts.threads already counts only threads with a post (docs/04), but the created-at count over threads_forum_created must add AND t.last_post_ordinal > 0, and the subtraction must use the same predicate. TestBadgesMatchLists must include a thread with no post and no read position, which counts in neither /new nor unread.new. Finding 2 (low): 'Replace the superseded forum mark-all-read rule'. docs/06 'Read tracking' still has the original bullet 'Forum and site level mark all read set users.mark_all_read_at' above the run decision that replaces it. What the implementation must do: in the PR that builds the two mark-read routes, rewrite that bullet to state the settled rule, which is that the site level sets users.mark_all_read_at and the forum level moves that forum's read positions forward, and keep the dated decision line as its record.

**agent:claude-code/phase3-cty7k** at 2026-10-01T15:23:54Z

run decision: pending maintainer review. The read boundary (docs/06 "Read tracking", recorded 2026-10-01). Nothing set users.mark_all_read_at before this ticket, and the thread page, its divider, first unread and the unread lens all read last_read_ordinal alone (TKT-01M29CTY4J "Out of scope" handed the folding to this ticket). The page now reads one integer, dbq ReadBoundary: the stored position moved to just before the first post past it written after the site-level mark, or to the last post when there is none. The landing page, the divider, the bare unread lens (unread:<boundary>), every list row's last_read_ordinal and the PUT/DELETE /threads/{id}/read answers all use it. When the mark moved the boundary, viewer.last_read_at is the mark's time. Alternatives that lost: leaving the page on the stored position (a thread the badge calls read would open on post 1 under "N new posts below"); writing every thread's position on a site-level mark (a row per thread per member, which the mark exists to avoid); keeping the row's time beside a boundary the mark set (it names a place the reader is no longer shown). A second, smaller decision rides with it: the forum-level mark writes no row for a thread already read by the site-level mark, and a row it does not move keeps its last_read_at.

**agent:claude-code/phase3-cty7k** at 2026-10-01T15:24:05Z

Implementation notes for the next reader (2026-10-01).

Access. Every list is filtered by perm.ReadableForums on the request, so a restricted forum, a thread moved into one, a forum made restricted and a group grant removed each drop rows on the next request; TestCrossListsArePermissionFiltered checks all four lists and the /me badges at each step. A quarantined thread is dropped from every list after the cursor is taken. MeCounts cannot see quarantine, so users.Me now takes quarantined threads back out of every count through views.Memberships, which runs only when a thread is quarantined (users.Service.Quarantined, wired to projection.Writer.QuarantinedIDs in serve), so TestMeBudget is unchanged on a healthy site. TestBadgesMatchLists covers read, unread, never opened, participated, a moved-only post, watched, thread-muted, forum-muted with a thread watch, restricted, an empty thread before and after the mark (review 1751), and a quarantined thread, before and after both marks.

Cache. The lists and both mark routes are no-store and never reach the response cache: TestCrossListsStayOutOfTheResponseCache.

Benchmark (BenchmarkCrossLists10k, 10 000 threads, 50 forums): a first page is 0.5 to 1.2 ms for every list with or without a mark; the walk's worst case (no mark, everything but the 30 oldest read) is about 10 ms for /unread and /new. MeCounts went from about 5 ms to 5.4 to 6 ms with new added; it grows with the reader's own read positions (about 31 ms before and 37 ms after at 9 970 positions), as it did before this ticket. Numbers in docs/04 "Sizing sanity check".

Migration 0023 adds threads_activity, threads_created and threads_forum_fresh. Mutations tried against the tests, each caught: dropping last_post_ordinal > 0 from the fresh count, dropping moved = 0 from mine, dropping the forum filter, the quarantine drop and subtraction, the mute filter, the boundary in the view, and the forward-only WHERE on the forum mark.

## Summary

The four cross-forum lists and both levels of mark all read are served. GET /api/v1/unread, /new, /participated and /watching answer dto.ThreadList with forum null, 25 to a page, keyset-paged by a cursor bound to its list, each row naming its forum. The rules are views.CrossList's and match /me's badges: MeCounts gained new (with the has-a-post predicate review 1751 asked for) and moved = 0 in mine, and users.Me takes quarantined threads out of every count. Lists are filtered by perm.ReadableForums on every request, drop muted and quarantined threads, carry no post body, and never reach the response cache. PUT /api/v1/me/read sets users.mark_all_read_at forward only; PUT /api/v1/forums/{slug}/read moves that forum's positions forward in one statement. The thread page, its bare unread lens, list rows and the read routes share one read boundary (dbq ReadBoundary), so a mark moves the landing page, divider, first unread and lens together (run decision in docs/06). Migration 0023 adds threads_activity, threads_created and threads_forum_fresh; BenchmarkCrossLists10k's numbers are in docs/04. docs/07 documents the six routes, unread.new, ThreadRow.forum and the two form-post rows. The screens and buttons are TKT-01M3V6Y4JP.
