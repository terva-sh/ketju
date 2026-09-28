---
schema: 3
id: TKT-01M29CA9CECTE02DFZGBGN2HC2
title: "Implement the viewer-state endpoints: read, watch, flags, drafts"
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
  - TKT-01M29BYYHQ7PTYNYA3VBBD4PDB
  - TKT-01M29C801M9WBEAVCCC6AXNZ4Z
blocks_on: none
references:
  - ref: design:read-tracking
    path: docs/06-thread-views.md
claim: null
archive: null
created_at: 2026-09-11T23:19:48Z
updated_at: 2026-09-28T06:08:02Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase1-run-438e26c1
  name: ""
extensions: {}
---

## Description

`PUT /threads/{id}/read` and its `DELETE`, `POST /threads/{id}/watch`, `POST /threads/{id}/flags`, and the draft endpoints.

Read position has one rule that is easy to get wrong: the server only ever moves the position forward, unless the user explicitly marks the thread unread. A beacon arriving late, out of order, or from a second device must not drag somebody back to post 12.

Every one of these is also reachable as a form post for the future HTML client, which is why `?mark_read=` on a page load has to be part of the design rather than an afterthought.

## Acceptance criteria

- [x] A read beacon never moves the position backwards, tested with out-of-order arrivals
- [x] An explicit mark-unread is the only way the position decreases
- [x] A draft is upserted per user and thread and survives a reconnect from another device
- [x] Each endpoint has an equivalent form-post shape documented for the HTML client

## Implementation plan

Revised at claim (2026-09-28), after reading main at 09e8f86: the threads service (access, MayWrite, the PR #95 write handlers), the viewer_state queries from TKT-01M29BYYHQ, and docs/04, 06 and 07.

### Scope
Only what the criteria name, as settled in grooming: the read position (PUT and DELETE /threads/{id}/read) and reply drafts (GET, PUT, DELETE /drafts/{thread_id}). The watch and flag endpoints stay with TKT-01M29CRJ29 and TKT-01M29CP2PJ. The new-thread draft (keyed by forum) has no URL under /drafts/{thread_id} and no criterion here, so it is filed as a draft ticket together with the title-column question the BYYHQ note left.

### Approach
1. Service methods on threads.Service (internal/service/threads/viewerstate.go), because access() is the rule every one of them needs and a second copy would drift. MayKeepState is member.Require, asked by the handlers before parsing, as MayWrite is.
2. PUT /threads/{id}/read {ordinal}: member, then access (read). The ordinal must be at least 1 and is clamped to the index's post count (the index, not the projection's post_count, which trails it). The existing UpsertReadPosition takes max() in one statement, and a GetReadPosition in the same write transaction answers the position as it now stands. The answer is 200 dto.ReadPosition {last_read_ordinal}.
3. DELETE /threads/{id}/read, optional {ordinal}: the only path that lowers a position, and it never raises one (min of current and requested; absent means 0). EnsureReadPosition (insert 0 if missing) and LowerReadPosition (UPDATE with CASE) run in one write transaction. Two statements because sqlc v1.31.1 does not bind a parameter inside ON CONFLICT DO UPDATE on SQLite; checked by generating it. The answer is 200 dto.ReadPosition.
4. Drafts: GET answers 200 dto.Draft or 204 when there is none (not-found stays reserved for the thread). PUT {source, reply_to?} upserts the one row per (user, thread) through the drafts_thread partial index and needs reply permission. reply_to must be 1..post count; the source may be empty and is bounded by the body limit. DELETE answers 204 whether or not a draft existed.
5. DTOs ReadPosition and Draft, with goldens read.member, draft.member and draft-plain.member (reply_to set and null).
6. docs/07: the resource rows for these routes, the read contract for the beacon (TKT-01M29CGBWN), and a form-post table in "Path to a non-JS client" covering every viewer-state mutation, including ?mark_read= and the watch and flag routes named for their owning tickets.

### Tests
- AC1: beacons 12, 40, 30, 39 through the API in shuffled order and concurrently (-race, real SQLite file); the final position and every later answer are 40. An ordinal past the last post clamps.
- AC2: after DELETE {ordinal:2} a beacon for 1 does not lower it further and one for 3 raises it; DELETE never raises; DELETE with no body goes to 0.
- AC3: PUT from one session and GET from a second session of the same user returns the same draft; a second PUT keeps one row and advances updated_at.
- AC4: a test parses the docs/07 form-post table and checks that every viewer-state route registered in internal/web/api has a row.
- Refusals: anonymous unauthenticated, pre-user forbidden, hidden thread not-found, a member without reply forbidden on PUT draft, reply_to past the end validation-failed.

