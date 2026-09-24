---
schema: 3
id: TKT-01M29CEE52GKX043EGKG9DWKFE
title: Build the client router and route table
type: task
status: in-progress
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
claim:
  actor: agent:claude-code/phase1-run-415af6aa
  branch: run/cee52-client-router
  worktree: /home/sothr/.t3/worktrees/ketju/t3code-415af6aa
  commit: 257372c9ffd5c13cf760606a30ed53a23488882d
  session: null
  claimed_at: 2026-09-24T10:46:05Z
  expires_at: null
archive: null
created_at: 2026-09-11T23:22:04Z
updated_at: 2026-09-24T10:47:12Z
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

- [ ] Every path in the docs/07 URL scheme resolves to a screen, tested by a route table test
- [ ] Lens and page live in the URL and a shared link reproduces the exact view
- [ ] A stale slug redirects to the canonical URL preserving page and lens
- [ ] The admin bundle loads only on an admin route

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
