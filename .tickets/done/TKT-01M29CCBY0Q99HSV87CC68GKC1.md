---
schema: 3
id: TKT-01M29CCBY0Q99HSV87CC68GKC1
title: Implement the response cache keyed on viewer class
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
  - TKT-01M29CA9AXKRTS6KMN5CTH7461
blocks_on: none
references:
  - ref: design:api-thread-shape
    path: docs/07-api-and-clients.md
claim: null
archive: null
created_at: 2026-09-11T23:20:57Z
updated_at: 2026-09-28T07:38:14Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase1-run-438e26c1
  name: ""
extensions: {}
---

## Description

A byte-bounded LRU of serialized JSON keyed on thread, page, lens, log_length, renderer version, and viewer class, where viewer class is anon, member, or moderator.

The key is invalid by construction when the thread changes, because log_length is in it. No invalidation logic exists and none should be added: a new log length is a new key and the old entry ages out.

Viewer class exists because moderators see tombstone contents. Three classes is the whole per-viewer variation in the cached part, and the read position and own-post markers stay outside it in the viewer block.

## Acceptance criteria

- [x] A mutation changes the log length and therefore the key, with no explicit invalidation call anywhere
- [x] A moderator never receives a cached member response and the reverse, tested
- [ ] The cache is bounded in bytes with its hit ratio exposed as a metric
- [x] Viewer-specific data is never stored in a cached payload

## Implementation plan

Revised at claim (2026-09-28), after reading the code on main at 9b76d9d.

### Approach
1. `internal/service/respcache`: a byte-bounded LRU on `render.Cache`'s pattern (entry overhead charged, an entry larger than the bound refused, `Stats` with `HitRatio`, plus a `Stale` count). It exports `New`, `NewMB`, `Enabled`, `Get`, `Put`, `Stats` and nothing that forgets an entry. Sized from `storage.response_cache_mb` in `ketju serve`.
2. Cache only the posts, as serialized JSON. The header and pagination come from the projection row, forum and tags that `View` reads anyway for access, so caching them saves a small marshal and would pin a forum rename. The viewer block is built per request and never stored. A hit decodes the stored JSON, so no two requests share a mutable `[]dto.Post`.
3. `Key{Thread, LogLength (index snapshot), Projected (threads.log_length_projected), Page (as resolved), PageSize, Lens, Renderer, Class, Authors}`. Class is the one `access` already computes: moderator when `Can(moderate)` on the thread's forum (admins included), member for an active member, anon otherwise.
4. `Authors` is a SHA-256 over the JSON of the page's authors' rows as this request read them (one query the page already makes). This does the job of the settled users-generation counter, and unlike an in-process counter it sees `ketju admin promote` and any other process's writes to `users`.
5. The render dependencies the key cannot name (mention handle lookups, quoted posts' deleted state) are stored with the entry and checked on every hit, as `render.Cache` does. A failed check is a miss plus a Put that replaces the entry. This covers a registration (TKT-01M29C8005) that turns an `@handle` into a link.
6. `after_log_length` requests bypass the cache.

### Tests
- respcache unit: every key field separates; byte bound, LRU order, refusal, hit ratio; replace; stale; copies; disabled/nil; concurrent use under -race; an AST test that the package exports no forgetting method (AC1 structural).
- API (cache on in every test world, so all existing thread tests run through it): six class orders, each page byte-equal to an uncached service's (AC2); edit, delete, restore through the endpoints and a moderator's delete/restore in the log (AC1 functional); promotion through a second Evaluator and a demotion by SQL, with the viewer read as the session does; a registered handle; two members' viewer blocks over one entry and a read beacon (AC4); concurrent readers of all classes while posts land.
- threads internal: the stored bytes hold no viewer field and equal the page's posts (AC4); the authors digest ignores map order and moves with every field.

### Settled in grooming (2026-09-24)

- The cache key includes page size, and the projected log length rather than the raw log length.
- Add a users-generation counter to the key, bumped on any profile change, so a rename does not serve stale names.

How each is carried: the key holds the projected log length beside the index snapshot's length, not instead of it, because the posts are drawn from the index and a post appended but not yet projected would otherwise share a key with the page before it. The users generation is the authors digest in step 4, for the reason given there.

## Notes

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T07:38:04Z

Implementation notes (agent:claude-code/phase1-run-438e26c1, 2026-09-28).

AC3 is left unticked. The cache is bounded in bytes (TestBoundedInBytes) and `respcache.Stats` carries `HitRatio`, reachable from `threads.Service.Responses.Stats()`. Nothing exports it as a metric yet: the metrics endpoint is TKT-01M29CEE7S (Add the metrics endpoint), a phase-4 draft, and building it here is outside Phase 1. That ticket's criteria already name cache hit ratios, so no new draft is filed.

The design (docs/07) leaves no who-can-see-what gap here. Viewer class is the one TKT-01M29CA9AX's access check already computes (moderator = may moderate the thread's forum), and docs/07 names the three classes. The internals settled in this PR are written into docs/07 under "The response cache (TKT-01M29CCBY0)".

The hit check on render dependencies is not invalidation: nothing is removed. An entry whose mention lookups or quoted posts have moved is treated as a miss, and the fresh page's Put replaces it.

Mutations tried against the tests, each caught:
- Class dropped from the key: TestResponseCacheKeepsViewerClassesApart, ...AfterMutations, ...AfterPromotion, ...UnderConcurrency, TestThreadPageGoldens, TestTombstonesByClass, TestCachedPayloadHoldsNoViewer fail.
- Authors digest dropped: TestResponseCacheAfterPromotion, TestETagCoversAuthorsAndForum fail.
- Handle check dropped on hit: TestResponseCacheSeesARegisteredHandle fails.
- LogLength dropped: TestResponseCacheAfterMutations fails.
- PageSize dropped: TestETagCoversPageSize fails.

Local runs: gofmt, go build, go vet, go test -race ./... all pass; Playwright (KETJU_E2E_PORT=8793) 45 passed, 9 skipped.

## Summary

The response cache is in internal/service/respcache and wired into threads.View and ketju serve. It caches a page's posts as JSON per (thread, index log length, projected log length, page, page size, lens, renderer, viewer class, authors digest), checks the posts' mention and quote dependencies on each hit, and has no invalidation. Tests prove class isolation in every request order and after edit, delete, restore, cross-process promotion, demotion and registration. AC3's metric export waits on TKT-01M29CEE7S (Add the metrics endpoint).
