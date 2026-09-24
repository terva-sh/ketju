---
schema: 3
id: TKT-01M29CA9CECTE02DFZGBGN2HC2
title: "Implement the viewer-state endpoints: read, watch, flags, drafts"
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
  - TKT-01M29BYYHQ7PTYNYA3VBBD4PDB
  - TKT-01M29C801M9WBEAVCCC6AXNZ4Z
blocks_on: none
references:
  - ref: design:read-tracking
    path: docs/06-thread-views.md
claim: null
archive: null
created_at: 2026-09-11T23:19:48Z
updated_at: 2026-09-24T09:06:42Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase1-run-415af6aa
  name: ""
extensions: {}
---

## Description

`PUT /threads/{id}/read` and its `DELETE`, `POST /threads/{id}/watch`, `POST /threads/{id}/flags`, and the draft endpoints.

Read position has one rule that is easy to get wrong: the server only ever moves the position forward, unless the user explicitly marks the thread unread. A beacon arriving late, out of order, or from a second device must not drag somebody back to post 12.

Every one of these is also reachable as a form post for the future HTML client, which is why `?mark_read=` on a page load has to be part of the design rather than an afterthought.

## Acceptance criteria

- [ ] A read beacon never moves the position backwards, tested with out-of-order arrivals
- [ ] An explicit mark-unread is the only way the position decreases
- [ ] A draft is upserted per user and thread and survives a reconnect from another device
- [ ] Each endpoint has an equivalent form-post shape documented for the HTML client

## Implementation plan

Pre-groom plan (2026-09-24). Revise at claim, after reading the code as it stands then.

### Approach
1. `PUT /api/v1/threads/{id}/read {ordinal}`: require `Member()` and `Can(read)`. Clamp the ordinal to `Index.Len()` (or `threads.post_count`). Write with a single sqlc upsert: `INSERT … ON CONFLICT(user_id, thread_id) DO UPDATE SET last_read_ordinal = max(last_read_ordinal, excluded.last_read_ordinal), last_read_at = … WHERE excluded.last_read_ordinal > last_read_ordinal`. Monotonicity lives in the SQL, so concurrent beacons cannot race a read-then-write.
2. `DELETE /api/v1/threads/{id}/read`, optionally `{ordinal}`, is the only statement that lowers the position: it sets `last_read_ordinal` to the given ordinal or 0. It writes a row rather than deleting one, because a missing row falls back to `mark_all_read_at` and could read as "read" again.
3. `POST /api/v1/threads/{id}/watch {level}`: an upsert into `watches` with level `watch` or `mute`. Clearing it is undefined in docs; see below.
4. `POST /api/v1/threads/{id}/flags {ordinal, reason, note}`: insert into `flags` with reason in {spam, abuse, off-topic, other} (docs/08). Reply 202 or 204 with no detail, matching "reporters see thanks and nothing else". The auto-hide threshold is TKT-01M29CP2PJ.
5. `GET/PUT/DELETE /api/v1/drafts/{thread_id}`: a sqlc upsert keyed (user_id, thread_id) holding `{source, reply_to, updated_at}`. Refuse a source over the post limit.
6. Form-post shapes: add a table to docs/07 "Path to a non-JS client" mapping each endpoint to its future HTML form route and fields. Include `?mark_read=N` on the thread page GET, and state that it shares the forward-only upsert.

### Files
internal/service/viewerstate/{read.go,watch.go,flags.go,drafts.go}, internal/web/api/viewerstate.go, sqlc query file(s), internal/dto (`Draft`, `ReadPosition` + goldens), docs/07 (form-post table).

### Tests
- AC1: fire beacons with 12, 40, 30, and 39 in random order and concurrently (`-race`, real SQLite file); the final position is 40. A beacon beyond `post_count` clamps.
- AC2: after DELETE with ordinal 5, a beacon for 3 does not move it back to 3. After DELETE, a later beacon of 6 advances to 6.
- AC3: PUT a draft from session A, GET it from session B for the same user (a second cookie), and the content is equal. PUT again and `updated_at` advances with one row.
- AC4: a doc test or checklist that every endpoint above has a row in the docs/07 table. A small test parses the table and compares it with the route list registered in internal/web/api.

### Design
Reread docs/06 "Read tracking" and "Composer" (drafts), docs/07 "Resources" and "Path to a non-JS client", docs/08 "Flags", and docs/04 "watches", "read_positions", "flags", and "drafts".

### Interactions
Consumes TKT-01M29BYYHQ (read_positions, watches, drafts), TKT-01M29BYYJ0 (flags), and TKT-01M29C5Y2E. Consumed by TKT-01M29CGBWN (beacon) and TKT-01M29CEE79 (composer). Overlaps TKT-01M29CRJ29 (watch semantics) and TKT-01M29CP2PJ (flags), so agree on who owns each endpoint's rules.
Open: how to clear a watch (a `none` level, or DELETE) is undocumented.
Open: the new-thread draft (null thread_id plus forum_id) has no URL under `/drafts/{thread_id}`. A nullable PRIMARY KEY column in SQLite also permits duplicate NULL rows.
Open: the `DELETE /read` semantics (to 0, or to a given ordinal) are undocumented.

### Settled in grooming (2026-09-24)

- Implement only what the criteria name. Watch and flag endpoints overlap Phase 2 tickets TKT-01M29CRJ29 and TKT-01M29CP2PJ, so leave those to them.
- DELETE on a watch falls back to the default level.
- The draft for a new thread is keyed by forum (see BYYHQ).

## Notes

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T09:06:42Z

From TKT-01M29BYYHQ (viewer-state schema): the drafts table has no title column, because docs/04 lists none. A new-thread draft therefore autosaves the body but not the thread title. Decide here whether it needs one; adding it is a one-line migration. Read positions only move forward (UpsertReadPosition takes max). Unread counts use UnreadCount(user, thread), whose rule is in docs/04 read_positions.
