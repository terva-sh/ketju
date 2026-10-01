---
schema: 3
id: TKT-01M29CTY8TB2CHCBQH3ABHCG3Q
title: Build the search API
type: task
status: in-progress
status_reason: null
priority: normal
due_on: null
labels:
  - area:search
  - effort:M
  - area:api
assignees: []
milestone: phase-3
parent: TKT-01M29BN36D34YVKVRPNQD3PHDV
origin: null
dependencies:
  - TKT-01M29CRJ3BEMZREJQRGAK3AVGK
  - TKT-01M29CEE4KNQBFP49FVSCRNZA4
blocks_on: none
references:
  - ref: design:api-resources
    path: docs/07-api-and-clients.md
claim:
  actor: agent:claude-code/phase3-cty8t
  branch: run/cty8t-search-api
  worktree: /home/sothr/workspace/git.local.sothr.com/terva-sh/ketju/.claude/worktrees/agent-aadadb9df5c175246
  commit: d80c2330c7d88339cac7bf28c89e67f97749fe4f
  session: null
  claimed_at: 2026-10-01T12:43:49Z
  expires_at: null
archive: null
created_at: 2026-09-11T23:28:54Z
updated_at: 2026-10-01T12:50:03Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase3-cty8t
  name: ""
extensions: {}
---

## Description

`GET /search?q=&forum=&author=&after=&before=` returning FTS5 snippets. The results screen that calls it is TKT-01M3V70X0F (Build the search results screen), split out in Phase 3 grooming.

Permission filtering has to happen in the query rather than after it, or a result count leaks the existence of posts in a restricted forum even when the bodies are withheld.

Snippets come from FTS5 rather than from re-reading logs, which keeps a search over half a million posts off the storage path entirely.

## Acceptance criteria

- [ ] Results exclude forums the viewer cannot read inside the statement, so a page's results and length are the same whether or not other forums hold matches (TestSearchPageLengthIgnoresUnreadableForums)
- [ ] Snippets come from the index as plain-text segments with matches flagged, and carry no markup (TestSearchSnippetIsNeverMarkup)
- [ ] q, forum, author, after, before and cursor follow docs/07 Search, and the filters combine (TestSearchQueryRules, TestSearchParams, TestSearchPagesFill)
- [ ] Responses are no-store and never in the response cache, and the goldens search.member.json and search-empty.anonymous.json are held by the client's DTO contract with the dist unchanged (TestSearchRoute, TestSearchIsNotCached, contract.test.ts)
- [ ] No result reaches a viewer who may not read the post when it is served, with a case each for a hidden forum, a signed-in-only forum to an anonymous reader, an author's delete, a moderator's hide including a group-grant moderator, an auto-hide, a redaction, a move to an unreadable forum (projected, and with the projection held behind the log), a suspended author's hidden posts, a revoked grant and a quarantined thread (TestSearchNeverWidensAccess)
- [ ] A post edited, removed or moved, or a thread renamed, after indexing is not served until the index catches up, and a title is judged at the log length the request read even when the projection trails, so no snippet or title shows text the thread no longer shows (TestSearchIsFreshWhenServed)
- [ ] Results are ordered newest first using only each post's own facts, so a viewer's pages, in order and content, do not change when posts are added in a forum they cannot read (TestSearchPagesIgnoreUnreadableForums)

## Implementation plan

Groomed 2026-10-01 against main at a599b48 (TKT-01M3V3BVPY, Phase 3 grooming, lane `search`). **Retitled from "Build the search API and results screen".** The screen is now TKT-01M3V70X0F (Build the search results screen). The API is server-lane work, and the screen queues for the committed dist. A single PR would carry the dist as well, and diffs that size have stopped Terva reviews before.

