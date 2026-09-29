---
schema: 3
id: TKT-01M29CP2R48HV8ZW0QVRK2JM6N
title: Build the audit log query and admin view
type: task
status: ready
status_reason: null
priority: normal
due_on: null
labels:
  - area:moderation
  - effort:M
assignees: []
milestone: phase-2
parent: TKT-01M29BN36JH4M1W5819D06PGPP
origin: null
dependencies:
  - TKT-01M29BYYJ0WJ3C7RRZQMDRP5ZB
  - TKT-01M3NRYCP5BSA717S1HANWY1AA
blocks_on: none
references:
  - ref: design:moderation
    path: docs/08-moderation.md
claim: null
archive: null
created_at: 2026-09-11T23:26:15Z
updated_at: 2026-09-29T07:18:12Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase2-run-orchestrator
  name: ""
extensions: {}
---

## Description

A filterable view over `audit_log` by actor, action, target, and date, and the query API behind it.

The log is only worth writing if somebody can read it. In a small community the common question is "who moved this thread and why", asked by a member who is annoyed, and an admin who can answer it in ten seconds defuses something that otherwise festers.

## Acceptance criteria

- [ ] No code path in the application updates or deletes an entry
- [ ] GET /api/v1/admin/audit filters by actor handle or system, action or action prefix, target type and id, and a time range, alone and combined, and pages newest first by an opaque keyset cursor (TestAuditPageFilters, TestAuditPageKeyset)
- [ ] Each entry whose target is a user, forum, thread, post or setting carries an href to it and the screen renders it as a link, and an absent user draws as deleted-user-<id> (TestAuditTargetsLink)
- [ ] The endpoint and /admin/audit answer 401 to anonymous and 403 to pre-user, member and moderator viewers, forum-scoped moderators included, and 200 to admins, and docs/08 records that the log is admin-only (TestAuditIsAdminOnly)

## Implementation plan

### Settled in grooming

- `audit_log` is append-only. Triggers refuse UPDATE, DELETE and a REPLACE, and `TestAuditLogAppendOnly` (`internal/store/db/moderation_schema_test.go`) refuses any query or SQL literal in `internal/` that tries (docs/04 "audit_log"). AC4 holds today. This ticket must keep it holding and add no write path.
- Every moderation and administration action appends a row (docs/08 intro). Writing those rows belongs to each action's ticket. This ticket reads them.
- Rows reach the table through `internal/audit`, whose target constructors fix the `target_id` formats. The target table is in docs/04 (TKT-01M3NRYCP5, a dependency).
- **Maintainer decision 2026-09-29:** the audit log is admin-only, and moderators see none of it. The alternatives were rows about threads in forums a moderator moderates, which lost because audit rows carry no forum and a thread's current forum is not the one it was in when the action happened, and a moderator's own actions only. A note on this ticket records the decision.
- Admin screens are the lazy admin chunk and do not count against the 60000 B budget (docs/07 "Preact client"). The built client is committed: run `go generate ./internal/web/spa` after any client change.
- Lists paged by time are cursor-paged with an opaque cursor, and a malformed cursor is `bad-request` (docs/07 "JSON API").

Engineering choices made here:
- Keyset paging on `id` descending rather than on `at`. Ids are assigned in insert order and are never reused, since rows are never deleted, so `id` is the insertion order with no ties. `at` can tie, and a CLI write can carry a clock that went backwards.
- The filter query is hand-written in `internal/store/db/views`, because its shape depends on which filters are present. docs/04 "Engine" puts dynamic view queries there and not in sqlc.
- The screen keeps its filters in the URL (`/admin/audit?actor=…`), the rule docs/07 "Path to a non-JS client" item 3 sets for lenses and pages. An answer to "who moved this thread" is then a link that can be pasted.

### Approach

