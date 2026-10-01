---
schema: 3
id: TKT-01M29CRJ1PY355KX3HX7A5N785
title: Build the lens framework over the in-memory index
type: task
status: in-progress
status_reason: null
priority: high
due_on: null
labels:
  - area:views
  - effort:M
assignees: []
milestone: phase-3
parent: TKT-01M29BN36D34YVKVRPNQD3PHDV
origin: null
dependencies:
  - TKT-01M29BYYGYZ8FQB2W2XC14S2T3
  - TKT-01M29CA9AXKRTS6KMN5CTH7461
blocks_on: none
references:
  - ref: design:lenses
    path: docs/06-thread-views.md
  - ref: pr:293
    path: null
claim:
  actor: agent:claude-code/phase3-crj1p
  branch: run/crj1p-lens-framework
  worktree: /home/sothr/workspace/git.local.sothr.com/terva-sh/ketju/.claude/worktrees/agent-a687b806d2a6e30c7
  commit: 3f1884b94717e1f95d536c39a0e1883a6f0c3eec
  session: null
  claimed_at: 2026-10-01T11:39:44Z
  expires_at: null
archive: null
created_at: 2026-09-11T23:27:36Z
updated_at: 2026-10-01T11:39:45Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase3-crj1p
  name: ""
extensions: {}
---

## Description

One interface: given a thread's in-memory index and a viewer, return the ordinals to show, the count hidden, and any gap markers. Paging then applies to the filtered sequence.

A lens filters and never reorders. That constraint belongs in the interface itself, so a lens returns a selection of ordinals rather than a list of posts it could have arranged. The type makes the rule unbreakable instead of merely documented.

The `?lens=` parameter parses into a lens here, which is what makes every lens shareable by URL and available to the future HTML client for free.

## Acceptance criteria

- [ ] A lens is a keep-or-skip filter the framework applies in one ascending walk of the in-memory index (Index.Select), and lens.Selection has no exported way to hold ordinals in any other order; TestIndexSelectAscending holds every selection strictly ascending over random indexes
- [ ] lens.Parse is total (FuzzParseLens), and an unknown, malformed or not-yet-served lens serves the canonical page with lens: null, never an error (TestUnknownLensServesCanonical)
- [ ] A lens page answers lens.shown, lens.hidden (post count minus shown) and lens.gaps, fixed by goldens in internal/dto and internal/web/api
- [ ] Paging applies to the lens's posts: page and total_pages count them, ?ordinal= finds its lens page, and an append leaves every earlier lens page unchanged (TestSelectionPagesStableUnderAppend)
- [ ] A lens never widens access: TestLensPostsMatchCanonical holds every post a lens serves equal to the canonical page's for the same viewer, across anonymous, member, author, moderator by role, moderator by group grant, admin and suspended viewers, and removed, redacted, hidden-pending-review, moved and suspended-author posts
- [ ] The response cache key carries the resolved lens and a digest of the page's ordinals, and two selections on the same page number never share an entry (TestLensResponseCacheKeysSelection)
- [ ] A lens page runs the canonical page's statements whatever the thread's length, counted through db.Options.Trace

## Implementation plan

Planned 2026-10-01 against main at a599b48 (Phase 3 grooming, lane `lenses`).

### Settled in grooming

- Lenses are computed server-side from the thread's in-memory index, and are URL state (docs/06 "Lenses"; docs/07 "Path to a non-JS client" point 3). `Query.Lens` already reaches `threads.View` from `threadQuery` in `internal/web/api/api.go`, and `dto.Lens {kind, value, shown, hidden}` is already in `internal/dto/dto.go`.
- A lens filters and never reorders (docs/06 "The stance").
- The lens grammar, the fallback to the canonical page for an unknown value, and the paging rules are in docs/07 "What the thread endpoint does", under "Decided by the run (2026-10-01, pending maintainer review): the `lens` parameter".
- What each lens keeps of removed posts is in docs/06 "Lenses", under "Decided by the run (2026-10-01): what each lens keeps of removed posts".
- Access is the canonical page's and comes first: `threads.access` runs before the lens is resolved. A thread hidden from the viewer is `not-found` whatever the lens says (docs/07 "Access").
- A removed post's body follows `revealRemoved` and `pageClass`, unchanged (docs/07 "Removed posts", TKT-01M29CKRJ9). A lens only chooses ordinals. It never decides what a post shows.
- The index format does not change (docs/03 "Index entry"). Nothing here touches the log format.

