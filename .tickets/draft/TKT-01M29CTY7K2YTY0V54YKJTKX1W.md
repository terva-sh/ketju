---
schema: 3
id: TKT-01M29CTY7K2YTY0V54YKJTKX1W
title: Build the cross-forum unread, new, participated, and watching lists
type: task
status: draft
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
claim: null
archive: null
created_at: 2026-09-11T23:28:54Z
updated_at: 2026-10-01T07:54:18Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase3-groom-unread
  name: ""
extensions: {}
---

## Description

`/unread`, `/new`, `/participated`, and `/watching`: the "what's new" surfaces that live in the top navigation on desktop and in the bottom tab bar on phones.

These are the queries most likely to get slow. Unread across all forums combines read_positions, mark_all_read_at, thread post counts, and permission filtering, and it runs on every visit from every member. Get the query shape right here rather than discovering it at a thousand threads.

## Acceptance criteria

- [ ] Each list is permission-filtered when served: a forum the viewer cannot read, a thread moved into one, a forum made restricted and a group grant removed from a moderator each drop rows on the next request (TestCrossListsArePermissionFiltered)
- [ ] No list row carries a post body or a quarantined thread, and a thread with a removed or redacted last post shows only what its thread page shows (TestListRowsCarryNoHiddenPost)
- [ ] Unread is correct for a user with no read_positions rows, relying on mark_all_read_at (TestUnreadWithNoReadPositions)
- [ ] Query cost is bounded and measured at ten thousand threads, with the numbers recorded in docs/04 'Sizing sanity check' (BenchmarkCrossLists10k, TestCrossListsScanNoTable)
- [ ] Mark all read at forum and site level works and is reflected in /me and the lists on the next request, never moving a position back (TestMarkAllRead)
- [ ] Every /me unread badge, the new count included, equals its list's matching rows (TestBadgesMatchLists); TestMeCountsScansNoTable and TestMeBudget still pass
- [ ] The six routes refuse non-members, page by cursor, and are documented in docs/07 with form-post rows for the two mark-read routes (TestCrossListRefusals, TestCrossListCursor, TestEveryViewerStateRouteHasAFormPost)

## Implementation plan

### Settled in grooming

- **What each list holds** (docs/06 "Thread list views", run decision 2026-10-01):
  - *Unread*: threads with an unread post by `/me`'s rule, most recent activity first.
  - *New*: threads the viewer has never opened (no read position) that were started after their site-level mark, newest started first.
  - *Participated*: threads where the viewer has a post that was not moved, read or unread, by activity.
  - *Watching*: threads with a thread-level `watch`, by activity.

  A thread whose effective level is `mute` is in none of them. Forum-level watches drive notifications only.
- **Badges agree with lists.** The TKT-01M29CA99R note of 2026-09-24 says the lists use `MeCounts`'s rule, or change `MeCounts` in the same PR. Here `MeCounts` keeps its rule and gains a `new` count for the New badge that docs/06 "Mobile" asks for.
- **Mark all read** (docs/06 "Read tracking", run decision 2026-10-01). The site level sets `users.mark_all_read_at`, forward only. The forum level moves each of the forum's thread positions to the thread's last post, forward only, in one statement.
- **Mute and precedence** come from TKT-01M29CRJ29 (docs/04 `watches`). This ticket depends on it.
- **Response shape.** `dto.ThreadList` already says it is the cross-forum surfaces' response with `forum: null` (`internal/dto/dto.go`). Paging is a cursor keyset, as for the forum list (docs/07).
- **The screens, the New badge and the "Mark all read" buttons** are TKT-01M3V6Y4JP (Build the unread, new, participated and watching screens), split out in grooming.

### Approach

1. **Views** in `internal/store/db/views/crosslist.go`: `CrossList(ctx, tx, CrossParams{List, UserID, Forums []int64, After *Key, Limit})`.
   - It reuses `rowColumns`, `scanRows` and `Key` from `threadlist.go`, plus the effective-level column TKT-01M29CRJ29 adds.
   - `Forums` is bound as one `json_each(?)` parameter, as `Pinned` is, so no viewer reaches SQLite's limit on bound parameters.
   - Each list needs `t.last_post_ordinal > 0`, a readable forum, and an effective level other than `mute`. Then:
     - **Unread**: `t.last_post_at > mark` and `t.last_post_ordinal > coalesce(rp.last_read_ordinal, 0)`, keyed on `(last_post_at, id)`.
     - **New**: no `read_positions` row and `t.created_at > mark`, keyed on `(created_at, id)`.
     - **Participated**: driven from `SELECT DISTINCT thread_id FROM post_meta WHERE author_id = ? AND moved = 0` (index `post_meta (author_id, created_at)`), keyed on `(last_post_at, id)`.
     - **Watching**: driven from the viewer's thread-level `watch` rows, keyed on `(last_post_at, id)`.