### Settled in grooming (2026-09-24)

- Implement only what the criteria name. Watch and flag endpoints overlap Phase 2 tickets TKT-01M29CRJ29 and TKT-01M29CP2PJ, so leave those to them.
- DELETE on a watch falls back to the default level.
- The draft for a new thread is keyed by forum (see BYYHQ).

## Notes

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T09:06:42Z

From TKT-01M29BYYHQ (viewer-state schema): the drafts table has no title column, because docs/04 lists none. A new-thread draft therefore autosaves the body but not the thread title. Decide here whether it needs one; adding it is a one-line migration. Read positions only move forward (UpsertReadPosition takes max). Unread counts use UnreadCount(user, thread), whose rule is in docs/04 read_positions.

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T06:07:40Z

Gaps settled while building, each an internals or API-shape question the nearest document answered, written into docs/07 in the same PR (docs/12 "Gaps the design leaves open"):

- **Package.** The methods live on threads.Service (internal/service/threads/viewerstate.go), not a new viewerstate package, because access() is the rule all five share. Alternative: export access, which would put the read rule in two packages that could drift.
- **PUT /read answers the stored position**, `dto.ReadPosition {last_read_ordinal}`, which can be past the ordinal sent. The existing one-statement max() upsert stays; GetReadPosition runs in the same write transaction (one write connection, BEGIN IMMEDIATE), so no beacon lands between the two. Alternative: change UpsertReadPosition to RETURNING, which rippled into five callers including cmd/ketju/e2e.go.
- **Clamp to the index's post count**, not threads.post_count: the projection trails the index by the bus, and a client shown a post may report it at once.
- **DELETE /read never raises the position** (min of stored and requested; no body means 0). The pre-groom plan said "set to the ordinal"; setting exactly would let a mark-unread mark posts read. It is two statements (EnsureReadPosition, then LowerReadPosition with CASE) because sqlc v1.31.1 leaves a parameter inside ON CONFLICT DO UPDATE unbound on SQLite; I generated it to check. Note also that a non-ASCII character in a query comment shifts sqlc's rewrite offsets and breaks every later query in the file.
- **Late beacon after mark-unread** moves the position forward again; the server cannot tell it from a new read. docs/07 tells the client to flush before a DELETE. This is for TKT-01M29CGBWN.
- **GET /drafts with no draft is 204**, not not-found, which stays reserved for a thread the viewer cannot see. DELETE is 204 whether or not a draft existed.
- **Saving a draft needs reply** in the forum; reading and deleting one need only read. The source may be empty (a draft holding only a reply target); reply_to must be 1..post count.
- **Scope.** Watch and flags are not built (settled in grooming). docs/07's form-post table names their form shapes and the tickets that own them.
- **New-thread drafts** are filed as TKT-01M3K9XBJA (Autosave new-thread drafts, and decide whether the title is saved), because whether the title autosaves is a product decision. docs/04 now points there.

Mutations tried against the tests, all caught: last-write-wins instead of max() (3 tests); DELETE setting the ordinal exactly; no clamp; no reply check on SaveDraft; no EnsureReadPosition; reply_to bound off by one; a docs/07 table row removed.

## Summary

Built the read position and reply drafts; watch and flags stay with TKT-01M29CRJ29 and TKT-01M29CP2PJ, as settled in grooming.

- `PUT /api/v1/threads/{id}/read {ordinal}` moves the position forward only (one-statement max() upsert), clamps to the index's post count, and answers 200 `dto.ReadPosition` with the stored position. `DELETE` with an optional `{ordinal}` is the only path down and never raises it.
- `GET/PUT/DELETE /api/v1/drafts/{thread_id}`: one draft per user per thread; GET is 200 `dto.Draft` or 204; PUT needs reply; DELETE is 204 always.
- Service methods on threads.Service (viewerstate.go) behind access(); new queries EnsureReadPosition, LowerReadPosition, GetThreadDraft, UpsertThreadDraft, DeleteThreadDraft; goldens read, draft, draft-plain.
- docs/07: resource rows, "Read position" and "Drafts" contracts for the beacon (TKT-01M29CGBWN) and composer, and a "Form posts for viewer state" table that a test holds against the served routes. docs/04 points the new-thread draft at TKT-01M3K9XBJA.