### Approach

1. **`threadlog.Index.Select`.** Add `func (ix *Index) Select(keep func(ordinal uint32, e IndexEntry) bool) []uint32` in `internal/store/threadlog/index.go`. It makes one ascending walk under the read lock and appends each kept ordinal. `ByAuthor` and `Since` become wrappers over it. The doc comment says that `keep` must not call back into the `Index`: the read lock is held, and a `sync.RWMutex` read lock deadlocks against a waiting writer when it is taken twice. A lens that needs a second look at the index, such as the chain's ancestor walk or the mentions lens's own-posts set, takes it before calling `Select`.
2. **Package `internal/service/threads/lens`.**
   - `Parse(raw string) Spec` is total. `Spec{Kind, Ordinal, Handle, SinceMS, Bare}` follows the decided grammar. Anything else parses to `Spec{Kind: Canonical}`.
   - `Filter` is an interface with one unexported method, `keep(ordinal uint32, e threadlog.IndexEntry) bool`, so only this package can implement it. The constructors (`Author`, `Unread`, `Chain`, `Since`, `Mentions`, `Mod`) arrive with their tickets. This ticket adds none, and an export_test.go filter (every odd ordinal) drives its tests.
   - `Apply(ix *threadlog.Index, f Filter) Selection` calls `ix.Select`. `Selection` holds its ordinals and the post count in unexported fields. Nothing outside the package can build or reorder one, which is what makes a reordering lens impossible to express.
   - `Selection` has these methods:
     - `Len()` and `Hidden()`, the post count minus `Len`.
     - `Page(n, size)`, which returns that page's ordinals, or `not-found` past the last page. An empty selection has one empty page.
     - `PageOf(ordinal, size)`, which returns the page holding that ordinal, or else the page holding the first selected ordinal after it, or else the last page.
     - `Gaps(n, size) []dto.LensGap`, the `{after, count}` gaps that touch page n, the gap after the last post of the last page included.
     - `Digest() [32]byte`, over the page's ordinals.
3. **Registration.** A `map[Kind]resolver` in `internal/service/threads/lenses.go` maps each served kind to the code that builds its `Filter` for a viewer. A kind with no resolver yet serves the canonical page with `lens: null`, so this ticket can land before any lens. Each lens ticket adds its row.
4. **`threads.View`.**
   - With `Spec.Kind == Canonical`, `View` keeps its current path byte for byte and answers `lens: null`.
   - Otherwise `View` runs `access` first, then the resolver's database reads (a handle, the mentions set), outside the actor.
   - Inside `th.Actor.Stable` it applies the filter to `th.Index` and picks the page. A permalink uses `PageOf`, then `Page`, and the landing rule does not apply. It then fills the snapshot with `snap.take(th.Index, logLength, pageOrdinals)`.
   - The page answers `dto.Pagination` over the selection and `Body.Lens = &dto.Lens{Kind, Value, Shown, Hidden, Gaps}`. Posts come from the same `pagePosts`/`postsDeps` path as the canonical page.
   - `after_log_length` filters the lens page's posts exactly as it filters a canonical page's.
   - Remove the `bad-request` for a non-empty `q.Lens`.
5. **DTO.** Add `Gaps []LensGap` (`{after, count}`, json `gaps`) to `dto.Lens`. Regenerate `internal/dto/testdata/golden` with a lens page, and add `internal/web/api/testdata/golden/thread.lens.json`, served with the test filter registered.
6. **Response cache.** Only the posts are cached (docs/07 "The response cache"). `dto.Lens`, like the header and the viewer block, is built on every request and never stored, so the key needs only what the posts differ by. `respcache.Key` keeps `Lens` and fills it with the resolved form (`unread:60`, `author:<user id>`, `mentions:<viewer id>`, `chain:42`, `since:<ms>`, `mod`). It gains `Selection [32]byte`, the page's ordinal digest. A lens whose selection depends on something its string does not name can then never serve one viewer's page to another. `pageClass` reads the lens page's snapshot as it reads the canonical one.
7. **Docs.** In docs/07 "What the thread endpoint does", replace the `lens ... is bad-request` sentence with a pointer to the decided grammar. In "The response cache", note the `Selection` field.

