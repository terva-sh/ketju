---
schema: 3
id: TKT-01M29CRJ1PY355KX3HX7A5N785
title: Build the lens framework over the in-memory index
type: task
status: done
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
  - ref: pr:299
    path: null
claim: null
archive: null
created_at: 2026-09-11T23:27:36Z
updated_at: 2026-10-01T12:05:18Z
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

- [x] A lens is a keep-or-skip filter the framework applies in one ascending walk of the in-memory index (Index.Select), and lens.Selection has no exported way to hold ordinals in any other order; TestIndexSelectAscending holds every selection strictly ascending over random indexes
- [x] lens.Parse is total (FuzzParseLens), and an unknown, malformed or not-yet-served lens serves the canonical page with lens: null, never an error (TestUnknownLensServesCanonical)
- [x] A lens page answers lens.shown, lens.hidden (post count minus shown) and lens.gaps, fixed by goldens in internal/dto and internal/web/api
- [x] Paging applies to the lens's posts: page and total_pages count them, ?ordinal= finds its lens page, and an append leaves every earlier lens page unchanged (TestSelectionPagesStableUnderAppend)
- [x] A lens never widens access: TestLensPostsMatchCanonical holds every post a lens serves equal to the canonical page's for the same viewer, across anonymous, member, author, moderator by role, moderator by group grant, admin and suspended viewers, and removed, redacted, hidden-pending-review, moved and suspended-author posts
- [x] The response cache key carries the resolved lens and a digest of the page's ordinals, and two selections on the same page number never share an entry (TestLensResponseCacheKeysSelection)
- [x] A lens page runs the canonical page's statements whatever the thread's length, counted through db.Options.Trace

## Implementation plan

Planned 2026-10-01 against main at a599b48 (Phase 3 grooming, lane `lenses`). Revised at claim on 2026-10-01 by agent:claude-code/phase3-crj1p.

### Settled in grooming

- Lenses are computed server-side from the thread's in-memory index, and are URL state (docs/06 "Lenses"; docs/07 "Path to a non-JS client" point 3). `Query.Lens` already reaches `threads.View` from `threadQuery` in `internal/web/api/api.go`, and `dto.Lens {kind, value, shown, hidden}` is already in `internal/dto/dto.go`.
- A lens filters and never reorders (docs/06 "The stance").
- The lens grammar, the fallback to the canonical page for an unknown value, and the paging rules are in docs/07 "What the thread endpoint does", under "Decided by the run (2026-10-01, pending maintainer review): the `lens` parameter".
- What each lens keeps of removed posts is in docs/06 "Lenses", under "Decided by the run (2026-10-01): what each lens keeps of removed posts".
- Access is the canonical page's and comes first: `threads.access` runs before the lens is resolved. A thread hidden from the viewer is `not-found` whatever the lens says (docs/07 "Access").
- A removed post's body follows `revealRemoved` and `pageClass`, unchanged (docs/07 "Removed posts", TKT-01M29CKRJ9). A lens only chooses ordinals. It never decides what a post shows.
- The index format does not change (docs/03 "Index entry"). Nothing here touches the log format.

### Approach (revised 2026-10-01 at claim, against main at 3f1884b9)

The groomed approach stands. Where the build differs from it, the reason is given.

1. **`threadlog.Index.Select(keep)`** makes one ascending walk under the read lock. `ByAuthor` and `Since` are now wrappers over it. Its doc comment says that `keep` must not call back into the `Index`.
2. **Package `internal/service/threads/lens`.**
   - `Parse` is total, and `Spec{Kind, Ordinal, Handle, SinceMS, Bare}` has `String()` and `Value()`.
   - Author handles are ASCII-lowercased and checked with `domain.ValidateHandle`, which also accepts `deleted-user-<id>`.
   - A since moment before 1970 reads as 0, so `Parse(s.String()) == s` holds for every spec.
   - `Filter` has one unexported method.
   - `Apply(ix, f) Selection` builds the selection. `Selection` has `Len`, `Hidden`, `Posts`, `TotalPages`, `Page` (which returns a copy), `PageOf` and `Gaps` (never nil).
   - **Change: `lens.Ordinals(...)`** is a set filter that ships now. The chain and mentions lenses are sets decided before the walk, and the api tests in another package need a sealed filter they can build. A filter still cannot reorder, because `Apply` makes the selection.
   - The export_test.go `Odd` filter drives the package's own tests.
