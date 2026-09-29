---
schema: 3
id: TKT-01M29CKRFSDCGKH605X8Y4RT3C
title: "Implement the account lifecycle: deactivate, suspend, anonymize"
type: task
status: ready
status_reason: null
priority: high
due_on: null
labels:
  - area:identity
  - area:moderation
  - effort:M
assignees: []
milestone: phase-2
parent: TKT-01M29BKBKZW26HNHFM1YFFGVZC
origin: null
dependencies:
  - TKT-01M29BYYH73ZBC9WTS7HN03PKJ
  - TKT-01M29C5Y3E5SKHFR2SHJ8Y60TP
  - TKT-01M3NRYCP5BSA717S1HANWY1AA
blocks_on: none
references:
  - ref: design:account-lifecycle
    path: docs/05-identity-and-accounts.md
claim: null
archive: null
created_at: 2026-09-11T23:24:59Z
updated_at: 2026-09-29T07:18:23Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase2-run-orchestrator
  name: ""
extensions: {}
---

## Description

Deactivate by self: sessions revoked, identities kept, posts kept, and the user can return through any linked identity. Suspend by a moderator: the same plus a reason shown at login and a duration. Anonymize by an admin: handle becomes `deleted-user-<id>`, display name and avatar cleared, identities removed, and attribution suppressed at render time by a flag.

Anonymize does not edit post bodies, because they are in append-only logs. Erasing body text is the offline redaction tool, and the two must not be confused when somebody arrives with a legal request.

## Acceptance criteria

