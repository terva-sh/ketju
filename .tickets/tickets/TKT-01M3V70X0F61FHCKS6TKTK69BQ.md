---
schema: 3
id: TKT-01M3V70X0F61FHCKS6TKTK69BQ
title: Build the search results screen
type: task
status: in-progress
status_reason: null
priority: normal
due_on: null
labels:
  - area:client
  - area:search
  - effort:M
assignees: []
milestone: phase-3
parent: TKT-01M29BN367F3YQZESM63VN26GP
origin: null
dependencies:
  - TKT-01M29CTY8TB2CHCBQH3ABHCG3Q
blocks_on: none
references: []
claim:
  actor: agent:claude-code/phase3-v70x0f
  branch: run/v70x0f-search-screen
  worktree: /home/sothr/workspace/git.local.sothr.com/terva-sh/ketju/.claude/worktrees/agent-afb3b3055ac2b1ea3
  commit: 86752c422f9d6152ce015e2c1b52cdd721f33910
  session: null
  claimed_at: 2026-10-01T18:09:09Z
  expires_at: null
archive: null
created_at: 2026-10-01T07:49:20Z
updated_at: 2026-10-01T18:33:55Z
created_by:
  id: agent:claude-code/phase3-groom-search
  name: ""
updated_by:
  id: agent:claude-code/phase3-v70x0f
  name: ""
extensions: {}
---

## Description

The `/search` screen behind `GET /api/v1/search` (TKT-01M29CTY8T, Build the search API). It replaces the placeholder route in `web/client/src/routes.tsx`, and the top bar's and phone top bar's existing links to `/search` lead to it.

Split from TKT-01M29CTY8T in Phase 3 grooming (2026-10-01): the API is server-lane work, and the screen queues for the committed dist with the other client tickets. One PR carrying both would also carry the dist, which is the diff size that stopped Terva reviews before.

docs/07 "Search" describes the screen. docs/09 Phase 3 names "FTS5 search with snippets" as a deliverable, and this is the part a member sees.

## Acceptance criteria

- [x] Each result links to its post's permalink, or to the thread for a title match, shows forum, author and PostTime, and renders its snippet as text with matches in mark elements, never as markup (SearchScreen.test.tsx)
- [x] With no query, with no results (offering Clear filters when a filter is set), while the index is still building, and on a validation or not-found error, the screen shows a useful message rather than a blank page (SearchScreen.test.tsx)
- [x] e2e/search.spec.ts passes at 390 px and 1440 px with one axe scan per test and no violations: a member finds a reply by a unique word and follows it, and finds nothing from a forum they cannot read
- [ ] The committed dist is rebuilt with go generate ./internal/web/spa, the reader bundle stays within the 70000 B budget, and the PR reports the screen's bytes against the 2.6 KB estimate
- [x] /search keeps q, forum, author, after, before and cursor in its address, applies the form's draft only on Search, converts local From and To dates to ms with To including its day, and shows the filters in a fieldset at 720 px and up and in a Filters disclosure on the phone (SearchScreen.test.tsx)

## Implementation plan

Groomed 2026-10-01 against main at a599b48 (TKT-01M3V3BVPY, Phase 3 grooming, lane `search`). Filed in grooming, split from TKT-01M29CTY8T. `/search` currently renders `placeholder('search', 'Search')` (`web/client/src/routes.tsx`). The desktop top bar (`shell/TopBar.tsx`) and the phone top bar (`shell/PhoneTopBar.tsx`) already link to it.

### Settled in grooming
- **The route** `/search?q=…` is in docs/07's URL scheme. `routes.test.tsx` diffs the route table against it, and the entry is already there.
- **Mockups.** The desktop top bar draws a search control ("Search threads and posts"), and the phone top bar a search button (`docs/mockups/FrontDark.dc.html`, `MobileFrontLight.dc.html`). Both are built as links to `/search`. No mockup draws the results screen, so it follows the thread list's patterns: rows, a `PostTime`, and "Older" links rather than infinite scroll (docs/06 "Paged, not infinite").
- **Precedent: the audit screen** (`screens/admin/AuditScreen.tsx`, docs/07 "The audit log"):
  - the endpoint's parameters live in the address;
  - a draft form, applied by a button;
  - dates are local, and "To" includes its day, sent as ms.
