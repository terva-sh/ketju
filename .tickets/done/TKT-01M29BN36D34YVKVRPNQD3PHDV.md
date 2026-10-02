---
schema: 3
id: TKT-01M29BN36D34YVKVRPNQD3PHDV
title: Lenses, unread surfaces, search, and live updates
type: epic
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area:views
  - effort:XL
assignees: []
milestone: phase-3
parent: null
origin: null
dependencies:
  - TKT-01M29BN367F3YQZESM63VN26GP
blocks_on: children
references:
  - ref: design:lenses
    path: docs/06-thread-views.md
claim: null
archive: null
created_at: 2026-09-11T23:08:14Z
updated_at: 2026-10-02T14:32:44Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase3-run-orchestrator
  name: ""
extensions: {}
---

## Description

Everything that answers "what is new and where is the part I care about", without ever reordering a thread.

A lens is a filter, marker, or overlay over the ordered post list. It never reorders and never indents. Most lenses are computed server-side from the in-memory index alone, which is what the 40-byte index entry buys: author, reply_to, and created_at are there, so the author, chain, and since lenses never touch the database.

Every lens is URL-addressable through `?lens=`, so a lens can be shared and the future non-JS client gets it for free. A lens header always shows the count of hidden posts and a one-click return to the full thread at the same ordinal.

The cross-forum surfaces (unread, new, participated, watching) are the "what's new" navigation. On phones they are the bottom tab bar, which is the whole argument in docs/10 against hiding them behind a hamburger.

Live updates are deliberately dumb. SSE carries `{thread_id, log_length}` hints with no content and no per-viewer logic, and the client reconciles through the normal authorized read path.

## Acceptance criteria

- [x] Every lens is reachable by URL alone with no client-only state
- [ ] The author, chain, and since lenses execute without a database query
- [x] Search returns FTS5 snippets and the index is rebuildable with `ketju reindex --search`
- [x] SSE payloads carry no post content, and authorization happens on the follow-up fetch
- [x] New posts never move the page under a reader who is not at the bottom; they arrive behind a 'N new posts' pill
- [x] Polling every 30 s takes over when SSE is unavailable

## Notes

**agent:claude-code/phase3-run-orchestrator** at 2026-10-02T14:32:43Z

Verification on 2026-10-02, against origin/main at 0c88403b. The maintainer chose "verify, then close". A read-only agent checked each criterion against the code and its tests. It read the code and did not run the tests, but every test it cites passed in the exit check's CI run 2359.

1. **Lenses by URL alone: holds.**
   - Server: `?lens=` is parsed by `lens.Parse` (`internal/service/threads/lens/lens.go:65-93`) for unread, unread:B, author:h, chain:N, since:d, mentions and mod.
   - Client: `view.ts:19-37` takes the lens from the query alone, and every control is a `lensHref` link. Bare `unread` is rewritten to `unread:B` in the address.
   - Tests: `ThreadScreen.lens.test.tsx:80`, `TestLensValueRoundTrips`, `TestParseLensTotal`, e2e `lenses.spec.ts:39` and `:164`.
2. **Author, chain and since without a database query: partly.**
   - Chain and since select from the in-memory index and add no query: `TestChainLensStatements` and `TestSinceLensStatements` show a lens page runs exactly the full page's queries.
   - Author adds one handle lookup (`GetUserByHandle`, `lenses.go:190-217`), proven by `TestAuthorLensFromIndex`. docs/07 records this: "the kind's resolver … reads what it needs from the database".
   - Every thread page still runs its access and page queries, so the literal wording holds for no lens.
   - Left unticked; the reading the design supports is recorded here.
3. **FTS5 snippets and `ketju reindex --search`: holds.**
   - Code: `migrations/0022_search.sql:50-54`, `views/search.go:64`, `cmd/ketju/reindex.go:141`.
   - Tests: `TestSearchSnippetMarksMatches`, `TestSearchSnippetIsNeverMarkup`, `TestReindexSearchEqualsIncremental`.
4. **SSE carries no content, and authorization happens on the follow-up fetch: holds.**
   - A frame is `ThreadHint{thread_id, log_length}` (`dto.go:643-646`). The client refetches through `GET /api/v1/threads/{id}`, which runs the access check.
   - Tests: `TestEventPayloadIsThreadAndLogLength`, `TestEventCarriesNoMoreThanThePage`.
   - The stream also checks access before each hint, which goes beyond the criterion.
5. **New posts never move the page; they wait behind the pill: holds.**
   - Code: `ThreadScreen.tsx:454-491`, `NewPostsPill.tsx`.
   - Tests: e2e `live.spec.ts:55`, `:74`, `:86`; unit tests in `ThreadScreen.test.tsx`.
6. **Polling every 30 s when SSE is unavailable: holds.**
   - Code: `live.ts:22-24`, `:61-86`.
   - Tests: `live.test.ts`, e2e `live.spec.ts:108`.

Closed: all 24 children are done, and five of the six criteria are ticked. Criterion 2 is the one left unticked, for the reason above.

## Summary

Phase 3's epic. All 24 children are done, and the Phase 3 exit check (TKT-01M3V3CFRE, #351) passed. On 2026-10-02, at the maintainer's request, criteria 1 and 3-6 were verified against the code and its tests, and ticked. Criterion 2 is unticked: chain and since add no query of their own, but the author lens resolves its handle with one query, which docs/07 documents, and every thread page runs its access queries. See the verification note.
