---
schema: 3
id: TKT-01M29CEE52GKX043EGKG9DWKFE
title: Build the client router and route table
type: task
status: draft
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
claim: null
archive: null
created_at: 2026-09-11T23:22:04Z
updated_at: 2026-09-24T05:21:03Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude/phase1-groom
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

Pre-groom plan (2026-09-24). Revise at claim, after reading the code as it stands then.

### Approach
1. `src/routes.ts` exports `routes: {path, screen, admin?}[]`, transcribed one-for-one from the docs/07 "URL scheme" block: `/`, `/f/:forum`, `/f/:forum/new`, `/t/:id/:slug`, `/t/:id/:slug/:ordinal`, `/t/:id/:slug/:ordinal/history`, `/unread`, `/new`, `/participated`, `/watching`, `/search`, `/u/:handle`, `/u/:handle/posts`, `/settings`, `/settings/sessions`, `/settings/identities`, `/apply`, `/apply/status`, and `/admin` with its six children. `/auth/*` is server-only and must not be a client route.
2. `app.tsx` builds a `preact-iso` `<Router>` from that table. Screens not built in Phase 1 map to `Placeholder` with a title, so every path still "resolves to a screen".
3. Admin routes use `lazy(() => import('./screens/admin/AdminRoot'))` so they emit a separate chunk.
4. State lives in the URL. `ThreadPage` reads `page` and `lens` from `useRoute().query`, and changing either calls `route()` with the new query. Nothing about the view is kept only in a signal.
5. Stale slug: after `GET /threads/{id}` returns, if `thread.slug !== params.slug`, call `route(canonicalUrl, true)` (replace) with the same `page`, `lens` and ordinal suffix.
6. Permalink `/t/:id/:slug/:ordinal` scrolls to `#post-N` once the containing page loads. Resolving that page needs server support; see interfaces item 7.

### Files
`web/client/src/routes.ts`, `src/app.tsx`, `src/screens/Placeholder.tsx`, `src/screens/NotFound.tsx`, `src/screens/admin/AdminRoot.tsx`.

### Tests
- AC1: a Vitest route-table test, `routes.test.ts`, holds a fixture list of concrete URLs (one per docs/07 line), matches each through preact-iso's exported matcher, and asserts it resolves to a non-NotFound screen. A second assertion diffs the table against the docs/07 list so neither side drifts silently. Parse the doc's code block if that is practical; otherwise keep a committed copy.
- AC2: Playwright opens `/t/{id}/{slug}?page=2&lens=author:alice`, reloads, and asserts the same posts. It then opens the same URL in a new context.
- AC3: Playwright with a seeded renamed thread opens the old slug with `?page=2&lens=…` and asserts the URL is replaced with the new slug and the query is kept.
- AC4: a build-output check reads the Vite manifest and asserts `AdminRoot` is a dynamic chunk not imported by the entry. Playwright records network requests on `/` and asserts no admin chunk loads.

### Design
docs/07 "URL scheme", "Principles" (URLs the same in both clients), "Path to a non-JS client" rule 3; docs/06 "Paged, not infinite".

### Interactions
Depends on CCC02. Every screen ticket registers here. CEE6V owns landing and permalink scroll. CGBXK hooks route-change focus. CGBYH depends on the admin split to keep the thread-view bundle small.
Open in docs: stale-slug handling is specified for the thread id only; forum slug renames are not addressed.