- [ ] Deactivate, suspend and anonymize each delete the user's sessions in the transaction that changes the status, and the old cookie's next request is anonymous (TestEachTransitionSignsOutAtOnce)
- [ ] Each transition writes its audit_log row (user.deactivated, user.suspended with reason and until, user.unsuspended, user.anonymized with no handle) in the same transaction as the status change; with an injected audit failure the status, sessions and identities are unchanged
- [ ] Signing in as a suspended user answers 403 with the reason, HTML-escaped, and the end date or 'No end date is set', and issues no session
- [ ] Anonymize sets users.anonymized, renames to deleted-user-<id>, clears display name, avatar and bio, and deletes identities; a schema trigger refuses any later change to those fields, and thread log files are byte-identical before and after
- [ ] After anonymization, the post header, thread-list last poster, profile, history, mentions and quotes of the account show deleted-user-<id> with no display name or avatar, read from users.anonymized, and a cached page is not served with the old author (TestAnonymizedAttributionEverywhere, TestResponseCacheAfterAnonymize)
- [ ] Status writes go only through perm (SetUserStatus and AnonymizeUser are in decisionWrites), and the last active admin cannot be suspended, deactivated, anonymized or demoted (TestLastAdminCannotLeave)
- [ ] Moderators can suspend members only, and admins anyone except themselves (TestSuspendAuthority)
- [ ] A deactivated member who signs in sees a Reactivate button; using it returns the account to active through session.IssueUserWith and writes user.reactivated, and a suspended, pending or anonymized account cannot use it (TestReactivation)
- [ ] After anonymization the old handle is reserved for 90 days with no redirect (once TKT-01M3NRP3W9's reservation table exists), and docs/05 says anonymization leaves earlier audit rows, old handles included, in place
- [ ] After suspend or deactivate, signing in through any linked identity writes no session row; after anonymize the identities are gone and the old identity id is ErrNoIdentity; the chokepoint's issuerCallers gains only identity/reactivate.go:Reactivate

## Implementation plan

Groomed 2026-09-29 for Phase 2 (TKT-01M3M9RR26) against main at 7af2c5a.

### Settled in grooming

- **Deactivate (self).** Sessions are revoked. Identities are kept, and posts are kept with the handle shown (docs/05 "Account lifecycle").
- **Suspend (moderator).** It works like deactivate, plus a reason shown at login and a duration. Hiding the user's posts is an option (docs/05; docs/08 "Primitives": moderator, `audit_log` + `users.status`, reversible). There is no shadow banning: the member can read the reason (docs/08).
- **Anonymize (admin, irreversible).** The handle becomes `deleted-user-<id>`, the display name and avatar are cleared, and the identities are removed. Post bodies are not edited, because they live in append-only logs. A `users.anonymized` flag suppresses attribution at render time (docs/05). GDPR erasure of body text is the offline redaction tool (docs/03 "Hard redaction", decided 2026-09-28, TKT-01M29BSPE2; the tool is TKT-01M29CXJZ9). docs/08 lists anonymize as admin-only and not reversible.
- **Sessions.** Suspending a user deletes their sessions (docs/05 "Sessions and CSRF"). The session middleware also builds every viewer from the users row, so a user who is not active has no member rights on the next request even before the delete (`session.Session.Viewer`).
- **Signing in.** A suspended, deactivated or pending account that signs in gets an "account unavailable" page and no session. The reason and any end date come with the Phase 2 moderation columns, which this ticket adds (docs/05 "Login flow"). `session.IssueUserWith` is the only way to a member session, and it refuses any status but `active` (TKT-01M29C7ZZR).
- **Profiles** (decided 2026-09-28, TKT-01M3KJJNDC; docs/05, docs/07 `GET /users/{handle}`). A pending account is `not-found` except to moderators and admins. A suspended or deactivated account's profile and history are served to members, with its status shown only to staff. An anonymized account shows `deleted-user-<id>` and no display name, avatar or bio. `domain.Viewer.MaySeeAccount` and `member.Account` implement this already. Until `users.anonymized` exists, the handle prefix is what marks an anonymized account (`domain.Anonymized`). This ticket adds the column and moves every reader onto it.
- **Agents.** For an agent, suspension and deactivation revoke its credential rather than its sessions (docs/05). TKT-01M29P9RSW owns that criterion (its AC4).
- **Renames.** An anonymized account cannot be renamed (docs/05 "Changing a handle", decided 2026-09-28). TKT-01M3NRP3W9 builds renames and reads the column this ticket adds.
- **Posts that name the account.** Quotes attribute by ordinal ("Quote from #37", "Post #37" in `internal/render/quote.go`) and never by name, so an old quote shows no handle. The quoted post's header, drawn from the users row, is what must be suppressed. Mentions resolve by user id, and a mention of an anonymized account renders with the current handle, `deleted-user-<id>`.
- **Cache.** The response cache's authors digest covers every author field a page draws, so an anonymization written by another process still changes the key (docs/07 "The response cache").
- **Returning after deactivation** (maintainer decision 2026-09-29). A deactivated member reactivates with a button on the "account unavailable" page. The page offers "Reactivate my account", which posts to `POST /auth/reactivate` with a sealed, five-minute `__Host-ketju_reactivate` cookie that names the identity. The handler reactivates inside `IssueUserWith`'s create func, moving only `deactivated → active`, and writes `user.reactivated`. A suspended, pending or anonymized account is never reactivated this way. This settles the contradiction in docs/05 between "can return via any linked identity" and the "Login flow" rule that a deactivated account gets no session. The implementing PR writes the rule into both places. Lost alternatives: reactivating silently at sign-in, which would let a stale IdP session undo a deliberate deactivation, and admin-only reactivation.
- **Who may change whose status** (maintainer decision 2026-09-29). Moderators suspend members. Admins suspend anyone except themselves. The last active admin cannot be suspended, deactivated, anonymized or demoted, so the forum cannot lock itself out. Anonymize stays admin-only (docs/08).
- **An anonymized account's old handle** (maintainer decision 2026-09-29). It is reserved for 90 days with no redirect. A redirect would tie the old handle to `deleted-user-<id>`. The reservation row does not name the user.
- **Audit rows keep old handles** (maintainer decision 2026-09-29). Rows written before anonymization, such as a rename's old and new handle (TKT-01M3NRP3W9), stay in the append-only `audit_log`. docs/05 "Account lifecycle" says so: `audit_log` is an admin-only operational record, and anonymization does not reach it. The `user.anonymized` row this ticket writes names no handle.

### Approach

1. **Migration** `internal/store/db/migrations/00NN_account_lifecycle.sql`, taking the next free number at implementation time.
   - `ALTER TABLE users ADD COLUMN status_reason TEXT NOT NULL DEFAULT ''`.
   - `ADD COLUMN status_until INTEGER`, in Unix ms; null means no end.
   - `ADD COLUMN anonymized INTEGER NOT NULL DEFAULT 0 CHECK (anonymized IN (0, 1))`.
   - Backfill `anonymized = 1` for any handle `GLOB 'deleted-user-*'`. None can exist, but the backfill makes the column and the prefix agree by construction.
   - Trigger `users_anonymized_is_final`, `BEFORE UPDATE ON users WHEN OLD.anonymized = 1 AND (NEW.anonymized <> 1 OR NEW.handle <> OLD.handle OR NEW.display_name <> '' OR NEW.avatar_blob IS NOT NULL OR NEW.bio_source <> '' OR NEW.status <> OLD.status)`, raising `'an anonymized account cannot change'`. Irreversible then holds for every write path, the CLI included.
   - Trigger `users_anonymized_handle`, `BEFORE UPDATE OF anonymized`, requiring `NEW.handle = 'deleted-user-' || NEW.id` when `NEW.anonymized = 1`.
   - Document the columns in docs/04 "users". The migration test in `internal/store/db/moderation_schema_test.go` or `identity_schema_test.go` drives both triggers.
2. **Queries** (`internal/store/db/queries/identity.sql`; regenerate dbq).
   - `SetUserStatus :exec` sets `status`, `status_reason`, `status_until` and `updated_at`.
   - `AnonymizeUser :exec` sets `handle = 'deleted-user-' || id`, `display_name = ''`, `avatar_blob = NULL`, `bio_source = ''`, `anonymized = 1`, `status = 'deactivated'`, `status_reason = ''` and `status_until = NULL`. Anonymized accounts become `deactivated`, so they drop out of `UsersByHandlePrefix` (`status = 'active'`) and are served as a deactivated profile is. A fifth status value was the alternative. It lost because it needs a rebuild of `users` to change the CHECK, and a fifth case in every status rule of docs/05 and docs/07.
   - `DeleteUserIdentities :exec`.
   - `CountActiveAdmins :one`.
   - `IdentityHolder` adds `u.status_reason`, `u.status_until` and `u.anonymized`.
3. **One transaction per transition, in `perm`** (`internal/perm/mutate.go`). Add `ChangeStatus(ctx, userID int64, c StatusChange) error` and `Anonymize(ctx, userID, actor int64) error`. `StatusChange{Actor int64; To string; Reason string; Until int64; Action string}`. Each runs in one `e.write`:
   - `GetUser`. An anonymized user is refused with `ErrAnonymized`.
   - Check the transition against a table: `active → deactivated`, `active|deactivated → suspended`, `suspended → active` (unsuspend), and `deactivated → active` (reactivate, the returning rule above). Anything else is `ErrBadTransition`.
   - For an admin leaving `active`, require `CountActiveAdmins > 1`, else `ErrLastAdmin`.
   - `SetUserStatus` or `AnonymizeUser`.
   - `DeleteUserSessions` in the same transaction.
   - For anonymize, also `DeleteUserIdentities`. It deletes sessions first, because `sessions.identity_id` references identities. If TKT-01M29CKRDQ's `applications` table exists, it sets `applications.identity_id = NULL` for those identities, and says so in its summary.
   - For anonymize, when TKT-01M3NRP3W9's handle-reservation table exists, it reserves the old handle for 90 days with no redirect target and no user id. If this ticket lands first, TKT-01M3NRP3W9 adds that write to `Anonymize` and says so in its summary.
   - `AppendAudit` with the action: `user.deactivated`, `user.suspended` (`{reason, until}`), `user.unsuspended`, `user.reactivated`, or `user.anonymized` (`{}`, with no handle).

   Then it invalidates the cache. `SetUserStatus`, `AnonymizeUser` and `DeleteUserIdentities` join `decisionWrites` in `internal/perm/layering_test.go`, so no other package can change a status without the audit row. A status write in a new `internal/service/account` transaction, followed by `perm.Invalidate`, was the alternative. It lost because it would put `SetUserStatus` outside the layering test's reach, the hole that `decisionWrites` exists to close. `DeleteUserSessions` is a DELETE, which the session chokepoint allows anywhere. Role is not changed by any transition, so no promotion or demotion row is written.

   The last-admin guard covers demotion too. `perm.SetRole` refuses to move the last active admin off `admin`, returning `ErrLastAdmin`, and checks `CountActiveAdmins` in its own transaction. The demote action, and the audit row it writes, belong to the admin users screen (TKT-01M29CXK1X).
4. **Authority and input** (`internal/service/account/lifecycle.go`, new). `Service{DB, Perm}` with:
   - `Deactivate(ctx, v)`: self, member.
   - `Suspend(ctx, v, handle, reason string, until *time.Time)`: the actor must be staff and be a moderator suspending a member, or an admin suspending anyone but themselves (settled above). The reason is 1–500 characters with no control characters. `until`, when given, is in the future.
   - `Unsuspend(ctx, v, handle)`: the same authority.
   - `Anonymize(ctx, v, handle)`: admin, not self.

   Refusals are `forbidden`, `not-found` (the target handle, looked up as `member.Account` does), `conflict` (bad transition, last admin, anonymized), and `validation-failed`.
5. **Routes** (`internal/web/api/account.go`, recorded in docs/07). Each answers 204 `no-store`.
   - `POST /api/v1/settings/deactivate`. It also clears the session cookie.
   - `POST /api/v1/admin/users/{handle}/suspend` `{reason, until?}`.
   - `POST /api/v1/admin/users/{handle}/unsuspend`.
   - `POST /api/v1/admin/users/{handle}/anonymize`.

   The admin routes are role-gated as docs/07 "Moderation and admin" requires. They are the first under `/api/v1/admin/`, so this ticket adds the prefix's role gate as `member.RequireStaff(v, what)`. If TKT-01M29CKRG9 has landed, the pre-user allowlist leaves them closed.
6. **The sign-in page** (`internal/auth/session/issue.go`, `internal/service/identity/login.go`, `internal/web/auth/page.go`). `NotActiveError` gains `Reason` and `Until`, read by `holder`. `identity.Result` carries them. `pageUnavailable` becomes a template:
   - Suspended: "This account is suspended. Reason: … Until: <date, UTC>", or "No end date is set."
   - Deactivated: the reactivate offer (settled above).
   - Pending: unchanged.

   `html/template` escapes the reason. Reactivation adds `POST /auth/reactivate` in `internal/web/auth/reactivate.go`, which calls `identity.Service.Reactivate`. That calls `s.Sessions.IssueUserWith` with a create func that runs `perm`'s transition inside the same transaction. That needs `ChangeStatusIn(ctx, q, …)`, the transaction-scoped form, and the cache invalidation after commit is the caller's. `issuerCallers` in `chokepoint_test.go` (TKT-01M29CKRG9) gains `identity/reactivate.go:Reactivate`.
7. **Attribution from the column.** `member.Author(u dbq.User)` becomes the one constructor of `dto.Author` from a users row, and it reads `u.Anonymized`:
   - Replace the inline builder in `internal/service/threads/view.go` `users()`.
   - Replace the one in `list.go` `listDetails`, for the last poster.
   - Replace the one in `userposts.go`, where the bio check at line 53 moves to the column.
   - `domain.Anonymized(handle)` stays only as registration's reserved-prefix rule. Its doc comment says so.

   Every later surface (notifications, search) must draw authors through `member.Author`. docs/07's "until the users.anonymized flag of 05 exists" clause is removed.
8. **Docs.** docs/05 "Account lifecycle" gains four things: the reactivation button (and "Login flow" is reconciled with it); the authority rule and the last-admin guard; the 90-day reservation of an anonymized handle, with no redirect; and the statement that anonymization does not reach `audit_log`, so rows written earlier, rename rows with old handles included, remain. docs/08 "Primitives" gets the authority rule. docs/07 gets the routes.
9. **Response cache.** `authorsDigest` (`internal/service/threads/cached.go`) digests the `dto.Author` values as served, so an anonymization changes it. The test pins this.

### Tests

- `internal/perm` `TestChangeStatusIsOneTransaction`. For each transition, an injected `BEFORE INSERT ON audit_log` trigger makes it fail, and afterwards the status, the reason, `until`, the sessions and (for anonymize) the identities are exactly as before.
- `TestEachTransitionWritesItsAuditRow`: action, actor, target, and details. For anonymize, the details hold no handle. No `user.promoted` row is written or touched.
- `TestLastAdminCannotLeave`, which covers suspend, deactivate, anonymize and `SetRole` to member or moderator, and `TestBadTransitionsAreRefused`, which covers anonymized → anything and suspended → deactivated.
- `internal/perm` `TestMutationsGoThroughPerm` with the new `decisionWrites`.
- `internal/web/auth` `TestEachTransitionSignsOutAtOnce`, with `web.New` and `session.Middleware`. After deactivate, suspend and anonymize, the old cookie's next `/api/v1/me` is anonymous and a member endpoint is `unauthenticated`.
- `TestNoTransitionLetsTheUserBackIn`, the chokepoint test for this ticket. After suspend and after deactivate, signing in through each linked identity gets the unavailable page and writes no session row. After anonymize, the identities are gone, so the same subject takes the registration path and gets a new user id, never the anonymized one. `IssueUser` for the old identity id is `ErrNoIdentity`. `TestOneDoorIntoSessions` passes, with `insertCallers` unchanged.
- `TestReactivation`. A deactivated user reactivates and gets a session through `IssueUserWith`. A suspended, pending or anonymized user posting the same form with a forged or expired cookie is refused and writes nothing.
- `TestTheSuspendedPageShowsReasonAndEnd`, with a reason of `<script>` that must come out escaped, both with an end date and without.
- `internal/store/db` `TestAnonymizedIsFinal`. The trigger refuses a direct UPDATE of the handle, the display name, `anonymized` or the status on an anonymized row.
- `internal/web/api` `TestAnonymizeLeavesTheLogsAlone`. The SHA-256 of every thread log file is the same before and after.
- `TestAnonymizedAttributionEverywhere`. After anonymizing Alice, each of these shows `deleted-user-<id>` with no display name or avatar: the thread page's post header, the thread list's `last_post`, her profile and history, Bob's post mentioning `@alice` (re-rendered), and Bob's quote of her post, which shows `Post #N` and no name. `GET /users?prefix=del` is empty.
- `internal/web/api` `TestResponseCacheAfterAnonymize`, following `TestResponseCacheAfterPromotion`. A page cached with Alice's name is not served after the anonymization.
- `internal/service/account` `TestSuspendAuthority`, one table test over the authority rule: moderator on member (allowed), moderator on moderator or admin (forbidden), admin on anyone (allowed), self (forbidden), and the last active admin (conflict).

### Out of scope

- The moderator UI for suspension, expiry of a suspension's end date without manual action, and hiding a suspended user's posts: TKT-01M29CP2QK.
- Revoking an agent's credential on suspension or deactivation: TKT-01M29P9RSW (its AC4). This ticket's transitions are where it hooks in.
- Renames, and the reservation table: TKT-01M3NRP3W9.
- The admin users screen: TKT-01M29CXK1X.
- Erasing post bodies: TKT-01M29CXJZ9.

## Notes

**agent:claude-code/phase2-groom** at 2026-09-29T05:06:41Z

Phase 2 grooming (TKT-01M3M9RR26), 2026-09-29, left four open decisions. The plan's "### Open decisions" section has the full options. Each changes what a member sees or can do, so each belongs to a person (docs/12).

1. How a deactivated member returns. docs/05 contradicts itself here. "Account lifecycle" says they can return through any linked identity. "Login flow" says a deactivated account gets "account unavailable" and no session. The options are A, reactivate silently at sign-in; B, a "Reactivate" button on the unavailable page, behind a sealed cookie and through IssueUserWith; and C, reactivation by an admin only. Recommended: B.
2. Who may change whose status. A: any moderator, anyone but themselves. B: strictly lower roles only. C: moderators suspend members, and admins suspend anyone but themselves. Recommended: C. The plan adds a last-active-admin guard under every option.
3. An anonymized account's old handle. A: free at once. B: reserved for 90 days, with no redirect and no link to the user. C: reserved forever. Recommended: B.
4. Audit rows that name the person. audit_log is append-only, and renames record the old handle, so anonymization cannot erase them. A: accept this and document it. B: keep handles in a separate table that can be erased. C: extend the offline redaction tool to rewrite audit rows. Recommended: A.

Build the parts no decision governs first: the migration, the perm transitions, the suspension page, and attribution from the column. Then block on the rest if they are still open.

**agent:claude-code/phase2-groom** at 2026-09-29T05:38:52Z

Maintainer decisions 2026-09-29 settle the four open decisions in the note above. They are recorded under Settled in grooming: (1) a deactivated member reactivates with a button on the 'account unavailable' page; (2) moderators suspend members, admins suspend anyone except themselves, and the last active admin cannot be suspended, deactivated, anonymized or demoted; (3) an anonymized account's old handle is reserved for 90 days with no redirect; (4) audit rows keep old handles after anonymization, and docs/05 says so. The plan adds the demotion guard to perm.SetRole, adds the reservation write to Anonymize (TKT-01M3NRP3W9 owns the table), and names a docs/05 step. The criteria were updated to match.
