---
schema: 3
id: TKT-01M29CKRFSDCGKH605X8Y4RT3C
title: "Implement the account lifecycle: deactivate, suspend, anonymize"
type: task
status: done
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
  - ref: pr:157
    path: null
claim: null
archive: null
created_at: 2026-09-11T23:24:59Z
updated_at: 2026-09-29T18:47:57Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase2-run-ckrfs
  name: ""
extensions: {}
---

## Description

Deactivate by self: sessions revoked, identities kept, posts kept, and the user can return through any linked identity. Suspend by a moderator: the same plus a reason shown at login and a duration. Anonymize by an admin: handle becomes `deleted-user-<id>`, display name and avatar cleared, identities removed, and attribution suppressed at render time by a flag.

Anonymize does not edit post bodies, because they are in append-only logs. Erasing body text is the offline redaction tool, and the two must not be confused when somebody arrives with a legal request.

## Acceptance criteria

- [x] Deactivate, suspend and anonymize each delete the user's sessions in the transaction that changes the status, and the old cookie's next request is anonymous (TestEachTransitionSignsOutAtOnce)
- [x] Each transition writes its audit_log row (user.deactivated, user.suspended with reason and until, user.unsuspended, user.anonymized with no handle) in the same transaction as the status change; with an injected audit failure the status, sessions and identities are unchanged
- [x] Signing in as a suspended user answers 403 with the reason, HTML-escaped, and the end date or 'No end date is set', and issues no session
- [x] Anonymize sets users.anonymized, renames to deleted-user-<id>, clears display name, avatar and bio, and deletes identities; a schema trigger refuses any later change to those fields, and thread log files are byte-identical before and after
- [ ] After anonymization, the post header, thread-list last poster, profile, history, mentions and quotes of the account show deleted-user-<id> with no display name or avatar, read from users.anonymized, and a cached page is not served with the old author (TestAnonymizedAttributionEverywhere, TestResponseCacheAfterAnonymize)
- [x] Status writes go only through perm (SetUserStatus and AnonymizeUser are in decisionWrites), and the last active admin cannot be suspended, deactivated, anonymized or demoted (TestLastAdminCannotLeave)
- [x] Moderators can suspend members only, and admins anyone except themselves (TestSuspendAuthority)
- [x] A deactivated member who signs in sees a Reactivate button; using it returns the account to active through session.IssueUserWith and writes user.reactivated, and a suspended, pending or anonymized account cannot use it (TestReactivation)
- [ ] After anonymization the old handle is reserved for 90 days with no redirect (once TKT-01M3NRP3W9's reservation table exists), and docs/05 says anonymization leaves earlier audit rows, old handles included, in place
- [x] After suspend or deactivate, signing in through any linked identity writes no session row; after anonymize the identities are gone and the old identity id is ErrNoIdentity; the chokepoint's issuerCallers gains only identity/reactivate.go:Reactivate

## Implementation plan

Groomed 2026-09-29 for Phase 2 (TKT-01M3M9RR26) against main at 7af2c5a; revised at implementation against 9d5ef44.

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

### Approach (revised at implementation, 2026-09-29, against main at 9d5ef44)

The groomed approach stands; what changed on reading the code is below.

1. **Migration `0012_account_lifecycle.sql`** (0012 was free; the /apply lane, TKT-01M29CP2KZ, adds no migration). `users` gains `status_reason TEXT NOT NULL DEFAULT ''`, `status_until INTEGER` (Unix ms, null = no end) and `anonymized INTEGER NOT NULL DEFAULT 0 CHECK (anonymized IN (0, 1))`. The backfill sets `anonymized = 1` and clears the name, avatar, bio and status of any `deleted-user*` handle (none can exist). Three triggers: `users_anonymized_is_final` (as groomed), `users_anonymized_handle` (the flag goes on only with `deleted-user-<id>`), and `users_insert_anonymized` (no row is inserted anonymized). docs/04 "users" records the columns, and "audit_log" the actions.
2. **Queries** (`identity.sql`): `SetUserStatus`, `AnonymizeUser` (also sets `status = 'deactivated'` and clears the reason and end), `DeleteUserIdentities`, and `DeleteIdentitySessions`, which removes a pre-user session still bound to one of the account's identities from before approval: its `identity_id` would otherwise block the identity delete. `IdentityHolder` adds `status_reason`, `status_until` and `updated_at`. `CountActiveAdmins` already existed (TKT-01M29CXK1X).
3. **`internal/perm/lifecycle.go`**: `ChangeStatus(ctx, userID, StatusChange)`, `ChangeStatusIn(ctx, q, userID, StatusChange)` for reactivation, and `Anonymize(ctx, userID, by, allow)`. `StatusChange{By, Action, Reason, Until, Allow}`. The transition table is keyed by action. **Change from the groomed plan: the authority rule runs inside the transaction** as `Allow func(target dbq.User) error`, asked about the row the change reads, so a promotion landing between the service's handle lookup and the write cannot slip past "moderators suspend members only". Leaving `active` deletes the user's sessions and the identity-bound ones. `ErrLastAdmin` (now worded for all four) is checked in the transaction; `ChangeRoleThen` already refused demoting the last active admin. `ErrBadTransition` and `ErrAnonymized` map to `conflict`. `SetUserStatus`, `AnonymizeUser` and `DeleteUserIdentities` joined `decisionWrites`. An application naming a deleted identity keeps its row: `applications.identity_id` is `ON DELETE SET NULL`.
4. **`internal/service/account`**: `Deactivate`, `Suspend`, `Unsuspend`, `Anonymize`, with the reason rule (1–500 characters trimmed, no control characters, valid UTF-8) and a future `until`. `mayActOn` is the settled authority rule.
5. **Routes** (`internal/web/api/account.go`): `POST /api/v1/settings/deactivate` (clears the session cookie), and through `adminRoute`, `POST /api/v1/admin/users/{handle}/suspend` and `/unsuspend` (staff gate) and `/anonymize` (admin gate). Each answers 204 `no-store`. The service is built inside `api.build` like the groups service, so `api.Deps` and `serve.go` are unchanged. The admin prefix's gates already existed (TKT-01M29CXK1X).
6. **Sign-in** (`internal/web/auth/reactivate.go`, `page.go`): `NotActiveError` and `identity.Result` carry the reason, the end and the row's `updated_at` as `Stamp`. A suspended account gets a template page with the escaped reason and "Until: <date> UTC" or "No end date is set." A deactivated account gets the "Reactivate my account" form and a sealed, five-minute `__Host-ketju_reactivate` cookie holding the claims, the return path and the stamp (the registration cookie's shape and shedding, `sealPending`). `POST /auth/reactivate` clears the cookie, then `identity.Service.Reactivate` runs `perm.ChangeStatusIn` inside `IssueUserWith`'s create func and then `settle` (bootstrap and group_map from the sealed claims, as registration does). **Addition: the cookie is bound to `users.updated_at`**, checked in the transaction, so a copy cannot undo a deactivation made after it. `issuerCallers` gains `internal/service/identity/reactivate.go:Reactivate IssueUserWith`, and nothing else changed in the chokepoint lists.
7. **Attribution**: `member.Author` reads `users.anonymized` through `member.Anonymized(u)`, and the thread page, thread list, forum tree and profile bio moved onto it. The admin users view reads the column too. `domain.Anonymized(handle)` is removed; the prefix stays as registration's reserved-prefix rule only.
8. **Docs**: docs/04, docs/05 ("Login flow" reconciled with the button, "Sessions and CSRF", "Account lifecycle"), docs/07 (the four routes, the profile clause).

