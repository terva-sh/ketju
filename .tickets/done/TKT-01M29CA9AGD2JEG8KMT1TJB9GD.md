---
schema: 3
id: TKT-01M29CA9AGD2JEG8KMT1TJB9GD
title: Implement the thread list endpoint
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
  - TKT-01M29C2WW5N8TR3V76VEB3TGK7
blocks_on: none
references:
  - ref: design:thread-lists
    path: docs/06-thread-views.md
  - ref: pr:84
    path: null
claim: null
archive: null
created_at: 2026-09-11T23:19:48Z
updated_at: 2026-09-25T03:07:49Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase1-run-415af6aa
  name: ""
extensions: {}
---

## Description

`GET /forums/{slug}/threads` with cursor paging: pinned threads first, then by `last_post_at` descending, each row carrying title, tags, author, reply count, last poster, last post time, and the viewer's unread badge count.

Cursor rather than offset paging, because the order is time and a thread bumping between page requests would otherwise duplicate or skip rows. `?sort=created` stays available for announcement forums.

## Acceptance criteria

- [x] Pinned threads sort ahead of the rest and respect a pin expiry
- [x] A cursor is stable across a thread being bumped mid-pagination, with no duplicate or skipped row
- [x] Unread badge counts are per viewer and correct for a thread with no read_positions row
- [x] Rows come from the threads projection with no thread log opened

## Implementation plan

Revised at claim (2026-09-25), after reading the code.

### Approach
1. **Where the SQL lives.** Hand-written `database/sql`, as docs/04 requires for cursor-paged view queries, in a new `internal/store/db/views` beside the generated set:
   - `ThreadList(ctx, tx, ListParams)` is the forum's unpinned rows after a cursor.
   - `PinnedThreads(ctx, tx, forumID, userID, now)` is the pinned rows.
   - They share one column list and one scanner, so TKT-01M29CTY7K's cross-forum lists can reuse them.
   - Only `threads`, `read_positions` and `users` are read, plus `post_meta` for the one unread case below. No thread log is opened.
2. **Order** (settled):
   - The first page opens with pins.
     - Global pins come first, from every forum the viewer may read. The foreign forums are filtered by one `CanEach` batch.
     - This forum's forum-scoped pins come next.
     - Each group is ordered `last_post_at DESC, id DESC`.
   - A pin whose `pinned_until` has passed counts as unpinned: it sorts with the rest and carries `pinned: null`.
   - The rest is keyset-paged on `(last_post_at, id)` descending, or `(created_at, id)` for `?sort=created`. It excludes this forum's actively pinned threads.
   - Pins do not count toward the page size of 25 (settled).