### Settled in grooming
- **The route** is `GET /search?q=&forum=&author=&after=&before=` under `/api/v1`, answering FTS5 results with snippets (docs/07 "Resources").
- **Filter inside the query** (this ticket's description). Permission filtering happens inside the statement, so a page's rows and length say nothing about forums the viewer cannot read. Snippets come from the index, not from re-reading logs.
- **Search never widens access** (docs/14 "Invariants, added"). This is the same `perm` evaluation the thread page uses, applied when the result is served, with removed, redacted and hidden posts included. A finding against it is a security finding. Moderation follows the forum grant, a group grant included (docs/08 "Decided 2026-09-30").
- **The precedent is the profile history**, `threads.Service.UserPosts` (`internal/service/threads/userposts.go`):
  - the forum set comes from `perm.ReadableForums`;
  - the thread's index decides visibility, never `post_meta`, which trails the log;
  - a quarantined thread's posts are left out;
  - a page is filled from further batches when rows are left out;
  - cursors are opaque, canonical positions.
- **The audit log's filters** (docs/07 "The audit log"):
  - an unknown actor handle is an empty page, not an error;
  - `from` and `to` are Unix ms, the first included and the first not.
- **Run decisions of 2026-10-01, pending maintainer review**, in docs/07 "Search":
  - anonymous readers may search;
  - `q`'s rules: required, 1 to 200 characters, at most 16 words, words ANDed, quoted phrases, no query syntax;
  - `forum` is answered as the thread list answers;
  - `author` is a handle, and an unknown one is an empty page;
  - `after` and `before` are ms;
  - newest-first order, which uses only each post's own facts, 25 to a page, at most 1000 results, no total (relevance lost to review 1728: bm25's index-wide statistics let unreadable posts reorder readable ones);
  - the result shape, with `snippet` as plain-text segments;
  - the freshness check at serve time;
  - the `indexing` flag.

  Alternatives for each are listed there. The index's contents and shape are TKT-01M29CRJ3B's decisions, in docs/04 "search (FTS)".

### Revised at claim (2026-10-01, against main at d80c2330)
The approach below stands. Reading the code as it is after #301 changed these points, each toward failing closed:
- **A trailing row fails closed.** After `projectNow`, the re-read row must have `log_length_projected` at or past `at`, or the thread's hits are dropped. That covers a service built with no `Project` too, where `projectNow` does nothing.
- **An ordinal the index does not hold is dropped**, not an error: `stable`'s pick keeps only ordinals up to `Index.Len()`, so one stale row cannot fail a whole search.
- **A post row must match its entry wholly:** visible, the same revision, and the same author and creation time. The doc's author and time serve the filters, so a doc that disagrees with the entry is not served.
- **The link is drawn from the re-read row**, never from the statement's join: the thread's title and slug and its forum's slug and name come from the row read at or past `at`, so a result for a post never carries a title or forum the thread no longer shows.
- **The thread must still have a post** in the re-read row (`post_count > 0`).
- **`q`'s field codes** are `required` (empty once trimmed), `too_long` (over 200 characters) and `too_many` (over 16 words), from docs/07's list. docs/07 "Field errors" gains `q`.
- **The access, freshness and paging tests live in `internal/web/api`** (`search_test.go`), not `internal/service/threads`. That package's world already has the thread page to compare with, and helpers for group-grant moderators, unprojected moves, offline redaction and suspensions. A real `search.Indexer` over the world's logs is driven by hand there, so "the indexer stopped" is the indexer not called. `parseTerms`, `splitSnippet` and the cursor are unit-tested in `internal/service/threads`.

### Approach
1. **DTO** in `internal/dto/dto.go`:
   - `SearchResults{Results []SearchResult "results"; NextCursor string "next_cursor"; Indexing bool "indexing"}`;
   - `SearchResult{Thread ThreadLink "thread"; Forum ForumRef "forum"; Ordinal uint32 "ordinal"; Author Author "author"; CreatedAt Millis "created_at"; Snippet []SnippetPart "snippet"}`;
   - `SnippetPart{Text string "text"; Match bool "match"}`.

   None is omitempty, by the package rule. Fixtures and goldens: `search.member.json` and `search-empty.anonymous.json`. Add the types to `web/client/src/api/dto.ts` and both goldens to `dto-contract.ts`, which `contract.test.ts` requires. These are type-only edits, and `go generate ./internal/web/spa` must leave the dist unchanged. Confirm that in the PR.
2. **Service: `threads.Service.Search(ctx, v, SearchQuery) (dto.SearchResults, error)`** in a new `internal/service/threads/search.go`. It lives with `UserPosts` because it reads indexes through `stable` and `snapshot`, which a separate package would need exported. `SearchQuery` holds the raw strings.
   - An inactive viewer is served as anonymous, as `access` does.
   - **`q`** is trimmed. Then 1 to 200 runes, and `parseTerms` splits it into words and `"…"` phrases, at most 16. A failure is `validation-failed` with a field error on `q` (docs/07 "Field errors on `validation-failed`"). An unbalanced quote ends its phrase at the end of the text. The terms go to `views.MatchAll` (TKT-01M29CRJ3B), so no FTS5 syntax is ever the reader's.
   - **`forum`** goes through `readForum` (`list.go`), so unknown or hidden is `not-found`, and a forum the viewer cannot read is `unauthenticated` or `forbidden`.
   - **`author`** is looked up as the audit log's actor is (`auditlog.go`, `GetUserByHandle` on the handle; nobody is an empty page). An anonymized account's old handle names nobody, so it finds nothing.
   - **`after`** and **`before`** must be non-negative integers, or `bad-request`.
   - **The cursor** is base64url of the decimal offset into the statement's rows, canonical only, or `bad-request`. Its encode and decode follow `encodePostCursor`.
   - **The forum set** is `s.Perm.ReadableForums(ctx, v)`, bound in the statement by `views.Search`.