### Mentions of an anonymized account

`threads.Service.handle` resolves a mention by the typed text at every render; it does not resolve by user id. After anonymization `@alice` no longer resolves, so it renders as the text typed, linking nobody. The render cache's handle check and the response cache see this at once. Showing it as `@deleted-user-<id>` would need id-pinned mention resolution, which TKT-01M3NRP3W9 builds; under its rule, a `handle_releases` row naming no user leaves an anonymized account's old mentions as plain text as well, which matches the 2026-09-29 decision that nothing ties the old handle to `deleted-user-<id>`.

### Tests, as built

- `internal/store/db`: `TestAnonymizedIsFinal`, `TestAnonymizedNeedsThePlaceholder`, `TestSetUserStatusWritesTheReason`.
- `internal/perm`: `TestChangeStatusIsOneTransaction` (injected audit failure, every transition), `TestEachTransitionWritesItsAuditRow`, `TestUnsuspendClearsTheReason`, `TestLastAdminCannotLeave` (suspend, deactivate, anonymize, demote to member and to moderator), `TestBadTransitionsAreRefused`, `TestAllowSeesTheAccountAsChanged`, `TestAnonymizeKeepsTheApplication`, `TestChangeStatusInvalidates`, and `TestMutationsGoThroughPerm` with the new `decisionWrites`.
- `internal/service/account`: `TestSuspendAuthority`, `TestSuspendChecksWhatItIsSent`, `TestDeactivateAndAnonymizeAuthority`.
- `internal/web/api`: `TestEachTransitionSignsOutAtOnce` (through the real session middleware in the admin world), `TestLifecycleRefusals`, `TestSuspendThroughTheAPI`, `TestAnonymizeLeavesTheLogsAlone`, `TestAnonymizedAttributionEverywhere`, `TestResponseCacheAfterAnonymize`, `TestEverySurfaceReadsTheColumn`.
- `internal/web/auth`: `TestTheSuspendedPageShowsReasonAndEnd`, `TestReactivation`, `TestNoTransitionLetsTheUserBackIn`; `TestOneDoorIntoSessions` with the one new issuer caller.

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