1. Migration (the next free number): `CREATE INDEX audit_log_actor ON audit_log (actor_id, id)` and `CREATE INDEX audit_log_action ON audit_log (action, id)`. `audit_log_target (target_type, target_id, at)` already exists.
2. `internal/store/db/views/audit.go`: `AuditFilter{Actor *int64; System bool; Action string; ActionPrefix bool; TargetType, TargetID string; From, To int64; Before int64; Limit int}` and `AuditPage(ctx, tx, f) ([]dbq.AuditLog, error)`. It uses `?` parameters only and ends `ORDER BY id DESC LIMIT ?`. `From` is inclusive and `To` exclusive, in unix ms. `System` selects `actor_id IS NULL`, which is the command line and the expiry jobs.
3. `internal/service/audit` (a new package, beside the `internal/audit` writer): `Service{DB}` with `Query(ctx, v, q Query) (dto.AuditPage, error)`.
   - The gate comes from TKT-01M3NRYCP5's `admin`.
   - Parameters: `actor` (a handle, or `system`), `action` (exact, or a prefix ending in `.` such as `user.`), `target_type` with `target_id`, `from`, `to`, `cursor`. An unknown actor handle returns an empty page rather than an error, since the caller is an admin and nothing leaks. A `target_id` without a `target_type` is `bad-request`.
   - Page size 50. `next_cursor` is the last id, base64url-encoded, and empty at the end.
   - Resolution, batched per page: actors, and user targets, through `UsersByIDs`, drawn with `member.Author` (an absent user draws as `member.Missing`). Thread and post targets read `threads` (title, slug) and get `/t/{id}/{slug}` or `/t/{id}/{slug}/{ordinal}`. Forum targets read `forums` and get `/f/{slug}`. Setting targets get the key as the label and `/admin/settings` as the link. Flag and application targets get `/admin/queue`. An unknown type, or a row that no longer resolves, gets its raw id as the label and no `href`.
4. `internal/dto`: `AuditPage{Entries []AuditEntry; NextCursor string}` and `AuditEntry{ID, At int64; Actor *Author; Action string; Target AuditTarget; Details json.RawMessage; IP string}`, with `AuditTarget{Type, ID, Label, Href string}`. Goldens: `audit.admin.json` (system and member actors, so the nullable actor appears both null and set, as the corpus rules require) and `audit-filtered.admin.json`. Import both in `web/client/src/api/dto-contract.ts`.
5. `internal/web/api/admin_audit.go`: `GET /api/v1/admin/audit`, `no-store`.
6. Client: `web/client/src/screens/admin/AuditScreen.tsx` in the admin chunk. It has a filter form (actor handle, action select of known actions plus free text, target type and id, from and to dates in local time, converted to ms), a table newest first (time, actor, action, target link, details as key and value pairs, IP), and an "Older" button that follows the cursor. Add `/admin/audit` to the `AdminRoot` sections, to `routes.tsx`, and to docs/07's URL scheme, because `routes.test.tsx` diffs the table against that document.
7. Docs: docs/07 "Moderation and admin" documents the endpoint (parameters, codes, page size, cursor). docs/08 records that the log is admin-only (maintainer decision 2026-09-29).
8. Run `go generate ./internal/web/spa`, commit `dist`, and run `npm --prefix web/client run budget`. The reader total must not move, because the screen is in the admin chunk.

### Tests

- `views`: `TestAuditPageFilters` covers each filter alone and combined, the `From` and `To` bounds, the `system` actor and an action prefix. `TestAuditPageKeyset` checks that a row appended between two pages appears on neither and nothing repeats. `TestAuditQueriesUseAnIndex` runs `EXPLAIN QUERY PLAN` for the actor, action and target filters and fails on a full scan of `audit_log`.
- `service/audit`: `TestAuditIsAdminOnly` expects anonymous 401, pre-user 403, member 403, moderator 403 (a forum-scoped moderator included), and admin 200. `TestAuditTargetsLink` covers user, thread, post, forum and setting targets and their hrefs, plus an absent user drawn as `deleted-user-<id>`. `TestAuditCursorIsOpaque` expects a malformed cursor to be `bad-request`.
- `TestAuditLogAppendOnly` passes unchanged.
- dto goldens and the corpus rules.
- Client: `AuditScreen.test.tsx` checks that filters round-trip through the URL, that "Older" appends the next page, and that targets render as links. `routes.test.tsx` passes with the docs/07 change. `TestTheAdminBundleIsSeparate` stays green.

### Out of scope

- Writing audit rows. Each action's ticket does that, through `internal/audit` (TKT-01M3NRYCP5).
- Any moderator view of the log (maintainer decision 2026-09-29).
- Export, retention or pruning. The log is append-only and nothing removes a row.

## Notes

**agent:claude-code/phase2-groom** at 2026-09-29T05:13:04Z

Open decision for the maintainer (grooming, 2026-09-29): what a moderator may see in the audit log. (a) nothing, admin-only; (b) rows about threads and posts in forums they moderate, joined through the thread's current forum; (c) their own actions only. Recommended: (a). (b) answers with the forum a thread is in now, not the one it was in when the action happened. The plan builds (a), so that only the gate changes if another option is chosen. See the plan's Open decision.

**agent:claude-code/phase2-groom** at 2026-09-29T05:41:11Z

Maintainer decision 2026-09-29: the audit log is admin-only; moderators see none of it. This supersedes the open decision in the earlier note. The plan's Settled section records it.
