---
schema: 3
id: TKT-01M29CP2MGC6FD35PJHR2Y2WNA
title: Implement moderator application review
type: task
status: in-progress
status_reason: null
priority: high
due_on: null
labels:
  - area:identity
  - area:moderation
  - effort:L
assignees: []
milestone: phase-2
parent: TKT-01M29BKBM38VTYY4Y91018GTH6
origin: null
dependencies:
  - TKT-01M29CKRDQHB528ZW1FFBN4SA1
  - TKT-01M29C7ZZR3FJMZ7P7VA0CWNJ2
  - TKT-01M29CP2KZPR0880MGFV3CPXR4
  - TKT-01M29CP2KEEB60VEVBPY9H2GBD
blocks_on: none
references:
  - ref: design:approval-mode
    path: docs/05-identity-and-accounts.md
claim:
  actor: agent:claude-code/phase2-run-cp2mg
  branch: run/cp2mg-application-review
  worktree: /home/sothr/workspace/git.local.sothr.com/terva-sh/ketju/.claude/worktrees/agent-a1ffd451064ec2f46
  commit: 31e681216c0cacb24f5467a824631f79d61108c5
  session: null
  claimed_at: 2026-09-29T19:28:45Z
  expires_at: null
archive: null
created_at: 2026-09-11T23:26:15Z
updated_at: 2026-09-29T19:28:47Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase2-run-cp2mg
  name: ""
extensions: {}
---

## Description

`/admin/applications` showing the answers, the identity's provider, email, and verified flag, prior applications from the same identity, and notes from other moderators. Actions are approve, deny with a note, and deny with "block this subject".

Approve creates the users row with status active, confirms or chooses the handle, links the identity, notifies the applicant, and writes audit_log. Deny with block keeps the identity with user_id NULL and a blocked marker so re-application is refused.

Approval is the only path from a pre-user session to a member session, and it goes through the single session-creation function like everything else.

## Acceptance criteria

- [ ] Approve creates the user (active, member, human), links the identity, marks the application approved with user_id, and writes its audit row in one transaction or none; a test failing the audit insert leaves none of them. TKT-01M29CP2N0 adds the applicant's notification inside the same transaction
- [ ] Approval issues no session: session rows are unchanged, a pre-user session held across approval still resolves as a non-member, the member session comes from the next sign-in through session.IssueUser, and TestOneDoorIntoSessions passes with its caller lists unchanged
- [ ] An approved user who is suspended before signing in is refused by IssueUser at sign-in, and approval never sets a role other than member or writes a user.promoted row
- [ ] Deny with block sets the identity's block marker with user_id NULL, and a later GET /apply/form or POST /apply from that identity answers 403
- [ ] Every decision writes one audit_log row naming the deciding moderator and carrying the note, tested for approve, deny, and deny with block; a denial without a note is refused
- [ ] Two concurrent decisions on one application produce one decision, one audit row, and 409 conflict for the second, tested for approve against deny and against withdraw
- [ ] Review routes are staff-only (anonymous 401; member, pre-user and suspended moderator 403) and no-store, and an application renders against the form version it answered after a newer version is active
- [ ] Approval creates and links the user before the guarded decision UPDATE, which sets status approved and user_id in one statement; zero rows there rolls back the user and the link
- [ ] A moderator or admin can unblock a denied applicant's identity: blocked_at and blocked_by are cleared, the application stays denied, and one identity.unblocked audit row names the actor; a second unblock is 409 and a member is 403
- [ ] The decision note never reaches the applicant: no route open to a pre-user session carries it, tested with a distinctive note
- [ ] Deny with block requires the block UPDATE to affect exactly one row: when the identity was removed or has a user it answers 409 and the denial rolls back, while a plain deny of that application still succeeds

## Implementation plan

Groomed 2026-09-29 for the Phase 2 autonomous run (TKT-01M3M9RR26), against main at 7af2c5a. Amended 2026-09-29 by TKT-01M29CP2KZ's agent to carry the maintainer's decision on TKT-01M3Q2V25E (the Settled item it adds, "Approval issues no session", and `TestApproveIssuesNoSession`); nothing else changed.

