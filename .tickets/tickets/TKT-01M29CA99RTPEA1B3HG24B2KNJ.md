---
schema: 3
id: TKT-01M29CA99RTPEA1B3HG24B2KNJ
title: Implement GET /me
type: task
status: ready
status_reason: null
priority: high
due_on: null
labels:
  - area:api
  - effort:S
assignees: []
milestone: phase-1
parent: TKT-01M29BKBMCNDSYHYF9WXAPDNRN
origin: null
dependencies:
  - TKT-01M29C801M9WBEAVCCC6AXNZ4Z
blocks_on: none
references:
  - ref: design:api-resources
    path: docs/07-api-and-clients.md
claim: null
archive: null
created_at: 2026-09-11T23:19:48Z
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

The viewer envelope: user, role, unread counts, pending notification count, and settings. The client bootstraps from it, and the SPA shell inlines its response in a `<script type="application/json">` block to save a round trip on first load.

That inlining is why the response has to be cheap and free of anything expensive to compute. An unread count that costs a table scan turns every page load into one.

## Acceptance criteria

- [ ] Returns the documented shape for an anonymous viewer, a member, a moderator, and a pre-user session
- [ ] Counts come from projections rather than being computed from logs
- [ ] The handler issues at most 2 SQL queries per request, asserted by a test that counts them, so it can be inlined on every shell render

## Implementation plan

Pre-groom plan (2026-09-24). Revise at claim, after reading the code as it stands then.

### Approach
1. Create the API handler package `internal/web/api` if no earlier ticket has. It holds a `Handler` struct carrying the services, a `route(mux)` table, and helpers `decodeJSON`, `writeJSON`, and `viewer(r)`. Errors go through `problem.WriteError`. This ticket is the smallest API ticket, so it is the natural place to create the package, and no other ticket owns it.
2. Budget: at most 2 SQL queries per request, as decided. Query 1 is the session lookup in the middleware (TKT-01M29C5Y3E), which already returns handle, display_name, avatar, role, kind, status, timezone, locale, prefs_json, and mark_all_read_at. Query 2 is one hand-written statement with scalar subqueries: unread threads, participated, watching, and unread `notifications` (`read_at IS NULL`). Anonymous viewers run 0 queries after the middleware.
3. Every count reads projections (`threads.post_count`, `last_post_at`) plus `read_positions`, `watches`, `post_meta`, and `notifications`. Nothing opens a thread log. The unread rule is the docs/06 rule: a row with `last_read_ordinal < post_count`, or no row and `last_post_at >= mark_all_read_at`.
4. Readable-forum filtering: the counts must exclude forums the viewer cannot read. Take the readable forum-id set from the TKT-01M29C5Y2E cache (60 s TTL) and bind it into query 2, so a cache hit adds no query.
5. `service/users.Me(ctx, viewer) (dto.Me, error)`, `GET /api/v1/me`, and shell inlining: add `Me template.JS` (or the marshalled bytes) to `web.ShellData` in internal/web/shell.go. Render it inside `<script type="application/json" id="me" nonce=…>`. html/template treats application/json as a JS context, so the value must be escaped JSON, never a raw string. Keep the shell `no-store`.
6. Settings.page_size comes from `prefs_json`, with 25 when unset (decided default, docs/07 "Page size").

### Files
internal/web/api/{api.go,me.go}, internal/service/users/me.go, internal/web/shell.go (ShellData plus the default template). The existing `dto.Me`, `dto.UnreadCounts`, `dto.Settings`, and goldens me.{anon,member,moderator}.json are reused. Add a `me.pre_user` golden once its shape is pinned.

### Tests
- AC1: goldens for anon, member, and moderator already exist. Add handler tests with httptest that decode the response with `DisallowUnknownFields` into `dto.Me` and compare against seeded data. pre_user is blocked on the open item below.
- AC2: a query-counting `database/sql` driver wrapper (or sqlc `DBTX` wrapper) asserts at most 2 queries for a member and 1 for anon, including the middleware. The budget is stated in the handler's doc comment.
- AC3: an import/AST test that internal/service/users/me.go does not import internal/store/threadlog or threadcache. Plus a test that counts match after the projection is seeded, with no logs on disk at all.
- A shell test: the inlined JSON parses, carries the nonce, and a handle containing `</script>` stays escaped.

### Design
Reread docs/07 "Resources" (/me) and "Preact client / Embedding", docs/06 "Read tracking", and docs/04 "read_positions".

### Interactions
Consumes TKT-01M29C5Y3E (Viewer carrying user fields), TKT-01M29C5Y2E (readable forum set), TKT-01M29BYYHQ (read_positions, watches, notifications), and TKT-01M29BYYJ8 (threads, post_meta). Creates `internal/web/api`, which every other API ticket then extends.
Open: the pre_user shape is undocumented. `dto.Me` says a nil User is the only signed-out signal, so a pre_user marker needs a docs/07 decision (TKT-01M29CKRG9 wants it).
Open: no projection holds per-user aggregates. "participated" over post_meta may be the expensive term; measure at 10k threads before adding a counter table.
Open: whether the budget counts the middleware query and a permission-cache miss. The plan assumes the middleware counts and a miss does not.
Open: `users.mark_all_read_at` appears in docs/06 but not in the docs/04 users column list.

### Settled in grooming (2026-09-24)

- A pre-user session gets `user: null` plus a separate `pre_user` object. A null user stays the only signed-out signal. Document it in docs/07 with a golden test.
- The 2-query budget covers the handler. The session middleware's single lookup is outside it.

## Notes

**agent:claude/phase1-groom** at 2026-09-24T05:06:25Z

Acceptance criteria rewritten during Phase 1 grooming (2026-09-24), agreed with the user as part of preparing the autonomous run. Rewritten criteria move to the end of the list, so their numbers changed. 'Cheap enough, with a stated query budget' did not state the budget. It is now at most 2 queries, one for the viewer and session and one for the projected counts, asserted by a test that counts queries.
