---
schema: 3
id: TKT-01M29CA9AXKRTS6KMN5CTH7461
title: Implement the thread page endpoint
type: task
status: ready
status_reason: null
priority: urgent
due_on: null
labels:
  - area:api
  - effort:L
assignees: []
milestone: phase-1
parent: TKT-01M29BKBMCNDSYHYF9WXAPDNRN
origin: null
dependencies:
  - TKT-01M29C801M9WBEAVCCC6AXNZ4Z
  - TKT-01M29C0WDJ9FETR8EFZ7ED5JYF
  - TKT-01M29C0WF04HEBYMB9DXCB9WCB
  - TKT-01M29C5Y2EVNCBADSQYW12F01D
  - TKT-01M29BW9NWKJGM3ZQ2DJH8WPWH
blocks_on: none
references:
  - ref: design:api-thread-shape
    path: docs/07-api-and-clients.md
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

The load-bearing endpoint: `GET /threads/{id}?page=&lens=&after_log_length=` returning the thread header, a page of rendered posts, lens metadata, and the viewer's read position.

The flow from docs/02: resolve the viewer, check permissions, open the thread, apply the lens to the in-memory index, fetch and render the selected posts through the post cache, then layer on viewer data. The viewer block is the only per-user part, and keeping it the only one is what makes the response cacheable across members.

`ETag` is `(thread_id, log_length, renderer_version)` with `Vary: Cookie`, and `If-None-Match` is honored. The log length is the thread's version number, available without a database query, which is what makes this cheap.

## Acceptance criteria

- [ ] The response matches the documented shape in docs/07, with a golden test per viewer class
- [ ] ETag is derived from thread id, log length, and renderer version, and a matching If-None-Match returns 304
- [ ] A permalink to an ordinal resolves to the page containing it
- [ ] `after_log_length` returns only posts past that point, which is what the SSE client uses to reconcile
- [ ] Deleted posts return tombstones for members and original content for moderators

## Implementation plan

Pre-groom plan (2026-09-24). Revise at claim, after reading the code as it stands then.

### Approach
1. `service/threads.View(ctx, viewer, id, q ViewQuery{Page, Lens, AfterLogLength, Ordinal}) (dto.ThreadPage, meta, error)`, following the docs/02 "Read a thread page" flow. Parse the id with `threadid.Parse`; a malformed id maps to `bad-request` through the existing internal/problem table. Load the `threads` row with sqlc, and answer `not-found` without `read`, from `perm.Evaluator.Can` (TKT-01M29C5Y2E).
2. Open the thread with `threadcache.Cache.With(id, fn)`. Read the log length first, then take the index snapshot. This needs a new `Actor.LogLength() int64` accessor (atomic) in internal/store/threadlog; today the value is unexported. Reading in that order means a response never claims a newer version than it contains.
3. Ordinals: compute them from `Index.Len()` in pages of the viewer's page size (default 25; docs/06 allows 10–50). Lenses go through a seam `lens.Apply(ix, viewer, spec) ([]uint32, *dto.Lens, error)`. This ticket ships only the identity lens; TKT-01M29CRJ1P owns the framework, and an unknown lens is `bad-request`. `?ordinal=N` resolves to the page containing N within the chosen lens. With no page and a read position, the viewer lands on the page containing `last_read_ordinal+1` (docs/06).
4. `after_log_length=L`: return only entries whose `Offset >= L`, which covers new posts and edits past L.
5. Posts come from `Thread.Reader.Range`, and HTML from `render.Cache.Get/Put(render.Key{Thread, Ordinal, Revision, Version: render.Version})` with a miss rendered by `render.RenderWith`. Resolvers are index-backed for quotes and users-backed for mentions. Authors come from one sqlc `UsersByIDs` per page.
6. Derived fields:
   - `reply_to_suppressed` is `reply_to == ordinal-1`, and `machine_authored` is `EntryMachineAuthored`.
   - `reply_count` comes from one pass over the index. Add `Index.ReplyCounts(ordinals)` instead of calling `Replies` 25 times.
