---
schema: 3
id: TKT-01M29CA9AGD2JEG8KMT1TJB9GD
title: Implement the thread list endpoint
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
  - TKT-01M29C801M9WBEAVCCC6AXNZ4Z
  - TKT-01M29C2WW5N8TR3V76VEB3TGK7
blocks_on: none
references:
  - ref: design:thread-lists
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

`GET /forums/{slug}/threads` with cursor paging: pinned threads first, then by `last_post_at` descending, each row carrying title, tags, author, reply count, last poster, last post time, and the viewer's unread badge count.

Cursor rather than offset paging, because the order is time and a thread bumping between page requests would otherwise duplicate or skip rows. `?sort=created` stays available for announcement forums.

## Acceptance criteria

- [ ] Pinned threads sort ahead of the rest and respect a pin expiry
- [ ] A cursor is stable across a thread being bumped mid-pagination, with no duplicate or skipped row
- [ ] Unread badge counts are per viewer and correct for a thread with no read_positions row
- [ ] Rows come from the threads projection with no thread log opened

## Implementation plan

Pre-groom plan (2026-09-24). Revise at claim, after reading the code as it stands then.

### Approach
1. `service/threads.List(ctx, viewer, forumSlug, cursor, sort) (dto.ThreadList, error)`. Resolve the forum by slug; if the viewer lacks `read` (TKT-01M29C5Y2E), answer `not-found`, never `forbidden`, matching the restricted-is-invisible rule.
2. The query is hand-written `database/sql`, because the shape is cursor- and sort-dependent (docs/04: dynamic view queries stay beside the generated set). It reads only the `threads` projection, `thread_tags`, `users` for author and last poster, and a LEFT JOIN of `read_positions` for the badge. No thread log is opened.
3. Order has two phases. Phase 1 is threads pinned in this forum with `pinned_until IS NULL OR pinned_until > now`, sent on the first page only. Phase 2 is the rest, ordered `(last_post_at DESC, id DESC)`, or `(created_at DESC, id DESC)` for `?sort=created`. An expired pin sorts as unpinned, and the row carries `pinned: null`.
4. The cursor is opaque: base64url of `{sort, last_post_at|created_at, id}`. Keyset paging is `WHERE (k, id) < (?, ?)`. A thread bumped mid-pagination moves above the cursor and cannot reappear lower down, so there are no duplicates.
5. Unread badge: `unread = post_count - last_read_ordinal` when a row exists. With no row: 0 if `last_post_at < mark_all_read_at`, else `post_count` (docs/06). Anonymous viewers get 0 and 0.
6. Page size: use a fixed list size of 25 until docs say otherwise. The 25 decided in docs/07 is the thread-post page size.

### Files
internal/service/threads/list.go, internal/web/api/threads_list.go, internal/service/threads/cursor.go. Reuses `dto.ThreadList`, `dto.ThreadRow`, `dto.Pinned`, and `dto.Author`, with the goldens threads.anon.json and threads.member.json.

### Tests
- AC1: seed threads with a forum pin, an expired pin, and a global pin. The ordering assertion runs with a fake clock straddling `pinned_until`.
- AC2: page 1, then bump a thread that sits on page 2 (update `last_post_at` in the projection), then fetch page 2. Assert no id repeats across pages and every thread still below the cursor appears. Run with `-race` and a concurrent writer.
- AC3: a viewer with no read_positions row, with mark_all_read_at before and after last_post_at, plus a viewer with a row. Table-driven.
- AC4: a test with `threadcache` wired to a root that does not exist (or a counting fake) asserts zero Acquire calls.
- Tamper test: a cursor edited by hand is `bad-request`.

### Design
Reread docs/06 "Thread list views", docs/07 "JSON API" (cursor paging) and "Resources", and docs/04 "threads" and "read_positions".

### Interactions
Consumes TKT-01M29C2WW5 (the writer that keeps `threads` current), TKT-01M29BYYJ8, TKT-01M29BYYHQ, and TKT-01M29C5Y2E. The same query shape serves `/unread` and the other cross-forum lists (TKT-01M29CTY7K), so keep the row scanner reusable.
Open: "no skipped row" is ambiguous for a bumped thread, which correctly leaves the lower pages. The test defines "skipped" as a row still below the cursor.
Open: whether `pinned_scope='global'` pins appear in every forum's list.
Open: the list page size is undocumented.

### Settled in grooming (2026-09-24)

- A list page holds 25 threads, the same constant as posts.
- Use a keyset cursor on (last_post_at, id). A thread bumped mid-paging may reappear, but no row is skipped.
- Global pins sort first in every forum (docs/06).