3. **Cursor.** The cursor is base64url of `sort|forum|key|id|pins` (revised after the review on PR #84).
   - A cursor is a position, not a credential. It is not signed, and a well-formed one written by hand is honoured.
   - A cursor that is malformed, or was made for another forum or the other sort, is `bad-request`, as is an unknown `sort`.
   - `pins` is the forum's threads that the first page showed as pins; later pages leave exactly those out. The pin block is capped at 50, so the cursor is bounded, and the ids reach SQL as one JSON parameter.
   - `next_cursor` is empty at the end; the query fetches 26 rows to know.
   - A bumped thread moves above the cursor. Rows are never duplicated, and no row that stays below the cursor is skipped (settled).
4. **Unread badge**, in SQL. It is the per-post rule of docs/04 (`UnreadCount`) for each row:
   - 0 when the read position is at or past the last post;
   - `last_post_ordinal - last_read_ordinal` with no mark;
   - 0 when the last post is at or before the mark;
   - otherwise a count of `post_meta` past the position and after the mark. That is a primary-key range bounded by the unread posts.
   `last_read_ordinal` is the row's value or 0. An anonymous viewer gets 0 and 0. A muted thread still shows its count in its own forum's list; muting affects notifications and the cross-forum badges.
5. **Access.** Access follows the thread page (`access`):
   - an unknown or hidden forum is `not-found`;
   - a forum that needs sign-in is `unauthenticated` for an anonymous viewer;
   - otherwise, a member without read gets `forbidden`.
   A thread with no post yet is not listed. `reply_count` is `post_count - 1`. Tags come from `thread_tags`, and authors from `UsersByIDs`.
6. **Endpoint.** `GET /api/v1/forums/{slug}/threads?cursor=&sort=` answers with `no-store`.
7. **Indexes.** Migration 0008 adds `threads_forum_created (forum_id, created_at)` and a partial `threads_pinned` index on `(pinned_scope, forum_id) WHERE pinned_scope IS NOT NULL`, so neither sort nor the pins scans threads. A plan test holds this.

### Tests
- AC1: forum, global, expired, foreign-global and hidden-foreign-global pins, with the clock on each side of `pinned_until`.
- AC2: page, bump a page-2 row, page again. No id repeats, and every row still below the cursor appears. Also a concurrent-writer run under `-race`.
- AC3:
  - Table cases: no row with the mark before and after the last post, a row behind, a row at the end, a mark in the middle of a thread.
  - A random test holds the badge equal to `UnreadCount` for every row.
- AC4: the service has no thread cache (nil `Threads`), so opening a log would panic. `views` does not import threadlog or threadcache.
- Malformed input: a malformed cursor, a cursor for another forum or the other sort, one over the pin cap, and an unknown sort are each `bad-request`. A hand-written, well-formed cursor is honoured.

### Settled in grooming (2026-09-24)

- A list page holds 25 threads, the same constant as posts.
- Use a keyset cursor on (last_post_at, id). A thread bumped mid-paging may reappear, but no row is skipped.
- Global pins sort first in every forum (docs/06).

## Notes

**agent:claude-code/phase1-run-415af6aa** at 2026-09-25T02:46:04Z

### Built
- `internal/store/db/views` is new. It holds hand-written `database/sql` view queries, per docs/04:
  - `ThreadList`, keyset-paged and sorted by activity or creation;
  - `PinnedThreads`;
  - a shared row scanner that TKT-01M29CTY7K can reuse.
- `threads.Service.List` and `cursor.go` form the service. `GET /api/v1/forums/{slug}/threads` answers with `no-store`.
- Migration 0008 adds `threads_forum_created` and a partial index, `threads_pinned`.
- `TagsForThreads` is a new sqlc query.
- `forumForThread` now uses the shared `readForum`, which admits a viewer the way `access` does.

### Decisions
- **Global pins** (settled): they head every forum's first page, from forums the viewer may read, filtered by one `CanEach` batch. The forum's own pins follow, and each group is by latest activity. A pin is over at `pinned_until` (exclusive); the thread then sorts with the rest and has `pinned: null`. Pins do not count toward the 25.
- **Cursor**: base64url without padding of `a|at|id` or `c|at|id`. The sort is carried in the cursor, so replaying it against the other sort is `bad-request`. Only the canonical spelling is accepted. The dto fixture's example now matches the real encoding.
- **Unread** is exact: it is `UnreadCount`'s per-post rule, computed in SQL. `post_meta` is read only for a thread with posts on both sides of the mark. A muted thread still shows its count in its own forum's list.
- **Access** answers as the thread page does. `TestThreadListAccess` checks this for every forum and viewer pair against `GET /threads/{id}`. A suspended member is served as anonymous by perm.

### Evidence
- AC1: `TestPins` (views) covers now at 499 and at 500 for a pin ending at 500. `TestThreadListPins` covers forum, expiring, foreign-global and hidden-foreign-global pins, per viewer, before and after the expiry.
- AC2:
  - `TestKeysetSurvivesBumps`: 40 seeds × 2 sorts, with ties. No repeats. Every thread not seen was bumped, and the created sort misses none.
  - `TestKeysetUnderConcurrentBumps`: a real concurrent writer under `-race`.
  - `TestThreadListPaging`: 26 then 5 rows, exactly 25 gives no cursor, and a bump between pages moves the thread up.
- AC3: `TestUnreadMatchesUnreadCount` (60 random seeds against `UnreadCount`) and `TestThreadListRow` (a position behind, no row, anonymous).
- AC4:
  - `TestThreadListOpensNoLog` serves the list, pins included, from a service with no thread cache.
  - `TestViewsReadNoLogs`: views imports no threadlog or threadcache.
  - `TestPlansSeek`: every list plan seeks an index with no sort step, with and without a cursor, and the pins scan nothing.
- Tamper: `TestThreadListBadRequests` has nine cases.
- Mutants: 29 run.
  - Survivors that led to fixes: exactly 25 rows now gives no cursor; a row with no tags now serializes `[]`; a redundant viewer normalisation was removed, with a test pinning the behaviour.
  - Equivalent: two CASE shortcuts (`<=` versus `<` on the read position and on the mark), and the cursor's explicit sort-tag check, which the canonical re-encode also enforces.
  - All others were killed.
- `go test -race ./...` passes. No client change, so dist is unchanged.

**agent:claude-code/phase1-run-415af6aa** at 2026-09-25T02:54:00Z

### Review 1 on PR #84 (head a876f4c, run 434): two medium findings, both fixed

1. **Fabricated or cross-forum cursors were accepted**, contradicting the claim that a cursor the server did not issue is refused.
   - Fix: the cursor now carries the forum id, and `decodeCursor` checks it against the requested forum, so a cursor from another forum is `bad-request`.
   - The claim is narrowed, in docs/07 and the code comments: a cursor is a position, not a credential.
   - Signing (HMAC) was considered and rejected: it needs a server key and its management, and hides nothing a reader cannot already read. A hand-written, well-formed cursor is honoured, and a test says so.
2. **A pin expiring between pages could appear twice.** The same was true of a pin removed between pages.
   - Fix: the first page's cursor carries the ids of this forum's threads it showed as pins. Later pages exclude exactly those ids and no longer apply the time-based pin filter.
   - So a pin that expires or is removed is not repeated, and a thread pinned after page 1 stays in its natural place instead of being skipped.
   - Evidence: `TestThreadListPinsChangeBetweenPages` expires one pin, removes one, and adds one between pages, with no repeats and no skip. A plan case covers a later page with pins (index seek, no sort).
- The cursor format is now `sort|forum|at|id|pins`, and the dto fixture's example is regenerated from the real encoder (`TestCursorRoundTrip`).
- Mutants on the fix: 6, all killed. The zero-pin check needed a new case.

**agent:claude-code/phase1-run-415af6aa** at 2026-09-25T03:01:29Z

### Review 2 on PR #84 (head 010b64a, run 436): finding 2 of review 1 resolved; two medium findings

1. **Edited cursors are accepted.** This is intended, and the claim is now narrowed wherever it was still broader.
   - The ticket's plan still said "an edited cursor ... is bad-request". It now says a cursor is a position, not a credential.
   - docs/07, the PR body and the code comments were already narrowed after review 1.
   - Signing remains rejected: it needs a server key, and hides nothing a reader could not read.
2. **Unbounded pin ids reached SQL as one parameter each.** Fixed:
   - The ids are one JSON parameter, through `json_each`.
   - `views.MaxPinned` (50) caps the pin block, and `decodeCursor` refuses a cursor carrying more.
   - Every page, the first included, now leaves out exactly the forum's threads shown as pins. Before, the first page left out every active pin, so pins past the cap would have vanished from the first page. The list query no longer takes a time.
   - Evidence: `TestThreadListPinCap` (60 pins: 50 at the top, all 60 once across the pages), the "too many pins" bad-request case, and a plan case for a later page with pins (the index seek stays; json_each scans only the parameter).
   - Mutants: 5, all killed.

**agent:claude-code/phase1-run-415af6aa** at 2026-09-25T03:07:49Z

### Review 3 on PR #84 (head f2e1f83, run 438): both findings of review 2 resolved; one medium finding, fixed

**The pin cap was applied before the access filter.** The SQL `LIMIT` cut global pins across every forum, and unreadable ones were dropped only afterwards. Hidden-forum pins could therefore crowd out readable pins, and push this forum's own pins down into the rows.

- Fix: `PinnedThreads` has no limit. It reads only pinned rows through the partial index, and only moderators create those.
- `List` then filters foreign global pins by `CanEach` and keeps the first `views.MaxPinned`. The rows are read after, in a second read, leaving out exactly the pins shown, so a pin changing between the two reads cannot duplicate a thread.
- Evidence: `TestThreadListPinCapAfterAccess` has 55 hidden global pins, all newer than alice's two visible pins. alice gets exactly those two, both badged. carol, who may read the hidden forum, hits the cap. Putting the SQL `LIMIT` back as a mutant fails the test, and so does removing the Go cap.

## Summary

GET /api/v1/forums/{slug}/threads is served by threads.Service.List over the new hand-written internal/store/db/views queries. Pins open the first page (global from readable forums, then the forum's own), and an expired pin sorts as unpinned. After them come 25 rows per page, keyset-paged on (last_post_at|created_at, id) with an opaque cursor bound to its sort. Each row carries the exact per-post unread count. Everything is answered from projections through indexes (migration 0008); no thread log is opened.
