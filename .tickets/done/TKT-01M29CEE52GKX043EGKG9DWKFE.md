---
schema: 3
id: TKT-01M29CEE52GKX043EGKG9DWKFE
title: Build the client router and route table
type: task
status: done
status_reason: null
priority: high
due_on: null
labels:
  - area:client
  - effort:M
assignees: []
milestone: phase-1
parent: TKT-01M29BN367F3YQZESM63VN26GP
origin: null
dependencies:
  - TKT-01M29CCC02K8F14P0SKAYFMSE6
blocks_on: none
references:
  - ref: design:url-scheme
    path: docs/07-api-and-clients.md
  - ref: pr:60
    path: null
claim: null
archive: null
created_at: 2026-09-11T23:22:04Z
updated_at: 2026-09-24T11:08:15Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase1-run-415af6aa
  name: ""
extensions: {}
---

## Description

`preact-iso` routing over exactly the URL scheme in docs/07, with the admin bundle lazy-loaded.

The routes are not an internal detail. They are the paths the future HTML client will serve, so a client-only route or a piece of state that lives outside the URL is a future porting cost. Lenses and page numbers are query parameters for the same reason.

A stale slug in a thread URL redirects to the current one rather than 404ing.

## Acceptance criteria

- [x] Every path in the docs/07 URL scheme resolves to a screen, tested by a route table test
- [x] Lens and page live in the URL and a shared link reproduces the exact view
- [x] A stale slug redirects to the canonical URL preserving page and lens
- [x] The admin bundle loads only on an admin route

## Implementation plan

Revised at claim (2026-09-24), after reading the code as it stands: the CCC02 scaffold, preact-iso 2.12.2's Router and exec, the server's SPA fallback, and the CSP.

### Approach
1. **`src/routes.ts`**
   - Holds `routes: {path, screen}[]`, one entry per docs/07 URL scheme path except `/auth/*`, which is server-only. `:param` replaces `{param}`; `{forum-slug}` becomes `:forum`.
   - preact-iso takes the first match in child order. Every pattern here differs in segment count or literal segments, so order does not matter, and the test proves it by resolving each URL to the screen named for it.
2. **Screens**
   - Screens that phase 1 has not built map to `Placeholder`, which names the screen and says it is not built yet. Every path therefore resolves to something that is not NotFound.
   - The forum index keeps the scaffold's signed-in line. CEE6V replaces the thread screen, and each screen ticket replaces its own placeholder.
3. **Admin**
   - `screens/admin/AdminRoot.tsx` is imported only through `lazy(() => import(...))`, so Vite emits it as a separate chunk.
   - The seven `/admin` paths all go to it, and it picks the section from the path.
   - The CSP allows `'self'` in script-src, so the chunk loads without a nonce.
4. **View state in the URL: `src/view.ts`**
   - `useThreadView()` reads `page` and `lens` from `useLocation().query`. `setPage` and `setLens` call `route()` with the new query.
   - No signal holds them, so a shared link is the view. CEE6V must use it; that is noted there.
5. **Stale slug**
   - `canonicalThreadUrl(location, thread)` is pure. It returns the URL with the thread's current slug, keeping the ordinal suffix, `/history`, and the query string byte for byte, or null when the slug is already current.
   - The thread screen calls it after `GET /api/v1/threads/{id}` answers, and replaces the history entry.
   - Until CEE5H lands, the thread placeholder does that one GET itself with `fetch`, passing the page and lens through. CEE5H swaps it for the fetch layer.
   - A server-side redirect was considered and not chosen: the Go server has no thread lookup in the shell path yet (`ketju serve` is CCBYX). docs/07 already has the client handle it, and the non-JS client will do its own.

### Tests
- **Vitest 5.0.1 with happy-dom**, exact pins, run by `npm test`. CI gets an added "client tests" step after "built client is current", which reuses its `npm ci`.
- **AC1**
  - `routes.test.tsx` renders `<App/>` at one concrete URL per docs/07 line and asserts the screen rendered is the one named for it, never NotFound.
  - A second test parses docs/07's URL scheme block and diffs it against the table both ways, minus `/auth`.
  - Unknown paths, such as `/admin/nope` and `/t/x`, render NotFound.