3. **Fill a page as `UserPosts` does.** Read `views.Search` batches of 26 from the offset, and for each batch:
   - group the rows by thread, and skip a quarantined thread (`s.Quarantined`);
   - in `s.Threads.With(id, …)`, take `s.stable` over the batch's ordinals, which also gives the log length `at` it read;
   - **judge the thread against its log at `at`, not against a projection that may trail it** (Terva review 1701). When the row's `log_length_projected` is below `at`, call `s.projectNow(ctx, id, at)`, as `authorThread` in `suspension.go` does for the same reason (Terva reviews 1495 and 1503). Then re-read the thread's row (`GetThread`). Keep the thread's hits only if the re-read `forum_id` is in the viewer's readable set and, when `forum` was given, equals it. A `ThreadMoved` into a forum the viewer cannot read, not yet projected when the statement ran, therefore drops the hits. A projection that fails drops the thread's hits and is logged, so the check fails closed;
   - keep a post row only if its entry is `Flags.Visible()` and `entry.Revision == row.revision`;
   - keep a title row (ordinal 0) only if the indexed title equals the title in that re-read row, which is the thread's title at `at` (Terva review 1701). Comparing with the projection as the statement read it could accept an old title when both the index and the projection trail a rename.

   Stop at 25 kept rows plus one, at the end of the rows, or at offset 1000. `next_cursor` is the offset just past the last row shown, and empty at the end. Authors come from `s.users`, so an anonymized account shows its placeholder (`member.Author`). A title row's author is the thread's.
4. **Snippet.** `splitSnippet` cuts the `snippet()` text at U+E000 and U+E001 into `SnippetPart`s, merging neighbours. `render.Plain` strips both characters from indexed text, so only FTS5 can place them. An unpaired marker is dropped, never shown.
5. **`indexing`** comes from a new `threads.Service.SearchIndexing func() bool` field, wired in `cmd/ketju/serve.go` to the indexer's `Indexing`. When it is nil, `indexing` is false.
6. **Handler.** A new `internal/web/api/search.go` holds `h.search`, with a row `{Method: get, Path: "/api/v1/search", Handler: h.search}` in `h.routes()`. It answers `Cache-Control: no-store` and never touches `respcache`, so the response-cache key invariant is not engaged. It is a read, so an agent's bucket is not charged (docs/08). The pre-user gate already refuses it.
7. **docs/07.** The "Search" section in this grooming PR is the contract. The implementing PR updates it only where the code had to differ, and says why.

### Tests
- `internal/service/threads`:
  - **`TestSearchNeverWidensAccess`**: table-driven. Each case puts a unique word in one post, then searches as anonymous, a member, a moderator by role, a member who moderates by group grant, and an admin. A result appears only where the thread page would show the post to that viewer. Cases:
    - a hidden forum;
    - a signed-in-only forum, to an anonymous reader;
    - a forum without read for a member;
    - an author's delete (the author included);
    - a moderator's hide (the group-grant moderator included);
    - an auto-hide pending review;
    - a redacted post;
    - a thread moved into a forum the viewer cannot read, and moved back. Each move is run twice: once projected, and once with the projection held behind the log, so that the statement still sees the old forum and only the re-read row after `projectNow` refuses the hit;
    - a suspended author whose posts were hidden, and one whose were not, who is found as on the thread page;
    - a group grant revoked between two requests;
    - a quarantined thread.
  - **`TestSearchIsFreshWhenServed`**: with the indexer stopped after indexing, none of these is served: a delete, a hide, a split, an edit (no snippet of the old revision), or a rename (no title hit for the old title). The rename case also holds the projection behind, so the title is judged at the log length the request read. Once the indexer runs, the edited post is served with its new text.
  - **`TestSearchPageLengthIgnoresUnreadableForums`**: 30 matches in a forum the viewer cannot read and one they can give one result and no `next_cursor`.
  - **`TestSearchPagesIgnoreUnreadableForums`**: two readable forums with 40 matches between them. The same viewer's pages, in order and content, are identical before and after 200 matching posts are added in a forum the viewer cannot read (Terva review 1728).
  - **`TestSearchPagesFill`**: 60 matches with 10 stale rows among them give full pages with no repeats, a short page only at the end, and no cursor past 1000.
  - **`TestSearchSnippetIsNeverMarkup`**: a post whose *text* holds markup characters, written so that `render.Plain` keeps them: `` `<script>alert(1)</script>` `` in a code span, and `\<mark\>` and `&amp;amp;` as escaped text. Its snippet's segments carry `<script>`, `<mark>` and `&amp;` as literal text, and the JSON escapes them. The same post's raw HTML, `<b>bold</b>`, is absent from the snippet, because `render.Plain` drops raw HTML before indexing (Terva review 1713).
  - **`TestSearchQueryRules`**:
    - empty, whitespace, 201 characters and 17 words are `validation-failed` on `q`;
    - `OR`, `NOT`, `NEAR(a b)`, `*`, `^x`, `title:x` and an unbalanced `"` are searched as words;
    - a phrase matches only in order.
  - **`TestSearchParams`**:
    - forum: unknown, hidden, signed-in-only to anonymous, and without read;
    - author: unknown, case-insensitive, and an anonymized account's old handle;
    - after and before: edges, and malformed input;
    - cursor: malformed and non-canonical;
    - every filter alone and combined.
  - **`TestSearchAnonymizedAuthor`**.