- **Placement.** The reader bundle, not a lazy chunk. `scripts/bundle.mjs` counts every non-admin chunk, so a split saves no budget and adds overhead (docs/07 "Preact client": "Lazy-loading small panels did not help").
- **Times are absolute** (docs/06, decided 2026-10-01). Use `PostTime`.
- **e2e conventions** (Phase 3 brief): stored sessions, one axe scan per test, and a failing test prints its error context. This spec calls no `/auth/*` route, so it needs no `paceAuth` row.
- **Run decisions of 2026-10-01, pending maintainer review**, in docs/07 "Search", second part:
  - the address carries the parameters, `cursor` included;
  - draft then apply, so typing fetches nothing;
  - focus on the query field when there is no query;
  - the filters: forum from `/forums`, author handle, and local From and To dates;
  - a "Filters" disclosure on the phone, open when a filter is set;
  - each result shows the title link (permalink, or the thread for a title match), forum, author, `PostTime`, and the snippet with `<mark>`;
  - "More results" follows the cursor;
  - the three empty states and the `indexing` note;
  - the reader bundle.

  The alternatives (search as you type, a field in the top bar) are listed there.

### Approach
1. **`screens/SearchScreen.tsx`** and `screens/search.css`, replacing the placeholder entry in `routes.tsx` with `{ path: '/search', screen: 'search', component: SearchScreen }`.
2. **The address is the state.** `useLocation().query` gives `q`, `forum`, `author`, `after`, `before` and `cursor`. `searchUrl(params)` builds `/api/v1/search?…` from the present ones. `useResource(key, fetch, peek)` fetches only when `q` is non-empty, as `ThreadListScreen` does, so Back renders at once. `useScrollMemory(key, !!data)`.
3. **The form** is `<form role="search">` with a labelled `<input type="search" name="q">`. On a screen with no `q`, the input gets focus on mount, in a layout effect.

   The filters:
   - `<select>` of forums from `forumsUrl` (`ForumIndex.tsx`), children indented under their parent, with "All forums" first;
   - an author handle field;
   - two `<input type="date">` labelled From and To.

   Local dates become ms as the audit screen converts them: From is local midnight, and To is the next local midnight. The draft is component state, seeded from the address. Search calls `route('/search?' + params)`, without `cursor`. Clear filters keeps `q` and drops the rest.

   **The filters' container depends on the layout**, read from the `isPhone` signal (`state/layout.ts`), as the shell reads it for the tab bar:
   - On the phone layout the filters sit in `<details class="search-filters">`, with a summary saying how many are set. They start open when any filter is set.
   - At 720 px and up they are a plain `<fieldset class="search-filters">` with a legend, always shown.

   CSS cannot set a `<details>` element's `open` state, so hiding a summary would leave desktop filters closed and unreachable (Terva review 1711).
4. **Results.** An `<ol class="search-results">` of `<li>`, each holding:
   - the title as a link: `/t/{id}/{slug}/{ordinal}` (`permalink` from `components/thread/Post`), or `/t/{id}/{slug}` when `ordinal` is 0;
   - a meta line: forum (linking to `/f/{slug}`), author (handle with `RoleBadge`, which also marks an agent), and `<PostTime at={created_at}/>`;
   - the snippet, as text nodes with each `match` segment in `<mark>`. It is never `dangerouslySetInnerHTML`, so a snippet holding `<script>` renders as text.

   `next_cursor` gives a "More results" link (`rel="next"`) to the same address with `cursor`. On a page with a cursor, a "First results" link drops it.
5. **States.**
   - No `q`: one paragraph saying search covers posts and thread titles in forums the reader can read.
   - No results: "Nothing matches “{q}”." With a filter set, a "Clear filters" link follows it.
   - `indexing`: a note above the results that some posts may not be found yet.
   - `validation-failed` on `q`: the field's message beside the input, from `forms/messages.ts`'s field-error mapping.
   - A `not-found` forum: a message that the forum filter names no forum the reader can see, with Clear filters.
   - Any other error: `ErrorPanel` with retry.
   - A visually hidden `role="status"` region says "N results on this page", or "No results", after each load.