What exists there:
- Sessions are created only through `session.insert` in `internal/auth/session/issue.go`, whose only callers are `IssueUserWith` (which also serves `IssueUser`) and `IssuePreUser`. `chokepoint_test.go`'s `doorCallers` and `insertCallers` hold those lists.
- `perm.Evaluator.Promote` writes a role change and its `user.promoted` audit row in one transaction (`internal/perm/mutate.go`).
- `identity.CheckHandle` and `identity.Prefill` live in `internal/service/identity/handle.go`.

### Settled in grooming

- **The review screen shows** the answers, the identity's provider, email and verified flag, prior applications from the same identity, and notes from other moderators (docs/05 step 5).
- **The actions are approve, deny with a note, and deny with "block this subject"** (docs/05 step 5). "Request changes" is a later phase.
- **Approve chooses or confirms a handle, creates the `users` row with status active, links the identity, and notifies the applicant** (docs/05 step 5). **Approval is recorded in `audit_log`** (docs/05 step 6, docs/08 "Primitives": moderator, `applications` plus `audit_log`, "deny → re-apply").
- **A block keeps the identity with `user_id = NULL`** and a marker, and re-application is refused (docs/05 step 5).
- **Who may review is the moderator role** (docs/08 "Primitives"). Each admin route is gated on the role docs/08 names (docs/07 "Moderation and admin"). An admin passes every check (docs/05 "Roles and groups"). An inactive viewer has no role (`session.Viewer`, docs/04 "permissions").
- **Approval is the only path from a pre-user session to a member session, and it goes through the single session-creation function** (this ticket's description; docs/09's Gitea row; TKT-01M29C7ZZR). A pre-user session is never upgraded in place (TKT-01M29CKRG9's criterion). The chokepoint scan refuses any UPDATE of `sessions.kind` or `user_id`.
- **The handle passes registration's checks**: valid, not reserved, and free in any case. A handle released by a rename stays reserved for 90 days (docs/05 "Changing a handle", decided 2026-09-28), so the check honours that reservation once its implementation exists.
- **A pending account is never created.** Approval writes `status = 'active'` directly (docs/05 step 1), so the profile rule for pending accounts (docs/05 "Account lifecycle", decided 2026-09-28) meets no applicant.
- **Maintainer decision 2026-09-29: a denied or blocked applicant sees the status only, never the moderator's note.** The status page says "not approved". A blocked applicant also gets no apply control, and the form answers 403 with a fixed message. `decision_note` never appears in `dto.ApplicationStatus` or on any route a pre-user session can reach. A showing of the note verbatim lost, because moderators write it for colleagues and showing it would publish what they did not mean to publish. A separate message-to-the-applicant field remains a possible follow-up, and nothing here builds it. This was the recommendation in grooming; it is recorded in a note on this ticket.
- **Maintainer decision 2026-09-29 (TKT-01M3Q2V25E): an approved applicant's pre-user session retires, and they learn the decision by signing in.** Once approval links the identity to a user, `LookupSession` no longer resolves the applicant's pre-user session and the session middleware clears its cookie (TKT-01M29CKRG9), so the status page never says "approved" to it. The applicant sees the signed-out shell with a hint like "Applied? Sign in to see your decision"; signing in gives them a member session through `session.IssueUser`, where the approval notification waits (TKT-01M29CP2N0). A retired pre-user session that still answers the status route lost, because it would loosen TKT-01M29CKRG9's session guarantee in three places for a page the member session already provides.
- **Maintainer decision 2026-09-29: moderators and admins can unblock an identity, with an audit row.** docs/08 asks that tools be reversible, and whoever can block can undo a mistaken block. Having no unblock in v1, and allowing admins only, both lost. Unblocking clears the marker and nothing else: it does not reopen the denied application, and the applicant may apply again under TKT-01M29CP2KZ's 24-hour rule.

Engineering decisions taken here, with the alternative that lost:
- **Approval issues no session, and it retires the applicant's pre-user sessions** (superseded 2026-09-29 by the maintainer decision above; the groomed version kept the pre-user session showing "approved", which merged code rules out). `LookupSession` stops resolving them the moment the identity is linked, whatever approval does, and approval also deletes the rows with `q.DeletePreUserSessions` in its transaction, which TKT-01M29CKRG9 left for it. The member session comes from the applicant's next sign-in: `identity.Service.Login` finds the identity with a user and calls `session.IssueUser`, which checks the status. A "continue" endpoint minting a member session from the pre-user cookie lost: it would add a caller of `IssueUser` keyed on a cookie issued before the account existed. Re-authenticating with the IdP is the better proof, and the chokepoint lists stay untouched.
- **Approval sets role `member` and nothing else.** The request body has no role field, and an unknown field is refused. A later role change goes through `perm` (`Promote` or a sibling). A bootstrap-named applicant is promoted at their first sign-in by the existing `settle → ApplyBootstrap`, so approval never touches roles.
- **The display name is the IdP `name` claim, or the handle when it is empty**, as open registration does (`identity.Register`). Reading the form's `name` answer lost, because it couples code to a key an admin can rename.
- **The suggested handle is `identity.Prefill(preferred_username, email)`** over the identity's stored claims, the same suggestion open registration makes.
- **The decision is guarded in SQL:** `UPDATE applications … WHERE id = ? AND status = 'pending'` inside the `BEGIN IMMEDIATE` write. Zero rows is `ErrAlreadyDecided`, which maps to conflict, and the whole transaction rolls back, including a user created earlier in it. A version column lost, because status already is the version.
- **An approval sets `status` and `user_id` in the same UPDATE, after the user exists** (review 1344). TKT-01M29CKRDQ's CHECK `(status = 'approved') = (user_id IS NOT NULL)` is evaluated per statement, not at commit. A decision UPDATE that ran before `CreateUser` would fail on every approval.
- **A denial requires a note, of 1–2000 characters** ("deny (with note)", docs/05). An approval's note is optional.
- **One audit row per decision**, with target type `application`, the application id as target, and the deciding moderator as actor:
  - `application.approved` with `{user_id, identity_id, handle, note}`;
  - `application.denied` with `{note, blocked}`;
  - `identity.unblocked`, with target type `identity`, the identity id as target, and `{application_id}`.
  A block writes no separate row, because it is part of the denial.

### Approach

1. `internal/store/db/queries/applications.sql`, beside TKT-01M29CP2KZ's queries:
   - `ListApplications`: keyset on (created_at, id) descending with an optional status, 25 to a page.
   - `GetApplication`, and `ApplicationsForIdentity` for the prior applications.
   - `GetPendingApplication`: the application with its identity's user_id and blocked_at, read inside the write transaction.
   - `DecideApplication`: UPDATE … SET status, decided_by, decided_at, decision_note, user_id WHERE id = ? AND status = 'pending', as `:execrows`. `user_id` is a parameter, so an approval sets it in the same statement as the status, and a denial passes NULL.
   - `LinkIdentityToUser`: UPDATE identities SET user_id = ?, updated_at = ? WHERE id = ? AND user_id IS NULL AND blocked_at IS NULL, as `:execrows`.
   - `BlockIdentity`: UPDATE identities SET blocked_at = ?, blocked_by = ? WHERE id = ? AND user_id IS NULL, as `:execrows`.
   - `UnblockIdentity`: UPDATE identities SET blocked_at = NULL, blocked_by = NULL, updated_at = ? WHERE id = ? AND blocked_at IS NOT NULL, as `:execrows`.
   - `InsertApplicationNote` and `ApplicationNotes`, the latter with author handles.
   - Regenerate dbq.
2. `internal/service/applications/review.go`:
   - `staff(viewer)`: anonymous is `ErrNoApplicant` (401). Otherwise the viewer needs `Member()` with role moderator or admin, else `ErrNotStaff` (403).
   - `List(ctx, viewer, status, cursor)`.
   - `Get(ctx, viewer, id) (dto.AdminApplication, error)`: the application; its own form version through `formsvc.Version(form_id, form_version)`, which works whatever the active version is now; the answers; the identity (provider, email, email_verified, blocked), or null once removed; the prior applications; the notes; `suggested_handle`; and the decision (decided_by as an author, decided_at, note).
   - `Approve(ctx, viewer, id, handle, note)`, in one `DB.Write`:
     1. Run `identity.CheckHandle(handle)`. An invalid or reserved handle is a field error `{field: "handle", code: "invalid" | "reserved"}`, 422.
     2. `GetPendingApplication`. A missing row is not-found. A row no longer pending is `ErrAlreadyDecided` (409).
     3. The identity must still exist, have no user, and not be blocked, else `ErrIdentityGone` (409).
     4. `GetUserByHandle` in any case: taken is a field error `{handle, taken}`, 409.
     5. `CreateUser` with status active, role member, kind human, and the display name as settled. A unique violation is `taken`.
     6. `LinkIdentityToUser`, which must affect one row.
     7. `DecideApplication(approved, user_id)`, setting the status and the new user's id in one statement, so the CHECK from TKT-01M29CKRDQ holds when that statement runs. Zero rows is `ErrAlreadyDecided`, and returning it rolls back the user and the link. The write connection serializes transactions, so step 2 already saw the truth; step 7's guard is the one that decides.
     8. `AppendAudit` `application.approved`.
     TKT-01M29CP2N0 adds the applicant's notification inside this transaction. The moderators' `new_application` rows are left alone, and they leave the count once the application is no longer pending.
   - `Deny(ctx, viewer, id, note, block)`, in one `DB.Write`: the note is required; `GetPendingApplication`; `DecideApplication(denied, NULL)`; then, when `block` is set, `BlockIdentity`, which must affect exactly one row. A block with no identity to act on is `ErrIdentityGone` (409), and the whole denial rolls back: either the identity is NULL because it was removed (`applications.identity_id` is ON DELETE SET NULL, TKT-01M29CKRDQ), or it now has a user. The moderator then sees the application still pending, and may deny it without the block (review 1353). Last comes `AppendAudit` `application.denied`, whose `blocked` records only a block that was written. A denial without `block` needs no identity, so an application whose identity was removed can still be denied.
   - `Unblock(ctx, viewer, id)`, in one `DB.Write`, for the same staff as the other actions: it looks up the application's identity; `UnblockIdentity`, where zero rows (not blocked, or the identity is gone) is `ErrNotBlocked` (409); and `AppendAudit` `identity.unblocked`. The application stays denied.
   - `AddNote(ctx, viewer, id, body)`: the body is 1–2000 characters. It writes no audit row, because it decides nothing.
   - Map `ErrAlreadyDecided`, `ErrIdentityGone`, `ErrNotBlocked` and `ErrNotStaff` in `internal/problem/domain.go`. The handle errors reuse the `errors` extension from TKT-01M29CP2KE.
3. `internal/dto`:
   - `AdminApplicationList{applications []AdminApplicationRow, next_cursor}`, where a row is `{id, status, created_at, provider, email, email_verified}`.
   - `AdminApplication{id, status, created_at, form Form, answers map[string]any, identity *ApplicantIdentity, prior []AdminApplicationRow, notes []ApplicationNote, suggested_handle, decision *ApplicationDecision}`.
   - `ApplicationNote{author Author, body, created_at}` and `ApplicationDecision{by *Author, at, note, user *Author}`.
   - Add goldens, the types in `web/client/src/api/dto.ts` and `dto-contract.ts`, and `go generate ./internal/web/spa` with any diff committed.
4. `internal/web/api/applications.go`. Every route answers `no-store` and none goes near `internal/service/respcache`:
   - `GET /api/v1/admin/applications?status=&cursor=`. `status` is one of pending (the default), approved, denied, withdrawn or all.
   - `GET /api/v1/admin/applications/{id}`.
   - `POST /api/v1/admin/applications/{id}/approve` with `{handle, note?}`, returning 200 `AdminApplication`.
   - `POST /api/v1/admin/applications/{id}/deny` with `{note, block}`, returning 200.
   - `POST /api/v1/admin/applications/{id}/notes` with `{body}`, returning 201 `ApplicationNote`.
   - `POST /api/v1/admin/applications/{id}/unblock` with an empty body, returning 200 `AdminApplication` with the identity no longer blocked. It is keyed on the application because that is where the review screen shows the block.
   - Bodies decode with `DisallowUnknownFields`.
5. Docs:
   - docs/07: the admin application routes and their codes.
   - docs/05 step 5: approval issues no session and the applicant signs in again; how the handle is suggested; where the display name comes from; that a denial needs a note, which the applicant never sees; and that staff can lift a block, with an audit row.

### Tests

- `internal/service/applications/review_test.go`:
  - `TestApproveIsOneTransactionOrNone`: an approval that succeeds (which fails outright if the status UPDATE precedes the user, as review 1344 found) creates the user (active, member, human), links the identity, and marks the application approved with user_id, with exactly one audit row naming the moderator and the note. A test trigger that fails the audit insert leaves no user, the identity's user_id NULL, the application pending, and no audit row.
  - `TestApproveIssuesNoSession`: approval adds no `sessions` row and deletes the applicant's pre-user rows. The applicant's pre-user session, issued before approval with `IssuePreUser`, no longer resolves through `session.Middleware`, which answers as anonymous and clears the cookie. `identity.Login` for that identity then returns `SignedIn` with a fresh session through `IssueUser`.
  - `TestApprovedThenSuspendedCannotSignIn`: approve, set status suspended, and `Login` returns `Unavailable` with no session row. This is the chokepoint's status check.
  - `TestApproveNeverSetsARole`: a body with `"role":"admin"` is 400; the created user is a member; no `user.promoted` row exists.
  - `TestApproveChecksTheHandle`: invalid and reserved handles are 422 with `errors[handle]`; a taken handle in another case is 409 with `errors[handle]`; the application stays pending.
  - `TestDenyWithBlockRefusesReapplication`: the identity has blocked_at and blocked_by set and user_id NULL, and TKT-01M29CP2KZ's `Form` and `Submit` for it return 403.
  - `TestDenyRequiresANote`.
  - `TestBlockNeedsAnIdentity`: delete the identity of a pending application, then deny with block. The answer is 409 `ErrIdentityGone`; the application stays pending; no audit row is written; and no identity row carries blocked_at. A plain deny of the same application then succeeds, with `blocked: false` in its audit row.
  - `TestUnblockLiftsTheBlock`: after deny with block, a moderator unblocks. blocked_at and blocked_by are NULL, the application stays denied, and one `identity.unblocked` row names the moderator and the application. TKT-01M29CP2KZ's `Form` then answers under the 24-hour rule instead of 403. A second unblock is 409 and writes no row. A member is 403.
  - `TestTheNoteNeverReachesTheApplicant`: after a deny with a distinctive note, neither `GET /apply/status` nor any other route open to the pre-user session carries the note's text.
  - `TestEveryDecisionIsAudited`: approve, deny, and deny with block each write exactly one row with actor_id set to the moderator and `details.note`.
  - `TestConcurrentDecisionsProduceOne`: approve and deny race on one application 200 times. Each round has one success, one 409, and one audit row. Withdraw against approve behaves the same.
  - `TestOldVersionStillRenders`: an application on version 1, with version 2 active, returns version 1's form and its answers.
- `internal/web/api/applications_test.go`:
  - `TestReviewIsStaffOnly`: anonymous is 401, a member 403, a pre-user 403, a suspended moderator 403, a moderator and an admin 200.
  - `TestPriorApplicationsAndNotes`.
  - `TestReviewResponsesAreNoStore`.
- Chokepoint: `TestOneDoorIntoSessions` passes with `doorCallers` and `insertCallers` unchanged, and this ticket adds no caller of `IssueUser`, `IssueUserWith` or `IssuePreUser`, which a reviewer checks with `grep -rn 'Issue\(User\|PreUser\)' internal --include='*.go' | grep -v _test`.
- The goldens for AdminApplication, AdminApplicationList and ApplicationNote, with the client contract test.

### Out of scope

- The client review screen: TKT-01M3NRX2NN "Build the admin applications review screen" (filed in this grooming).
- The unified queue and its badge: TKT-01M29CRJ4.
- The notifications written inside these transactions: TKT-01M29CP2N0.
- A separate message-to-the-applicant field on deny: not built; the maintainer decided the applicant sees the status only.
- Request changes: a later phase (docs/05 step 5).
- `auto_approve` providers, which skip review: TKT-01M29CKRE9.

## Notes

**agent:claude-code/phase2-groom** at 2026-09-29T05:08:57Z

Two open product decisions were found in grooming (2026-09-29). Both are written into the plan's "Open decision" section. Neither blocks the build, because each has a safe default that the other options only add to.

1. **What a denied or blocked applicant sees.** The options: (a) the status only, with the note kept internal; (b) the decision note shown verbatim; (c) a separate, optional message to the applicant. The recommendation is (a) now and (c) later. Moderators write the note for colleagues, so showing it would publish something they did not mean to publish. The default build is (a).
2. **Whether a block can be lifted.** The options: (a) no unblock in v1; (b) moderators and admins unblock, audited as `identity.unblocked`; (c) admins only. The recommendation is (b), because docs/08 asks for reversible tools and whoever can block should be able to undo a mistaken block. The default build has no unblock. If (b) or (c) is chosen, it adds one endpoint and one audit row to this ticket.

A maintainer's answer goes into this ticket's "Settled in grooming" section and, for (1), into docs/05 step 5.

**agent:claude-code/phase2-groom** at 2026-09-29T05:38:55Z

This note supersedes the 2026-09-29 note that listed two open decisions. The maintainer accepted both recommendations on 2026-09-29, and the plan now records them under "Settled in grooming" as "maintainer decision 2026-09-29":

1. A denied or blocked applicant sees the status only, never the moderator's note. `decision_note` is kept out of `dto.ApplicationStatus` and every route a pre-user session can reach, and `TestTheNoteNeverReachesTheApplicant` checks this.
2. Moderators and admins can unblock an identity, with an audit row. The plan now builds the unblock: `POST /api/v1/admin/applications/{id}/unblock`, backed by `UnblockIdentity` and one `identity.unblocked` audit row. The application stays denied, and the applicant may apply again under TKT-01M29CP2KZ's 24-hour rule. The review screen's control is TKT-01M3NRX2NN's new criterion 7.

The same commit fixes review 1344's first finding here: approval now creates and links the user before the guarded decision UPDATE, which sets `status` and `user_id` in one statement.

**agent:claude-code/phase2-run-cp2kz** at 2026-09-29T18:18:12Z

2026-09-29, superseding the engineering decision "Approval issues no session" as groomed, where the pre-user session "keeps showing the status page, which then says approved with a sign-in link". Maintainer decision on TKT-01M3Q2V25E (Decide what an approved applicant sees once their pre-user session ends): keep the merged behaviour. Once approval links the identity, `LookupSession` stops resolving the applicant's pre-user session and the session middleware clears its cookie (TKT-01M29CKRG9). The applicant sees the signed-out shell with a hint like "Applied? Sign in to see your decision", and signing in gives them a member session where the approval notification waits. Approval still issues no session. It now also deletes the applicant's pre-user rows with `q.DeletePreUserSessions` in its transaction, and `TestApproveIssuesNoSession` checks that the old session no longer resolves. The plan carries this as a new Settled item. Every other Settled item is unchanged. Recorded by TKT-01M29CP2KZ's agent, which carried the decision.