- `internal/web/api`:
  - **`TestSearchRoute`**: `no-store`; a pre-user is refused by the gate; a bearer agent's bucket is not charged.
  - **`TestSearchIsNotCached`**: `respcache` holds no entry after a search.
  - **`TestSearchIndexingFlag`**.
  - The golden JSON tests for both fixtures.
- `web/client`: `contract.test.ts` and `tsc` over the new goldens.

### Out of scope
- The `/search` screen, its empty states, and the 390 and 1440 px checks. TKT-01M3V70X0F (Build the search results screen) owns them.
- The index, `views.Search` and `views.MatchAll`. TKT-01M29CRJ3B (Build the FTS5 search index and indexer) owns them.
- A sort option and a result count. These were left out by the run decision in docs/07 "Search", and no ticket owns them.
- Rate limiting reads. docs/08 "The transport-level ceiling" says reads are not limited at all. A search's cost is bounded instead by `q`'s 16 terms and 200 characters and by the 1000-result reach. `views.Search` runs under the request's context, so a client that goes away stops the statement.

## Notes

**agent:claude-code/phase3-groom-search** at 2026-10-01T07:58:30Z

run decision: pending maintainer review. Recorded in docs/07 "Search" (2026-10-01): anonymous readers may search what they can read; q is 1-200 characters, at most 16 words, words ANDed, quoted phrases, no FTS5 syntax; forum is answered as the thread list answers; author is a handle and an unknown one is an empty page (audit precedent); after/before are Unix ms (audit precedent); relevance order, 25 to a page, at most 1000 results, no total; snippet is plain-text segments; a hit is served only if the thread's index still shows that revision as visible (title: still the title); indexing flag. Also split in grooming: this ticket is now the API only, retitled from 'Build the search API and results screen'; the screen is TKT-01M3V70X0F (Build the search results screen), which depends on this one. The empty-state criterion moved there.

**agent:claude-code/phase3-groom-search** at 2026-10-01T08:22:42Z

Terva review 1701 (PR #270, head b52fce1, base a599b48), both findings fixed in the plan and in docs/07 'Search' ('Fresh when served'). (high) 'Recheck the thread's current forum before serving a hit': under the thread, at the log length the stable snapshot read, a trailing projection is brought up with projectNow (the suspension.go pattern, reviews 1495 and 1503) and the thread's row is read again; its forum must be readable (and equal the forum filter), and a failed projection drops the hits. The move case in TestSearchNeverWidensAccess runs with the projection held behind. (medium) 'Compare title hits with the current thread title, not its projection': the title is compared with that re-read row, which is the title at the snapshot's log length; TestSearchIsFreshWhenServed holds the projection behind for the rename.

**agent:claude-code/phase3-groom-search** at 2026-10-01T08:50:46Z

Terva review 1713 (PR #270, head d0b6f16, base a599b48), fixed. (medium) 'Make the literal-markup snippet test compatible with plain-text extraction': TestSearchSnippetIsNeverMarkup now writes the markup characters as text Plain keeps (a code span and backslash escapes) and asserts the post's raw HTML is absent from the snippet. Review round 5 of 5 on PR #270; the head carrying this fix has not been reviewed.

**agent:claude-code/phase3-groom-search** at 2026-10-01T09:44:32Z

Terva review 1728 (PR #275, head b5fa248), finding 2 (medium) 'Account for restricted documents affecting relevance-ranked pages': fixed. Relevance (bm25) is dropped because its statistics are index-wide; results are ordered newest first (created_at, then row), which uses only each post's own facts, so the docs/07 guarantee that pages do not depend on unreadable forums now holds for order too. This supersedes the run decision 'relevance order' recorded earlier; docs/07 'Search' records the change and why relevance lost. TestSearchPagesIgnoreUnreadableForums; criterion 7.