6. **Shell.** `useShellContext({ title: 'Search', actions: [] })`. The phone top bar's search link already shows the current page through `current('/search', path)`.
7. **Build.** Run `go generate ./internal/web/spa` and commit the dist. Report the reader bundle before and after in the PR. **Estimate:** about 2.1 KB gzip of JS and 0.5 KB of CSS, from the audit screen's filter bar and the thread list's rows, which this resembles. Measure the entry against a lazy `SearchScreen` chunk (`renderAt`'s `lazyChunks` would then need a row). Keep the lower counted total. Stop and report if the screen adds more than 3000 B or takes the reader past 70000 B.

### Tests
- `screens/SearchScreen.test.tsx` (Vitest, `renderAt` from `testing.tsx`):
  - no query: the intro, and focus on the field;
  - submitting puts `q` and the filters in the address with no `cursor`, and typing fetches nothing;
  - the dates: From is local midnight and To is the next local midnight, checked in a non-UTC zone;
  - the filters' container: with `isPhone` false the fields are visible in a `fieldset` and no `details` exists; with it true they sit in a closed `details` when no filter is set, and an open one when a filter is;
  - results: the link targets for a post and for a title match, `PostTime`, and the snippet's `<mark>` around matches only;
  - a snippet segment holding `<script>` renders as text, and no element is created from it;
  - the empty states, with and without filters;
  - the `indexing` note;
  - `validation-failed` beside the field;
  - a `not-found` forum;
  - `ErrorPanel` with retry;
  - "More results" carries `next_cursor`;
  - Back renders from the held answer without a fetch.
- `routes.test.tsx` keeps passing with the placeholder replaced.
- `e2e/search.spec.ts` (Playwright), on both projects (390 and 1440 px), using a stored member session and the e2e seed:
  - a member posts a reply carrying a unique word, opens `/search` from the top bar or the phone top bar, searches, waits for the result (the indexer is asynchronous, so the test polls with `expect.poll`), and follows it to the post's permalink;
  - a search for a word that is only in the seed's restricted forum finds nothing for that member;
  - the forum filter and a date filter narrow the results;
  - one axe scan per test.

### Out of scope
- The API: TKT-01M29CTY8T (Build the search API).
- A `/` keyboard shortcut to open search. The desktop mockup shows the key, but docs/06's shortcut list does not include it, and the thread page owns the shortcuts today. Filed as a draft with no milestone: TKT-01M3V7GE59 (Add the / shortcut that opens search on desktop).
- Highlighting the matched words on the post's own page after following a result. No ticket owns it, and it is not a docs/09 deliverable.

## Notes

**agent:claude-code/phase3-groom-search** at 2026-10-01T07:58:30Z

run decision: pending maintainer review. Recorded in docs/07 "Search" (2026-10-01), the screen paragraph: the address carries the endpoint's parameters; the form is a draft applied by Search (audit-screen precedent), so typing fetches nothing; filters are forum, author handle and local From/To dates; a Filters disclosure on the phone; results link to the post's permalink with forum, author, PostTime and a marked snippet; More results follows the cursor; empty states for no query, no results and indexing; the screen goes in the reader bundle because bundle.mjs counts every non-admin chunk anyway.

**agent:claude-code/phase3-groom-search** at 2026-10-01T08:43:37Z

Terva review 1711 (PR #270, head e5998ef, base a599b48), fixed. (medium) 'Do not rely on CSS to open the desktop filters': the container now follows the isPhone signal, a details disclosure on the phone and a fieldset at 720 px and up, with a Vitest case for each (criterion 5).

**agent:claude-code/phase3-v70x0f** at 2026-10-01T18:21:24Z

run decision: pending maintainer review. Recorded in docs/07 "Search", the "As built (TKT-01M3V70X0F)" paragraph. Each departs from, or adds to, the plan:

- Nothing a search answers is held. The plan had `useResource(key, fetch, peek)` so Back renders at once, but the endpoint is no-store with no ETag, so `apiGet` holds nothing and `peekGet` has nothing to peek. A screen-local cache would show results without the server's per-request access check (docs/14 "Invariants", a security class), so Back asks again and `useScrollMemory` restores the position once the answer is on screen. The Vitest case is "asks again on Back, and shows the earlier search". Alternative that lost: a module-level cache of the last few answers, for the reason above.
- The query's field errors are in the screen's own words, for `required`, `too_long` and `too_many`, with the refusal's own words for any other code. The plan named `forms/messages.ts`, but `src/forms` sits in the ApplyRoot chunk group (`vite.config.ts`). Importing it from an entry screen would load the whole application chunk on every page.
- A post's result shows `#N` beside the thread title, so results from one thread are distinguishable links. A title match shows none.
- Focus on arrival: `routeFocuser` runs in the Router's layout effect, after the screen's own, and moved focus to the h1. `focusMainHeading` now prefers `main [data-autofocus]`. The query field carries the mark only with no query. The screen's layout effect still focuses it on a first load and when the query is cleared in place.
- `dateOf`, `startOf` and `stamp` moved from `screens/admin/AuditScreen.tsx` to `src/dates.ts`, which AuditScreen re-exports for its tests. Importing them from the admin chunk would have pulled that chunk into the entry.

Bundle, measured both ways on main 86752c42: the reader bundle is 66189 B before and 68626 B after (+2437 B: JS +1898 B, CSS +537 B), against the 2.6 KB estimate. With SearchScreen lazy it is 70151 B, so the screen stays in the entry.

Caught while checking screenshots: the top bar's search link is `class="search"` (shell/TopBar.tsx), so a `.search` rule in search.css restyled it on every page. The screen's root is `.search-page`, and a Vitest case refuses a bare `.search` selector in search.css.
