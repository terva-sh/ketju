---
schema: 3
id: TKT-01M29CP2R48HV8ZW0QVRK2JM6N
title: Build the audit log query and admin view
type: task
status: in-progress
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
claim:
  actor: agent:claude-code/phase2-run-cp2r4
  branch: run/cp2r4-audit-log
  worktree: /home/sothr/workspace/git.local.sothr.com/terva-sh/ketju/.claude/worktrees/agent-aa80f4148362320ca
  commit: 94ce6d89dad22d6baaeef0a790ec8e75ab87ba42
  session: null
  claimed_at: 2026-09-30T10:42:43Z
  expires_at: null
archive: null
created_at: 2026-09-11T23:26:15Z
updated_at: 2026-09-30T10:42:46Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase2-run-cp2r4
  name: ""
extensions: {}
---

## Description

A filterable view over `audit_log` by actor, action, target, and date, and the query API behind it.

The log is only worth writing if somebody can read it. In a small community the common question is "who moved this thread and why", asked by a member who is annoyed, and an admin who can answer it in ten seconds defuses something that otherwise festers.

## Acceptance criteria

- [ ] GET /api/v1/admin/audit filters by actor handle or system, action or action prefix, target type and id, and a time range, alone and combined, and pages newest first by an opaque keyset cursor (TestAuditPageFilters, TestAuditPageKeyset)
- [ ] No code path in the application updates or deletes an entry, and this ticket adds no write path (TestAuditLogAppendOnly, TestOneWayIntoAuditLog)
- [ ] Each entry whose target is a user, forum, thread, post, setting, flag, application, group, permission or form carries an href and the screen renders it as a link; a blob or identity target has none; an absent user draws as deleted-user-<id>; a thread.moved entry carries both forums in related (TestAuditTargetsLink)
- [ ] The endpoint and /admin/audit answer 401 to anonymous and 403 to pre-user, member and moderator viewers, forum-scoped moderators and members who moderate through a group included, and 200 to admins, and docs/08 records that the log is admin-only (TestAuditIsAdminOnly)
- [ ] audit.Actions lists every action written today, post.redacted, blob.deleted, thread.title_redacted, thread.renamed, thread.moved, thread.slow_mode, user.renamed, flag.resolved and flag.dismissed included; it matches docs/04 audit_log in both directions; and the answer's actions fill the screen's action filter (TestActionsDocumented, AuditScreen.test.tsx)
- [ ] The screen lives under src/screens/admin, TestTheAdminBundleIsSeparate passes, the reader total stays at 47415 B, and the dist from go generate ./internal/web/spa is committed

## Implementation plan

Groomed 2026-09-29. Refreshed 2026-09-30 against main at 5124209.

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