- **AC2**
  - `useThreadView` reads `page` and `lens` from the URL.
  - Setting either changes `location.search` and nothing else.
  - A fresh render at the same URL shows the same view.
- **AC3**
  - Unit tests of `canonicalThreadUrl` over the thread page, the permalink, `/history`, and a query with `page` and a `lens` containing `:`.
  - A render test with a stubbed fetch returning a new slug asserts `location` becomes the canonical URL and that history was replaced, not pushed.
- **AC4**
  - A Go test in `internal/web/spa` reads the embedded `.vite/manifest.json`. It asserts `src/screens/admin/AdminRoot.tsx` is a dynamic entry, and that no static import reachable from `index.html` includes it.
  - A browser check: load `/` and `/admin` from the built client, record the network, and confirm the admin chunk loads only on `/admin`.
- Mutation checks over the route table, the slug function, and the manifest check.

### Out of scope
- Permalink scrolling and landing: CEE6V.
- Focus on route change: CGBXK.
- The fetch layer: CEE5H.
- Forum slug renames are undocumented (the ticket's open item); left as is.

## Notes

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T10:51:56Z

Built and verified.

### What landed
- **`src/routes.tsx`:** one entry for each of the 25 non-auth paths in the docs/07 URL scheme.
  - Screens phase 1 has not built map to `Placeholder`, which renders the screen's name as `data-screen`.
  - The forum index keeps the scaffold's signed-in line.
  - `/auth/*` is not a client route.
- **Admin:**
  - The seven `/admin` paths go to `lazy(() => import('./screens/admin/AdminRoot'))`, which is built as `AdminRoot-*.js` (0.3 KB gzipped).
  - `/admin/nope` is NotFound, because only the documented children are routes.
- **`src/view.ts`:**
  - `useThreadView()` reads and writes `page` and `lens` in the query string and holds them nowhere else.
  - `canonicalThreadUrl()` swaps a stale slug for the current one, keeping the ordinal, `/history`, the query string exactly as written, and the hash.
- **`src/screens/ThreadScreen.tsx`:**
  - A stand-in for the thread route until CEE6V. It GETs `/api/v1/threads/{id}` with the URL's page, lens and ordinal, and does a replace (not a push) when the slug is stale.
  - A 404 renders NotFound.
  - The bare `fetch` is temporary: CEE5H's fetch layer replaces it, and CEE6V replaces the screen.
- **Tests:**
  - Vitest 5.0.1 and happy-dom 20.14.5, exact pins, run by `npm test` (47 tests).
  - CI gains an added "client tests" step after the build check. No existing step changed.
- **`internal/web/spa`:** `TestTheAdminBundleIsSeparate` reads the embedded Vite manifest and checks four things:
  - the admin module is a dynamic entry of `index.html`;
  - nothing statically reachable from the entry is under `src/screens/admin`;
  - the shell does not reference the chunk;
  - the chunk serves.

### Decisions (alternatives considered)
- **AC1 renders the real Router at each URL rather than calling preact-iso's `exec` over the table.** That tests the wiring, and the lazy chunk actually resolving, as well as the patterns. The docs/07 diff parses the document's code block, so neither side can drift silently. A committed copy of the list was rejected, because it would itself drift.
- **The AC4 bundle check is a Go test over the embedded manifest, not a Node script.** It runs in the ordinary suite. It checks the build that ships, which CI has already proved matches the source. It needs no browser.
- **The stale-slug redirect is client-side,** as docs/07 has it. A server-side 301 was rejected for now: the shell path has no thread lookup, and `ketju serve` is CCBYX. The non-JS client will need its own, and that is its ticket's call.
- **`hasPage` is exposed separately from `page`,** so CEE6V can tell "no page named" (the server picks the landing page, docs/06) from "page 1".

### Verification
- **Vitest:** 47 tests. Each of the 26 concrete URLs (one per docs/07 line, including the thread URL with a query) renders its own screen, and 6 near-miss paths render NotFound. The table matches docs/07 in both directions.
  - `useThreadView` reads page and lens and writes them to the URL, and a fresh mount of the resulting URL is the same view.
  - `canonicalThreadUrl` is tested over 10 cases.
  - The stale-slug redirect replaces rather than pushes, and keeps page and lens.
- **Browser** (headless Chromium through playwright-core, against the built client served by `web.New` with the real shell under the enforcing CSP; a scratch server under `cmd/`, deleted and never committed):
  - `/` loads only `index-*.js`.
  - Navigating client-side to `/admin/queue` loads `AdminRoot-*.js` in the same document.
  - A direct load of `/admin/settings` loads both.
  - `/t/{id}/old-name?page=2&lens=author:alice` became `/t/{id}/renamed?page=2&lens=author:alice`, with history length 2 (a replace), and the API was asked for `?page=2&lens=author%3Aalice`.
  - `/t/{id}/old-name/42/history` became `/t/{id}/renamed/42/history`.
  - `/admin/nope` is NotFound.
  - No console errors or warnings anywhere.
- **Mutation:** 15 mutants in the client, all killed by Vitest. They covered replace versus push, dropping the query, the page default, `hasPage`, lens clearing, slug decoding and encoding, the id check, the 404 path, the lens pass-through, a removed route, an extra route, a wrong screen, the admin section lookup, and the NotFound marker. One build mutant, a static import of AdminRoot, was killed by the Go manifest test. Afterwards the build was regenerated and matches the committed dist.

### For CEE6V
- The thread page must read its page and lens through `useThreadView` and call `canonicalThreadUrl` after its load. That keeps AC2 and AC3 true once `ThreadScreen` is replaced.
- The permalink scroll and the landing page are still CEE6V's.

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T10:58:29Z

Review 400 (1 high, 2 medium; all fixed).
(1) High: a thread's 404 persisted after navigating to another thread, because ThreadScreen never cleared 'missing'. A new thread id now resets the title and the 404, and a successful load clears the 404. Three new Vitest tests cover this: a 404, then navigating to another thread that exists; a 404, then a later success on the same thread; and one thread's title not showing while the next loads. Each clear, removed alone, is killed by its own test.
(2) Medium: setPage and setLens dropped the #post-N anchor. They now keep the path and the hash and change only the query, and a test covers it.
(3) Medium: parseInt read page=2junk as page 2. parsePage now accepts only a whole positive integer that is exactly representable. Anything else counts as no page, so a malformed link gives everyone the same view instead of a guess at a prefix. It is tested over 15 inputs.
Mutants on the fixes, all killed: the hash, the regex, the safe-integer check, the page default, and each clear alone and both together. dist rebuilt; 67 Vitest tests.

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T11:03:59Z

Review 401 (medium, fixed): review 400's fixes are all marked resolved. The new finding is that the id reset ran in an effect, after the new route had already rendered once with the old thread's title or 404. Fix: ThreadScreen keeps one answer tagged with the thread id it came from, and reads it only when that id is the one on screen. There is no reset effect left to be late. Two tests record every render of ThreadScreen through preact's options.diffed hook, not just the settled one: moving from a loaded thread never renders its title for the next thread, and moving from a 404 never renders Not found. Put back the effect-reset design that was reviewed, and both tests fail; the keyed version passes. While doing this I found and fixed a leak in the test helper: renderAt never unmounted the previous test's app, which kept following history and rendering into later tests. 69 Vitest tests; dist rebuilt.

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T11:08:15Z

Review 402 (medium, fixed): review 401's finding is marked resolved. The new finding: a stale slug was corrected only when a request came back, so following a link to an old slug of the thread already on screen did not trigger a fetch and left the old slug in the address bar. The redirect now has its own effect, keyed on the current answer and the URL, so it runs on arrival and on any navigation. A test loads the thread under its current slug, then follows a link to an old slug with an anchor. The URL becomes the canonical one with the anchor kept, and no second fetch happens. Mutants: removing url from the effect's dependencies is killed by that test; removing the redirect is killed by three tests. 70 Vitest tests; dist rebuilt.

## Summary

The client routes every non-auth path in docs/07's URL scheme through a table (web/client/src/routes.tsx). A Vitest test renders each URL and diffs the table against the document. Screens not yet built are named placeholders. Page and lens live only in the query string (useThreadView). A stale thread slug is replaced in place with the canonical URL, keeping the ordinal, /history, page and lens. The admin screens are a lazy chunk; a Go test over the embedded Vite manifest keeps them out of the entry, and a browser check confirmed they load only on /admin. CI runs the client tests in an added step.