**agent:claude-code/phase2-run-ckrfs** at 2026-09-29T18:44:40Z

Implementation notes, 2026-09-29 (PR to follow on this branch).

Two criteria stay unticked. What stopped each:

- **AC5 (attribution everywhere).** Everything except the mention clause holds and is tested: post headers, the quoted post's header (quotes attribute by ordinal only), the thread list's author and last poster, the forum tree's last post, the profile, the history, and the response cache (`TestAnonymizedAttributionEverywhere`, `TestResponseCacheAfterAnonymize`, `TestEverySurfaceReadsTheColumn`). The mention clause does not hold as written. A mention resolves by the typed handle at every render, not by user id, so after anonymization Bob's `@alice` renders as the plain text he typed, linking nobody. Showing `@deleted-user-<id>` instead would need id-pinned resolution (TKT-01M3NRP3W9, Implement handle changes with cooldown, reservation, and redirect). It would also show the tie between the old handle and the placeholder that the 2026-09-29 no-redirect decision avoids. This is a question of what members see, so a person decides it. Filed as draft TKT-01M3Q7Q1T4 (Decide how a mention of an anonymized account renders), which recommends keeping plain text and rewording this clause.
- **AC9 (90-day reservation).** The docs/05 half holds: "Account lifecycle" says anonymization leaves earlier audit rows, old handles included, in place. The reservation does not exist yet, because the `handle_releases` table is TKT-01M3NRP3W9's, and that ticket depends on this one. Its plan (step 5) and its criterion `TestAnonymizeReleasesWithoutRedirect` add the write to `perm.Anonymize`'s transaction. Until it lands, an anonymized account's old handle is free at once.

Decisions made while building (internals only, per docs/12 "Gaps"):
- The authority rule runs inside perm's transaction as `StatusChange.Allow`, against the row the change reads. The alternative was checking in the service after its own read, which a concurrent promotion could slip past. That alternative lost.
- The reactivation cookie is bound to `users.updated_at` as the sign-in found it. Without that, a copy replayed within its five minutes could undo a deactivation made after it. Clearing the cookie in the browser alone was the other option, and it lost for that reason.
- Leaving `active` also deletes a pre-user session still bound to one of the account's identities from before approval. It no longer resolves, but its `identity_id` would block deleting the identity on anonymization.
- Reactivation does not rewrite the identity's `raw_claims_json` or `last_login_at`. `RecordIdentityLogin` would null the stored claims, since the sealed cookie does not carry them.
- `domain.Anonymized(handle)` is removed. The `deleted-user` prefix stays as registration's reserved-prefix rule only.

## Summary

The account lifecycle is built. Every status change goes through `internal/perm` (`lifecycle.go`: `ChangeStatus`, `ChangeStatusIn`, `Anonymize`). Each one commits the status, its `audit_log` row and the deletion of the account's sessions in one transaction, or nothing. The authority rule (`internal/service/account`) is asked about the row inside that transaction. The last active admin cannot leave by any of the four routes.

Migration 0012 adds `users.status_reason`, `status_until` and `anonymized`, with triggers that make anonymization final. Routes: `POST /api/v1/settings/deactivate`, and `POST /api/v1/admin/users/{handle}/suspend`, `/unsuspend` (staff) and `/anonymize` (admin).

At sign-in, a suspended account sees the escaped reason and the end date. A deactivated account gets the Reactivate button. The button's sealed cookie is bound to the row's `updated_at`, and the reactivation runs inside `session.IssueUserWith`, the one new issuer caller.

Every author block reads `users.anonymized` through `member.Author`, so a cached page with the old author is not served. An application naming a removed identity keeps its row with a null `identity_id`, through the column's foreign key. docs/04, 05 and 07 record the columns, the audit actions, the reconciled login flow and the routes.

Unticked: AC5's mention clause, where a mention renders as the typed text rather than as `deleted-user-<id>`. That decision is filed as draft TKT-01M3Q7Q1T4 (Decide how a mention of an anonymized account renders). Also AC9's 90-day reservation: TKT-01M3NRP3W9 (Implement handle changes with cooldown, reservation, and redirect) owns the `handle_releases` table and adds that write to `perm.Anonymize` when it lands.