3. **Registration.** `lensResolvers map[lens.Kind]lensResolver` lives in `threads/lenses.go`, and is empty at this ticket.
   - A resolver runs after `access`, outside the actor. It returns `resolvedLens{kind, value, key, filter func(*threadlog.Index) (lens.Filter, error)}`.
   - **Change:** the filter is built inside `Stable` from the same index snapshot. The chain lens can then walk ancestors consistently with the page.
   - **Test seam:** `Service.TestLenses map[lens.Kind]lens.Filter` serves a kind with a fixed filter, keyed `test:<spec>`. The server leaves it nil.
4. **`threads.View`.**
   - With no resolved lens, the canonical path is unchanged.
   - With a lens, `lensPage` runs inside `Stable`: filter, `Apply`, page choice, then `snap.take`.
   - **Decision (internal):** `?ordinal=` past the thread's last post is `not-found` under a lens, as on the canonical page. Within the thread, `PageOf` applies.
   - `total_pages` counts the selection, and `Body.Lens` is `lensBody`.
5. **DTO.** `dto.Lens.Gaps []LensGap` and `dto.LensGap{after, count}`. The internal/dto goldens are regenerated, and the new `internal/web/api/testdata/golden/thread.lens.json` is added.
   - **Change: a type-only client edit.** `dto-contract.ts` holds `web/client/src/api/dto.ts` to the dto goldens with no extra keys. So `Lens.gaps` and `LensGap` are added there, and `gaps: []` is added to two literals in `src/state/threads.test.ts`.
   - `go generate ./internal/web/spa` leaves `internal/web/spa/dist` byte-identical, because types are erased. The orchestrator approved this. It is not a dist-lane PR.
6. **Response cache.**
   - `respcache.Key.Lens` holds `resolvedLens.key`, or "" for the canonical page.
   - The new `Key.Selection` field is `ordinalsDigest(snap.ordinals)`. It is filled for every page, the canonical page included, which costs nothing and keeps a lens with the canonical page's ordinals apart from it anyway.
7. **Docs.** In docs/07, the access bullet loses the `lens ... bad-request` line and says that access is decided before the lens. The decided grammar's "once that ticket lands" becomes past tense, and a paragraph says how a lens is applied and added. The response cache section gains the lens/selection bullet, the example gains `gaps`, and the goldens line names `thread.lens.json`.

### Tests

- threadlog has `TestIndexSelectAscending` (a property test over random indexes, against a hand-written filter), `TestSelectWalksOnceInOrder` and `TestByAuthorAndSinceUnchanged`.
- lens has:
  - `TestParseLensTotal` and `TestSpecString`;
  - `FuzzParseLens`, covering round-trip, no panic, a canonical spec with no fields set, and no negative moment;
  - `TestSelectionPaging`, `TestOrdinalsFilter`, `TestSelectionPagesStableUnderAppend`, `TestSelectionGaps` and `TestSelectionAgreesWithItsGaps`.
- threads has `TestLensResolverRegistration`: a kind with no row is the canonical page, and with a row, its filter, value and spec are used. It also has `TestCanonicalPageUnchangedByTheFramework`.
- api (`lens_test.go`):
  - `TestUnknownLensServesCanonical` checks that body and ETag are equal to the canonical page's, for four classes.
  - `TestLensOnHiddenThreadIsNotFound` covers both the 404 and the 401.
  - `TestLensPageGolden`, `TestLensPaging` and `TestLensAfterLogLength`.
  - `TestLensPostsMatchCanonical` uses `accessThread`, which builds visible, edited, agent, author-deleted, moderator-hidden, hidden-pending-review, suspended-author-hidden (dave is suspended), hard-redacted and moved posts. The moved flag is set in the sidecar index offline, since no record moves a post yet. Every post is checked under the lenses odd, even, all, and each post alone. There are nine viewers: anonymous, a member, the author of a moderator-hidden post, the author of a pending-review post, a moderator by role, a moderator by group grant, a moderator of another forum, an admin, and the suspended author. The test runs two rounds in reversed viewer order, through the cache.
  - `TestLensResponseCacheKeysSelection` uses a thread written by one author, so the authors digest does not separate anything. It covers the canonical page, a lens with the same ordinals, the same lens string over three selections, and another kind. It counts inserts and hits, and compares against an uncached draw. A moderator's page never answers a member, an anonymous viewer or the author.
  - `TestLensPageStatements` checks that the statements equal the canonical page's for anonymous and member viewers on 10-post and 2000-post threads, counted through `db.Options.Trace`.
