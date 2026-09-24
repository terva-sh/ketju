---
schema: 3
id: TKT-01M29CCBY0Q99HSV87CC68GKC1
title: Implement the response cache keyed on viewer class
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
  - TKT-01M29CA9AXKRTS6KMN5CTH7461
blocks_on: none
references:
  - ref: design:api-thread-shape
    path: docs/07-api-and-clients.md
claim: null
archive: null
created_at: 2026-09-11T23:20:57Z
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

A byte-bounded LRU of serialized JSON keyed on thread, page, lens, log_length, renderer version, and viewer class, where viewer class is anon, member, or moderator.

The key is invalid by construction when the thread changes, because log_length is in it. No invalidation logic exists and none should be added: a new log length is a new key and the old entry ages out.

Viewer class exists because moderators see tombstone contents. Three classes is the whole per-viewer variation in the cached part, and the read position and own-post markers stay outside it in the viewer block.

## Acceptance criteria

- [ ] A mutation changes the log length and therefore the key, with no explicit invalidation call anywhere
- [ ] A moderator never receives a cached member response and the reverse, tested
- [ ] The cache is bounded in bytes with its hit ratio exposed as a metric
- [ ] Viewer-specific data is never stored in a cached payload

## Implementation plan

Pre-groom plan (2026-09-24). Revise at claim, after reading the code as it stands then.

### Approach
1. Create `internal/service/respcache`: a byte-bounded LRU modelled on `render.Cache` (internal/render/cache.go), with the same `Stats`/`HitRatio` shape, entry overhead accounting, and refusal of an entry larger than the bound. Size it from `config.Storage.ResponseCacheMB` (default 64, already in internal/config).
2. `Key{Thread threadid.ID, Page, PageSize int, Lens string, LogLength int64, RendererVersion int, Class ViewerClass}`. `ViewerClass` is `anon`, `member`, or `moderator`, computed per request from the Viewer and `Can(moderate)` on the thread's forum. An admin is `moderator`. PageSize is included on purpose; see the open item below.
3. Value: the non-viewer parts of `dto.ThreadPage` pre-marshalled as `json.RawMessage` (thread, page, lens, posts). On a hit, the handler marshals a wrapper struct with the same field order and tags as `dto.ThreadPage`, with the RawMessages plus a freshly built `viewer`. The wire bytes then equal a direct marshal, and viewer data is never stored.
4. Wire it into `service/threads.View` from TKT-01M29CA9AX: `Get`, or on a miss compute, `Put`, then attach the viewer. Requests with `after_log_length` bypass the cache.
5. Invalidation: none, as the ticket requires. The one exception: do not `Put` while `threads.log_length_projected < LogLength`. A header built from a lagging projection would otherwise be pinned under a key that never changes.
6. Expose `Stats()` for the metrics endpoint (TKT-01M29CEE7S), including `HitRatio`.

### Files
internal/service/respcache/{cache.go,cache_test.go}, internal/service/threads/view.go (wiring), cmd wiring in `ketju serve` (TKT-01M29CCBYX).

### Tests
- AC1: a grep/AST test that nothing outside respcache calls a delete or invalidate method, and the package exports none. Functional check: append a post; the next request misses because `LogLength` moved.
- AC2: populate as a member, request as a moderator for the same thread and page, and assert a miss and tombstone content present. Then the reverse. Run under `-race` with interleaved requests.
- AC3: fill past the bound; `Bytes <= Max`, evictions counted, `HitRatio` matches the counted hits and misses.
- AC4: two members with different read positions get identical cached bytes, and each response's `viewer` block is their own. A test also asserts the stored value holds no `"viewer"` key.

### Design
Reread docs/07 "Thread response shape" (the ETag and cache-key paragraph), docs/03 "Caching", and the TKT-01M29BKBMC epic description.

### Interactions
Consumes TKT-01M29CA9AX (View, viewer class), TKT-01M29C5Y2E (moderate check), and config. TKT-01M29CEE7S consumes `Stats`.
Open: the docs/07 key omits page size, but docs/06 lets a user choose 10–50, so page N differs by viewer. The plan adds PageSize to the key.
Open: author display data (handle, display_name, avatar, role) is baked into cached posts. A rename, a role change, or an anonymization (TKT-01M29CKRFS) stays visible until the thread next changes. That conflicts with "no invalidation logic ever" and matters most for anonymization.

### Settled in grooming (2026-09-24)

- The cache key includes page size, and the projected log length rather than the raw log length.
- Add a users-generation counter to the key, bumped on any profile change, so a rename does not serve stale names.