### Tests

- `TestIndexSelectAscending` (threadlog) is a property test over random indexes: the result is strictly ascending, and equal to a filter written by hand. `TestByAuthorAndSinceUnchanged`.
- `TestParseLensTotal` is a table of every grammar form and its malformed neighbours. `FuzzParseLens`: never panics, and `Parse(spec.String())` round-trips.
- `TestSelectionPaging` covers the page size, the empty selection (one empty page), a page past the end (`not-found`), and `PageOf` for a selected ordinal, an unselected one, and one past the last selected.
- `TestSelectionPagesStableUnderAppend`: an append leaves every earlier lens page's ordinals unchanged.
- `TestSelectionGaps` covers leading, inner, cross-page and trailing gaps.
- `TestUnknownLensServesCanonical` (api): `?lens=nonsense`, `?lens=`, `?lens=chain:abc` and a kind with no resolver each answer 200 with `lens: null` and the canonical page's posts.
- `TestLensOnHiddenThreadIsNotFound`: any lens on a thread in a forum hidden from the viewer is `not-found`. A signed-in-only forum is `unauthenticated` to an anonymous viewer.
- **Never widen access.** `TestLensPostsMatchCanonical` (internal/web/api `lens_test.go`, table-driven on the `world` fixture of `thread_test.go`, whose `svc` is the `threads.Service`) holds every post a lens page serves byte-equal to the post the canonical page (`?ordinal=`) serves the same viewer.
  - The viewers are anonymous, member, the author of a hidden post, a moderator by role, a moderator by group grant (`world.groupModerator`), a moderator of another forum, an admin, and a suspended viewer, who is served as anonymous.
  - The posts are visible, author-deleted, moderator-hidden, hidden pending review, redacted (no body for anyone, moderators included), moved (no body, `moved_to` null until TKT-01M29CXJZZ), and a suspended author's posts hidden by the suspension.
  - This ticket runs it with the test filter. Each lens ticket adds its kind's rows.
- `TestLensResponseCacheKeysSelection`: two selections that land on the same page number of the same thread at the same length never share an entry. A moderator's lens page never answers a member.
- `TestLensPageStatements`: a test-filter lens page runs exactly the statements the canonical page runs, on a 10-post and a 2000-post thread. The count goes through `db.Options.Trace`, with the counter `internal/web/api/me_test.go` uses (`statements`).
- Invariants untouched, and said so in the PR:
  - no session is issued (the chokepoint's caller list is unchanged);
  - no role or status changes;
  - no record type, field or index bit is added (format goldens unchanged);
  - the cache key only gains fields.
- `go test -race ./...`, `gofmt`, `go vet`.

### Out of scope

- The lenses themselves: unread `TKT-01M29CTY4J`, author and the header's author `TKT-01M29CTY58`, chain `TKT-01M29CTY5T`, since, mentions and mod `TKT-01M29CTY6E`.
- Every client change: `TKT-01M29CTY71`.
- The phone lens sheet: `TKT-01M29CGBZD`.
- A moved post's destination and who may follow it: `TKT-01M29CXJZZ` (split, Phase 4).
- The digest lens: docs/09, post-v1.

## Notes

**agent:claude-code/phase3-groom-lenses** at 2026-10-01T07:51:00Z

run decision: pending maintainer review. Two decisions bind this ticket. (1) The lens parameter's grammar, the canonical fallback for an unknown value, and paging under a lens are recorded in docs/07 'What the thread endpoint does'. Rejected: bad-request for an unknown lens, a date in the viewer's timezone, and following a released handle. (2) Which lenses keep removed posts is recorded in docs/06 'Lenses': unread, since and chain keep tombstones in place; author and mentions keep only posts shown as themselves; mod keeps only removed posts. Both were decided in the Phase 3 grooming of 2026-10-01, lane lenses.