7. Tombstones: members and anon get `deleted:true` with empty HTML. Moderators, meaning `Can(moderate)` on the forum, get the original content. That needs a new reader call that reads tombstoned bodies, for example `Reader.RangeAll`, because `Range` returns them with no source.
8. Viewer block: sqlc `ReadPosition` plus `Watch` for the viewer, `can_reply = Can(reply) && (!locked || can_moderate)`, and `can_moderate`.
9. Handler: set `ETag: "<base32 id>-<log_length>-r<render.Version>"` and `Vary: Cookie`. An `If-None-Match` that matches returns 304 with no body.

### Files
internal/service/threads/view.go, internal/service/lens/lens.go (seam only), internal/web/api/thread.go, internal/store/threadlog/{actor.go,reader.go,index.go} (the three small additions above). Reuses `dto.ThreadPage` and friends and the goldens thread.{anon,member,moderator}.json and thread-locked.member.json.

### Tests
- AC1: goldens exist per viewer class. Add handler tests over a seeded temp data dir plus an in-memory DB that decode strictly into `dto.ThreadPage` and diff against expected values per class.
- AC2: unit test for the ETag string format. Handler test: a matching If-None-Match returns 304; after an append, the old ETag returns 200. AC3: ordinal 26 at page size 25 lands on page 2, and ordinals 25 and 1 land on page 1. Also with page size 10.
- AC4: seed 30 posts, record L, append 3, request `after_log_length=L`, and get exactly those 3. Add an edit to an old post and assert it is included too. Run with `-race` against concurrent appends.
- AC5: a deleted post appears as a tombstone to member and anon and with content to a moderator.

### Design
Reread docs/02 "Read a thread page", docs/07 "Thread response shape", docs/03 "Read path" and "Caching", and docs/06 "Paged, not infinite", "Landing on return", and "Post anatomy".

### Interactions
Consumes TKT-01M29C0WDJ (read path), TKT-01M29C0WF0 (render cache), TKT-01M29C5Y2E, TKT-01M29BYYJ8, and TKT-01M29BYYHQ. TKT-01M29CCBY0 wraps this. TKT-01M29CRJ1P plugs into the lens seam. TKT-01M29CA9C2 and TKT-01M29CA9B9 reuse the post assembler (`posts.Assemble(thread, entries, viewerClass)`).
Open: the ETag omits the viewer block and the viewer class, so a 304 can hand back a stale read position, or member content to someone just promoted to moderator. Also open: the header comes from the async `threads` projection. It can lag the `log_length` it is served under (see TKT-01M29CCBY0). Also open: "land on last read" was decided, while docs/06 says the page holding `last_read_ordinal+1`. The two differ at a page boundary. Also open: `?ordinal=` is not in the docs/07 query list. `after_log_length` cannot surface a Delete or Restore, because neither moves an entry's offset.

### Settled in grooming (2026-09-24)

- See the grooming note on the ticket. When page is absent the server picks the landing page. Permalinks use ?ordinal=. The ETag includes the viewer class and read position. Deletes and restores trigger a page refetch, not after_log_length.

## Notes

**agent:claude/phase1-groom** at 2026-09-24T05:17:38Z

Settled during Phase 1 grooming (2026-09-24), and written into docs/07 in this ticket's own PR:
1. When page is absent, the server picks the landing page. For a signed-in viewer with a read position, that is the page containing last_read_ordinal + 1 (docs/06). Otherwise it is page 1.
2. The permalink criterion uses ?ordinal=N, which resolves to that ordinal's page.
3. The ETag also includes the viewer class (anonymous, member, moderator) and the viewer's read position. Otherwise a 304 could hand a moderator a member's cached page, or serve a stale divider.
4. after_log_length does not report deletes and restores, because those change posts at or before that point. The SSE client refetches the page on a moderation event instead.