Added in the 2026-09-30 refresh:
- **The audit log stays admin-only under the forum-grant decision.** The maintainer decided on 2026-09-30 that thread moderation follows the forum grant, and that "Admin screens and account actions keep their role gates" (docs/08 "Decided 2026-09-30", on PR #208's branch). TKT-01M3RQP0TG names the audit log among the screens that keep their gate. A member who moderates a forum through a group is refused here, as a moderator by role is.
- **docs/08 already records that the log is admin-only**, under "Decided 2026-09-29" on main. The earlier Approach step that added it is removed, and AC4's docs half is already met.

Engineering choices made in the refresh, each with the alternative that lost:
- **The service package is `internal/service/auditlog`, not `internal/service/audit`.** It imports `internal/audit` for the target types and `ParsePost`, and two packages named `audit` in one file need an alias at every import. Keeping the earlier name lost for that reason alone.
- **The known actions are a closed list in `internal/audit`, `audit.Actions`, held to docs/04 "audit_log" by `TestActionsDocumented` in both directions.** The screen's action filter offers the list, and free text for anything else. The answer carries the list, so the client holds no copy. Two alternatives lost. `SELECT DISTINCT action` over the table scans the index on every page load, and it offers only actions that have happened. Making `audit.Append` refuse an unlisted action would make the list complete by construction, but a missed action would then fail a moderator's action in production rather than only the filter.
- **User targets link to `/u/{handle}`**, the profile. The admin detail at `/admin/users?user={handle}` needs the address change TKT-01M3P0CYR3 makes, and this ticket does not wait on it. Once that lands, the href can change in one place (`targetHref`).
- **A moved thread's forums are resolved.** `thread.moved` carries `from_forum` and `to_forum` as ids. The entry carries them as `related` targets with the forums' names and `/f/{slug}` links, because "who moved this thread, and from where" is the question the ticket names.

### What changed since the plan was written

Read from main at 5124209:
- The target types are now twelve: `user`, `forum`, `group`, `flag`, `application`, `thread`, `post`, `setting`, `permission`, `form`, `identity` and `blob` (`audit.Types`, docs/04 "audit_log"). `blob` came with hard redaction (PR #192), and `identity` came with application review.
- The actions written today, and the ones this screen must draw:
  - `thread.renamed` and `thread.moved` (PR #193). `thread.renamed` carries no title, old or new (maintainer decision 2026-09-29 on TKT-01M3NS147C), so the label is the thread's current title.
  - `thread.slow_mode` (PR #188), with `from` and `to`.
  - `user.renamed` (PR #186), with `from` and `to`, the old and new handle.
  - `post.redacted`, `blob.deleted` and `thread.title_redacted` (PR #192). Each is written with `"via": "cli"` and the operator as actor. `thread.title_redacted` never carries title text.
  - `flag.resolved` and `flag.dismissed` (PR #185), against the post. docs/08 "Flags" documents them. docs/04 does not yet, so this ticket adds them there.
  - `agent.credential_issued`, `agent.credential_revoked`, `agent.limits_changed` and `user.created` (TKT-01M29P9RSW).
  - `user.posts_hidden` and `user.posts_restored` (TKT-01M29CP2QK).
- `member.AuthorsByID` (`internal/service/member/account.go`) resolves actors in one batch. It draws an absent user with `member.Missing` and an anonymized one with `member.Author`.
- The newest migration is 0017 (`0017_thread_slow_mode.sql`), so the index migration is 0018 unless another ticket claims it first.
- docs/07 "Moderation and admin" is now split into `####` subsections, one per area. The endpoint gets its own subsection.

### Approach

1. **Migration `0018_audit_log_filters.sql`** (the next free number): `CREATE INDEX audit_log_actor ON audit_log (actor_id, id)` and `CREATE INDEX audit_log_action ON audit_log (action, id)`. `audit_log_target (target_type, target_id, at)` and `audit_log_at` already exist (0006).
2. **`internal/audit`**: `Actions []string`, every action written today (listed above), sorted. docs/04 "audit_log" gains an `<!-- audit-actions:begin -->` table of action, target type and ticket, fenced as the `audit-targets` table already is, so the prose's other dotted names (`registration.mode`) are not read as actions. `TestActionsDocumented` reads that table and fails on a name in one and not the other, as `TestTargetTypesDocumented` does for types. The table includes `flag.resolved` and `flag.dismissed`, against the post, which docs/04 lacked.
3. **`internal/store/db/views/audit.go`**: `AuditFilter{Actor *int64; System bool; Action string; ActionPrefix bool; TargetType, TargetID string; From, To int64; Before int64; Limit int}` and `AuditPage(ctx, tx, f) ([]dbq.AuditLog, error)`. It uses `?` parameters only, and ends with `ORDER BY id DESC LIMIT ?`. `From` is inclusive and `To` exclusive, in Unix ms. `System` selects `actor_id IS NULL`, which covers the command line, the expiry job, the auto-hide and thread-log rows with no actor.
4. **`internal/service/auditlog`** (new, beside the `internal/audit` writer): `Service{DB}` with `Query(ctx, v, q Query) (dto.AuditPage, error)`.
   - The gate is the admin gate (`adminGate`, `internal/web/api/admin.go`).
   - Parameters: `actor` (a handle, or `system`), `action` (exact, or a prefix ending in `.` such as `user.`), `target_type` with `target_id`, `from`, `to` and `cursor`. An unknown actor handle returns an empty page rather than an error: the caller is an admin, and nothing leaks. A `target_id` without a `target_type` is `bad-request`, and so is a `target_type` not in `audit.Types`.
   - The page size is 50. `next_cursor` is the last id, base64url-encoded, and empty at the end.
   - Resolution is batched per page, with one statement per kind: actors and user targets through `member.AuthorsByID`; threads (title, slug) for thread and post targets and for flags' posts; forums for forum targets and for `thread.moved`'s `from_forum` and `to_forum`; flags (thread, ordinal) for flag targets.
   - `targetHref` gives each type its link:
     - user: `/u/{handle}`;
     - thread: `/t/{id}/{slug}`;
     - post: `/t/{id}/{slug}/{ordinal}`;
     - forum: `/f/{slug}`;
     - setting: `/admin/settings`, with the key as the label;
     - flag: the flagged post's link, labelled "flag {id} on #{ordinal}";
     - application: `/admin/applications?id={id}`;
     - group: `/admin/users?tab=groups`;
     - permission: `/admin/users?tab=permissions`;
     - form: `/admin/forms`;
     - identity: no link, labelled "identity {id}";
     - blob: no link, since a `blob.deleted` blob is gone. The label is the first 12 hex digits, and the full hash is in `id`.
   - A row whose target no longer resolves gets its raw id as the label and no `href`.
5. **`internal/dto`**: `AuditPage{Entries []AuditEntry; Actions []string; NextCursor string}` and `AuditEntry{ID int64; At Millis; Actor *Author; Action string; Target AuditTarget; Related []AuditTarget; Details json.RawMessage; IP string}`, with `AuditTarget{Type, ID, Label, Href string}`. `Related` is empty (`[]`), not null, except for `thread.moved`. Goldens: `audit.admin.json`, with a system actor and a member actor so the nullable actor appears both null and set, as the corpus rules require, and one `thread.moved` row with `related` set; and `audit-filtered.admin.json`. Import both in `web/client/src/api/dto-contract.ts`.
6. **`internal/web/api/admin_audit.go`**: `GET /api/v1/admin/audit` through `adminRoute` with `adminGate`, `no-store`. Register it in `registerAdmin`.
7. **Client**: `web/client/src/screens/admin/AuditScreen.tsx`, in the admin chunk.
   - A filter form: actor handle, an action select of `actions` plus free text, target type (a select of the twelve) and id, and from and to dates in local time, converted to ms.
   - A table, newest first, with the time, the actor, the action, the target link, `related` links, the details as key and value pairs, and the IP. Details render as text, never HTML.
   - An "Older" button follows the cursor.
   - Add `/admin/audit` to `sections.ts` (`isAdmin`), to `routes.tsx`, and to docs/07's URL scheme line, because `routes.test.tsx` diffs the table against that document.
8. **Docs**: a new `####` subsection under docs/07 "Moderation and admin", "The audit log (TKT-01M29CP2R4)", documents the endpoint: parameters, codes, page size and cursor. docs/04 "audit_log" gains the two flag actions (step 2).
9. Run `go generate ./internal/web/spa`, commit `internal/web/spa/dist`, and run `npm --prefix web/client run budget`. The reader total must stay at 47415 B, because the screen is in the admin chunk.

### Tests

- `views`: `TestAuditPageFilters` covers each filter alone and combined, the `From` and `To` bounds, the `system` actor and an action prefix. `TestAuditPageKeyset` checks that a row appended between two pages appears on neither, and that nothing repeats. `TestAuditQueriesUseAnIndex` runs `EXPLAIN QUERY PLAN` for the actor, action and target filters and fails on a full scan of `audit_log`.
- `audit`: `TestActionsDocumented`.
- `service/auditlog`, or `internal/web/api/admin_audit_test.go`:
  - `TestAuditIsAdminOnly`: anonymous 401; pre-user, member and moderator 403, including a forum-scoped moderator and a member who holds `moderate` through a group; admin 200.
  - `TestAuditTargetsLink`: user, thread, post, forum, setting, flag, application, group, permission and form targets have their hrefs. A blob and an identity have none. An absent user draws as `deleted-user-<id>`. A `thread.moved` row carries both forums in `related`.
  - `TestAuditCursorIsOpaque`: a malformed cursor is `bad-request`, and so are a `target_id` alone and an unknown `target_type`.
- `TestAuditLogAppendOnly` and `TestOneWayIntoAuditLog` pass unchanged.
- The dto goldens and the corpus rules.
- Client: `AuditScreen.test.tsx` checks that the filters round-trip through the URL, that "Older" appends the next page, that targets and `related` render as links, and that a detail holding `<b>` renders as text. `routes.test.tsx` passes with the docs/07 change. `TestTheAdminBundleIsSeparate` stays green.

### Out of scope

- Writing audit rows. Each action's ticket does that, through `internal/audit` (TKT-01M3NRYCP5). The forum writes (`forum.*`) arrive with TKT-01M29CXK19, which adds them to `audit.Actions` and docs/04 if it lands after this ticket.
- Any moderator view of the log (maintainer decisions of 2026-09-29 and 2026-09-30).
- Export, retention or pruning. The log is append-only and nothing removes a row.
- Linking user targets to the admin detail: possible once TKT-01M3P0CYR3 puts the selected account in the address.

## Notes

**agent:claude-code/phase2-groom** at 2026-09-29T05:13:04Z

Open decision for the maintainer (grooming, 2026-09-29): what a moderator may see in the audit log. (a) nothing, admin-only; (b) rows about threads and posts in forums they moderate, joined through the thread's current forum; (c) their own actions only. Recommended: (a). (b) answers with the forum a thread is in now, not the one it was in when the action happened. The plan builds (a), so that only the gate changes if another option is chosen. See the plan's Open decision.

**agent:claude-code/phase2-groom** at 2026-09-29T05:41:11Z

Maintainer decision 2026-09-29: the audit log is admin-only; moderators see none of it. This supersedes the open decision in the earlier note. The plan's Settled section records it.

**agent:claude-code/phase2-groom** at 2026-09-30T08:53:41Z

Plan refreshed against main at 5124209 on 2026-09-30. The Settled items are carried verbatim; new ones follow them. Changes: (1) The audit view now resolves all twelve target types. blob (PR #192) and identity draw with no link, and group, permission, form, flag and application now have links. It must also render the actions landed since: thread.renamed and thread.moved (#193), thread.slow_mode (#188), user.renamed (#186), post.redacted, blob.deleted and thread.title_redacted (#192), and flag.resolved and flag.dismissed (#185). (2) A closed list, audit.Actions, is held to docs/04 by TestActionsDocumented and fills the action filter. docs/04 gains the two flag actions it lacked. (3) The service package is renamed internal/service/auditlog, so that it does not share a name with internal/audit. (4) thread.moved's forum ids are resolved into related targets. (5) The migration is 0018, since 0017 is the newest. (6) docs/08 already records that the log is admin-only, so that step is removed. (7) The maintainer's decision of 2026-09-30 keeps admin screens on their role gates, so a member who moderates through a group is refused too. TestAuditIsAdminOnly covers that case. (8) User targets link to /u/{handle} until TKT-01M3P0CYR3 puts ?user= in the address. Criteria 1, 3 and 4 are rewritten, and two are added.