2. **Cost.** First measure the plans at 10 000 threads in 50 forums, with 1 000 read positions and 100 watches. If Unread or New sorts every qualifying thread rather than seeking, add a migration at the next free number (0020 or later, checked at implementation) with `threads_activity ON threads (last_post_at)` and `threads_created ON threads (created_at)`, both ending in the rowid. Record the numbers before and after in docs/04 "Sizing sanity check" and in a ticket note.
3. **Service** `threads.Service.CrossList(ctx, v, list, cursor)`, in `internal/service/threads/crosslist.go`:
   - It requires a member (`member.Require`). The forums come from `s.Perm.ReadableForums(ctx, v)`.
   - The cursor reuses `cursor`, with the list's name bound in. A malformed cursor, or one from another list or a forum's list, is `bad-request`.
   - Quarantined threads (`s.Quarantined`) are dropped. `listDetails` supplies tags and authors, and one `ForumsByID` read names each row's forum.
   - The answer is `dto.ThreadList{Forum: nil}`, 25 to a page (`ListPageSize`).
4. **DTO.** `dto.ThreadRow` gains `forum *ForumRef`, set on cross-forum rows and null on a forum's own list. `dto.UnreadCounts` gains `new`. Regenerate the goldens (dto fixtures, `me.*`, the thread list, and the four new `crosslist.*.json`).
5. **`/me`.** `MeCounts` gains a per-forum `new` column by the New rule, in the same statement. With no mark it starts from `forum_thread_counts.threads`. With a mark it starts from a count over `threads_forum_created` after the mark. From that start it subtracts the threads the viewer has opened, and the muted ones, from the viewer's own rows, as `threads` already does. `users.Me` sums it over readable forums.
6. **Mark all read.**
   - `PUT /api/v1/me/read` sets `users.mark_all_read_at = max(coalesce(mark, 0), now)` through a new `SetMarkAllRead`.
   - `PUT /api/v1/forums/{slug}/read` admits the viewer with `readForum`. It runs `INSERT INTO read_positions SELECT ?, id, last_post_ordinal, ? FROM threads WHERE forum_id = ? AND last_post_ordinal > 0 ON CONFLICT DO UPDATE` to the larger ordinal (`UpsertReadPosition`'s rule).
   - Both answer 200 `dto.MarkedRead{at}`, are `no-store`, and join `unchargedPaths`.
7. **Routes**: `GET /api/v1/unread`, `/new`, `/participated` and `/watching`, each with `?cursor=` and `no-store`. Anonymous is `unauthenticated` and a pre-user `forbidden`.
8. **docs/07**:
   - one resources row for the four lists, replacing the one-line row, and rows for the two mark-read routes;
   - `/me`'s `unread.new`;
   - form-post rows `PUT /api/v1/me/read` to `POST /read` and `PUT /api/v1/forums/{slug}/read` to `POST /f/{slug}/read`, both served in `TestEveryViewerStateRouteHasAFormPost`.

### Tests

- `TestCrossListsArePermissionFiltered` (api), each case across all four lists:
  - a restricted forum the viewer has no grant for;
  - a thread moved into such a forum;
  - a forum made restricted (`forum.visibility_changed`);
  - a moderator by group grant (docs/08 "Decided 2026-09-30"), who sees the forum's threads, and sees none of them on the next request once removed from the group (perm's cache cleared by the group write).
- `TestListRowsCarryNoHiddenPost`: no row carries a post body. A thread whose last post was hidden or hard-redacted shows only what its thread page's tombstone shows (author and time, no text). A redacted title shows the replacement. A quarantined thread is absent.
- `TestUnreadWithNoReadPositions`: a member with no `read_positions` rows sees exactly the threads with a post after their mark, and every thread with a post when they have no mark.
- `TestBadgesMatchLists`, one seeded world with read, unread, never-opened, participated, watched, thread-muted, forum-muted and restricted threads:
  - every page of `/unread` counted equals `me.unread.threads`;
  - `/new` equals `me.unread.new`;
  - the unread rows of `/participated` equal `me.unread.participated`;
  - the unread rows of `/watching` equal `me.unread.watching`.
- `TestMarkAllRead`. Site and forum level each clear the matching rows from `/me` and the lists on the next request. The forum level leaves other forums unread. Neither moves a position or the mark back. A hidden forum is `not-found`.
- `TestCrossListCursor`: keyset paging, a thread bumped between pages not shown twice, and a cursor from another list or from a forum's list `bad-request`.
- `TestCrossListsScanNoTable` (`internal/store/db`, as `TestMeCountsScansNoTable` does): the plans read no `post_meta` or `threads` table scan and use the named indexes.
- `BenchmarkCrossLists10k`: the numbers recorded in docs/04.
- `TestMeCountsScansNoTable` and `TestMeBudget` still pass with `new`. `TestUnreadMatchesUnreadCount` still holds.
- `TestCrossListRefusals`: anonymous `unauthenticated` and pre-user `forbidden`. `TestEveryViewerStateRouteHasAFormPost` gains the two mark-read routes.

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