- Mutations tried:
  - Dropping `Selection` from the key fails `TestLensResponseCacheKeysSelection`. It needed the one-author thread, because the authors digest had hidden it.
  - Keying every lens as "" fails the insert count.
- Invariants are untouched:
  - no session is issued;
  - no role or status changes;
  - no record type, field or index bit is added;
  - the cache key only gains a field.

### Out of scope

- The lenses themselves: unread `TKT-01M29CTY4J`, author and the header's author `TKT-01M29CTY58`, chain `TKT-01M29CTY5T`, since, mentions and mod `TKT-01M29CTY6E`.
- Every client change beyond the type-only edit above: `TKT-01M29CTY71`.
- The phone lens sheet: `TKT-01M29CGBZD`.
- A moved post's destination and who may follow it: `TKT-01M29CXJZZ` (split, Phase 4).
- The digest lens: docs/09, post-v1.

## Notes

**agent:claude-code/phase3-groom-lenses** at 2026-10-01T07:51:00Z

run decision: pending maintainer review. Two decisions bind this ticket. (1) The lens parameter's grammar, the canonical fallback for an unknown value, and paging under a lens are recorded in docs/07 'What the thread endpoint does'. Rejected: bad-request for an unknown lens, a date in the viewer's timezone, and following a released handle. (2) Which lenses keep removed posts is recorded in docs/06 'Lenses': unread, since and chain keep tombstones in place; author and mentions keep only posts shown as themselves; mod keeps only removed posts. Both were decided in the Phase 3 grooming of 2026-10-01, lane lenses.

**agent:claude-code/phase3-crj1p** at 2026-10-01T12:00:52Z

How to add a lens, for TKT-01M29CTY4J, CTY58, CTY5T and CTY6E:

1. Write the Filter constructor in `internal/service/threads/lens`. Filter's one method is unexported, so the constructor has to live in that package. A lens whose posts are a set decided before the walk can return `lens.Ordinals(...)`.
2. Add a row to `lensResolvers` in `internal/service/threads/lenses.go`, a `lensResolver func(ctx, s *Service, a access, spec lens.Spec) (resolvedLens, error)`:
   - It runs after `access` and outside the actor, and may read the database.
   - It refuses with a problem code where docs/07 says so, for example mod to a non-moderator.
   - It returns `{kind, value, key, filter}`. `value` is `lens.value` in the answer. `key` is the resolved form for the cache key, for example `author:<user id>`.
   - `filter(ix)` runs inside Stable. It may read `ix` before it returns, which the chain's ancestor walk needs, but the Filter it returns must not touch `ix`.
3. Add the kind's rows to `TestLensPostsMatchCanonical` in `internal/web/api/lens_test.go`. `accessThread` builds every post state, and the viewers table holds every class. Also adjust `TestLensResolverRegistration` if the kind's sample spec changes.

Nothing else changes: paging, gaps, the cache key's ordinal digest, the ETag and what each post shows follow from the ordinals.

Internal decisions taken at build, not member-visible beyond what docs/07 already says:
- `?ordinal=` past the thread's last post is not-found under a lens, as on the canonical page. This sentence was added to docs/07's paging paragraph.
- A since moment before 1970 parses as 0, which selects the same posts and keeps `Parse(s.String()) == s`.
- Author handles are ASCII-lowercased at parse.

Type-only client edit: `web/client/src/api/dto.ts` gains `Lens.gaps` and `LensGap`, and `src/state/threads.test.ts` gains `gaps: []` in two literals. `dto-contract.ts` refuses a golden key the types lack. Dist is byte-identical after `go generate ./internal/web/spa`, and the orchestrator approved the edit on 2026-10-01.

`Service.TestLenses` is a test seam that the server leaves nil. It lets another package's tests serve a kind with a fixed filter.

## Summary

The lens framework is in. threadlog.Index.Select is the one ascending walk. Package internal/service/threads/lens holds the total Parse, sealed Filter, Ordinals, Apply and Selection (paging, PageOf, gaps). threads/lenses.go holds the resolver table, empty until the four lens tickets add their rows, and threads.View serves a lens page through the canonical posts path, after access. dto.Lens gains gaps, the response cache key gains Selection (a digest of the page's ordinals) beside the resolved lens, and docs/07 drops the lens bad-request line. An unknown lens serves the canonical page. TestLensPostsMatchCanonical holds lens posts byte-equal to the canonical page's across nine viewer classes and every removed-post state. All seven criteria are ticked.
