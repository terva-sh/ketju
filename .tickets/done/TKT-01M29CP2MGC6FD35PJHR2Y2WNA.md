---
schema: 3
id: TKT-01M29CP2MGC6FD35PJHR2Y2WNA
title: Implement moderator application review
type: task
status: done
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
  - ref: pr:159
    path: null
claim: null
archive: null
created_at: 2026-09-11T23:26:15Z
updated_at: 2026-09-29T19:53:49Z
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

- [x] Approve creates the user (active, member, human), links the identity, marks the application approved with user_id, and writes its audit row in one transaction or none; a test failing the audit insert leaves none of them. TKT-01M29CP2N0 adds the applicant's notification inside the same transaction
- [x] Approval issues no session: session rows are unchanged, a pre-user session held across approval still resolves as a non-member, the member session comes from the next sign-in through session.IssueUser, and TestOneDoorIntoSessions passes with its caller lists unchanged
- [x] An approved user who is suspended before signing in is refused by IssueUser at sign-in, and approval never sets a role other than member or writes a user.promoted row
- [x] Deny with block sets the identity's block marker with user_id NULL, and a later GET /apply/form or POST /apply from that identity answers 403
- [x] Every decision writes one audit_log row naming the deciding moderator and carrying the note, tested for approve, deny, and deny with block; a denial without a note is refused
- [x] Two concurrent decisions on one application produce one decision, one audit row, and 409 conflict for the second, tested for approve against deny and against withdraw
- [ ] Review routes are staff-only (anonymous 401; member, pre-user and suspended moderator 403) and no-store, and an application renders against the form version it answered after a newer version is active
- [x] Approval creates and links the user before the guarded decision UPDATE, which sets status approved and user_id in one statement; zero rows there rolls back the user and the link
- [x] A moderator or admin can unblock a denied applicant's identity: blocked_at and blocked_by are cleared, the application stays denied, and one identity.unblocked audit row names the actor; a second unblock is 409 and a member is 403
- [x] The decision note never reaches the applicant: no route open to a pre-user session carries it, tested with a distinctive note
- [x] Deny with block requires the block UPDATE to affect exactly one row: when the identity was removed or has a user it answers 409 and the denial rolls back, while a plain deny of that application still succeeds

## Implementation plan

Groomed 2026-09-29 for the Phase 2 autonomous run (TKT-01M3M9RR26), against main at 7af2c5a. Amended 2026-09-29 by TKT-01M29CP2KZ's agent to carry the maintainer's decision on TKT-01M3Q2V25E. Revised 2026-09-29 at claim, against main at 31e6812, by agent:claude-code/phase2-run-cp2mg. Every Settled item below is carried unchanged.

What exists at 31e6812:
- Sessions are created only through `session.insert`; `chokepoint_test.go` holds `doorCallers`, `insertCallers` and `issuerCallers`. `DeletePreUserSessions` exists in `identity.sql` for this ticket.
- `identity.CheckHandle`, `identity.Prefill` and `identity.HandleFree(ctx, q, h, now)` live in `internal/service/identity/handle.go`. `TestEveryHandleWriteChecksHandleFree` fails any file that calls `CreateUser` without `HandleFree`.
- `member.RequireStaff` is the service-side staff rule; `adminRoute` with `staffGate` is the route-side one. An inactive viewer has no role and is refused as unauthenticated (401) by both, which `TestAdminRoutesAreGated` pins for every admin route.
- `internal/audit.Append` is the only writer of `audit_log`. It has no `identity` target type yet.
- `internal/service/applications` (apply.go) holds the applicant's side; `formsvc` has `ActiveIn`, `Version` and `ToDTO`.
- `internal/problem` imports the domain packages, so a service that imports `problem` cannot be mapped from `problem/domain.go` (an import cycle). `applications` codes its errors where they are raised with `problem.Coded`, and this ticket does the same.
- Field errors (`errors: [{field, code}]`) are carried only on a `validation-failed` body, from a `*forms.Invalid`, with a code from `forms.Codes`, which docs/07's form-codes table must list.

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
- **The handle is checked by `identity.HandleFree` inside the approval's transaction** (added at claim: main now has it, and `TestEveryHandleWriteChecksHandleFree` requires it of every `CreateUser` caller). A handle a user holds in any case, or a release still reserves, is `taken`.
- **Every handle refusal is `validation-failed` (422) with `errors: [{field: "handle", code}]`, the code one of `invalid_handle`, `reserved` or `taken`** (changed at claim from the groomed "taken is 409 with errors[handle]"). `internal/problem` carries field errors only on a validation-failed body, and a 409 without them could not be told apart from the other two conflicts an approval can meet (already decided, identity gone). Allowing `errors` on a conflict body lost: it changes a contract every client reads, pinned by `TestOnlyValidationFailedCarriesErrors`, for one field. The three codes join `forms.Codes` and docs/07's form-codes table, whose test holds them together.
- **The errors are coded where they are raised** (`problem.Coded`), as `apply.go` does, not mapped in `problem/domain.go` as groomed: `problem` imports the domain packages and `applications` imports `problem`, so a table entry would be an import cycle. The staff rule is `member.RequireStaff`, not a new `ErrNotStaff`.
- **A denial requires a note, of 1–2000 characters** ("deny (with note)", docs/05). An approval's note is optional, and at most 2000. A missing or overlong note is validation-failed with `errors: [{field: "note", code: "required" | "too_long"}]`.
- **One audit row per decision**, with target type `application`, the application id as target, and the deciding moderator as actor:
  - `application.approved` with `{user_id, identity_id, handle, note}`;
  - `application.denied` with `{note, blocked}`;
  - `identity.unblocked`, with target type `identity`, the identity id as target, and `{application_id}`. `identity` is a new `internal/audit` target type, listed in docs/04's audit-targets table.
  A block writes no separate row, because it is part of the denial.
- **The pending application is read with the existing `GetApplication`-style row and `ApplicantIdentity`** rather than a joined `GetPendingApplication`: the write transaction reads both anyway, and the guarded UPDATEs decide.

### Approach

1. `internal/store/db/queries/applications.sql`:
   - `ListApplications`: keyset on (created_at, id) descending with an optional status, joined to the identity for provider, email and verified; 25 to a page.
   - `GetApplication`, and `OtherApplicationsForIdentity` for the prior applications (at most 50, newest first).
   - `DecideApplication`: UPDATE … SET status, decided_by, decided_at, decision_note, user_id WHERE id = ? AND status = 'pending', as `:execrows`.
   - `LinkIdentityToUser`: … WHERE id = ? AND user_id IS NULL AND blocked_at IS NULL, as `:execrows`.
   - `BlockIdentity`: … WHERE id = ? AND user_id IS NULL, as `:execrows`.
   - `UnblockIdentity`: … WHERE id = ? AND blocked_at IS NOT NULL, as `:execrows`.
   - `InsertApplicationNote` and `ApplicationNotes`.
   - Regenerate dbq.
2. `internal/service/applications/review.go`: `List`, `Get`, `Approve`, `Deny`, `Unblock`, `AddNote`, each refusing through `member.RequireStaff` first; the writes in one `DB.Write` each, in the order the groomed plan gives (Approve: CheckHandle, application pending, identity usable, HandleFree, CreateUser, LinkIdentityToUser, DecideApplication with the user id, DeletePreUserSessions, audit). `formsvc.VersionIn(ctx, q, formID, n)` is added so `Get` reads the version the application answered in its own transaction.
3. `internal/dto`: `AdminApplicationList`, `AdminApplicationRow`, `AdminApplication`, `ApplicantIdentity`, `ApplicationNote`, `ApplicationDecision`, with goldens; the matching types in `web/client/src/api/dto.ts` and `dto-contract.ts`, types only, because the contract test requires every golden there. `internal/web/spa/dist` is not changed (the client lane, TKT-01M29CP2NG, holds it); a types-only edit leaves the build byte-identical, which is checked before pushing.
4. `internal/web/api/applications.go`, every route through `adminRoute` with `staffGate`, every answer `no-store`, none near `respcache`, bodies decoded with `decodeStrict`:
   - `GET /api/v1/admin/applications?status=&cursor=` (pending by default; approved, denied, withdrawn or all);
   - `GET /api/v1/admin/applications/{id}`;
   - `POST …/{id}/approve` `{handle, note?}` → 200 `AdminApplication`;
   - `POST …/{id}/deny` `{note, block}` → 200 `AdminApplication`;
   - `POST …/{id}/notes` `{body}` → 201 `ApplicationNote`;
   - `POST …/{id}/unblock` → 200 `AdminApplication`.
5. Docs: docs/07 (the routes and their codes; the three handle codes in the form-codes table), docs/05 step 5 (approval issues no session and retires the pre-user sessions; the handle suggestion and checks; the display name; a denial needs a note the applicant never sees; staff can lift a block, audited), docs/04 (the `identity` audit target and the three actions).

### Tests

- `internal/service/applications/review_test.go`:
  - `TestApproveIsOneTransactionOrNone` (success, and a trigger failing the audit insert leaves nothing);
  - `TestApproveIssuesNoSession` (no session row added, the pre-user rows deleted, the old cookie resolves as anonymous and is cleared by `session.Middleware`, then `identity.Login` signs in through `IssueUser`);
  - `TestApprovedThenSuspendedCannotSignIn`;
  - `TestApproveNeverSetsARole` (service side: the created user is a member and no `user.promoted` row; the `"role"` body field is 400 in the API test);
  - `TestApproveChecksTheHandle` (invalid, reserved, taken in another case, and reserved by a release, each 422 with `errors[handle]`, the application still pending);
  - `TestDenyWithBlockRefusesReapplication`; `TestDenyRequiresANote`; `TestBlockNeedsAnIdentity`; `TestUnblockLiftsTheBlock`;
  - `TestEveryDecisionIsAudited`; `TestConcurrentDecisionsProduceOne` (approve against deny, and approve against withdraw, raced); `TestOldVersionStillRenders`.
- `internal/web/api/applications_test.go`: `TestReviewIsStaffOnly` (anonymous 401, member 403, pre-user 403, suspended moderator refused, moderator and admin 200), `TestPriorApplicationsAndNotes`, `TestReviewResponsesAreNoStore`, `TestApproveRefusesARole`, and `TestTheNoteNeverReachesTheApplicant`, which walks every API route as the denied applicant's pre-user session through the real chain and finds the distinctive note in no body.
- Chokepoint: `TestOneDoorIntoSessions` passes with its caller lists unchanged, and this ticket adds no caller of an issuer. `TestEveryHandleWriteChecksHandleFree` passes with review.go as a `CreateUser` caller.
- Audit rows are counted as deltas in the admin test world.

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

**agent:claude-code/phase2-run-cp2mg** at 2026-09-29T19:49:47Z

2026-09-29, at the close of the build (commit 9dbb88e on run/cp2mg-application-review). Three things the criteria and the groomed plan do not say:

1. **Criterion 7 is left unticked.** Everything in it holds except one word. A suspended moderator is refused every review route, but with 401 `unauthenticated`, not 403. That is the site-wide rule in `member.Require`: an inactive account is treated as no member at all, as the thread endpoints treat it, and `TestAdminRoutesAreGated` pins 401 for every admin route (TKT-01M29CKRFS). Answering 403 here alone would make this ticket's routes disagree with every other admin route, so I kept the rule. `TestReviewIsStaffOnly` checks the 401, and `TestOnlyStaffReview` checks it at the service. Anonymous 401, member / pre-user / group-scoped moderator 403, the no-store answers, and the old form version rendering (`TestOldVersionStillRenders`) all hold. If 403 is wanted, the change belongs in `member.Require` for every route, and a maintainer should decide it.

2. **Criterion 2 is ticked as the maintainer's 2026-09-29 decision on TKT-01M3Q2V25E reworded it.** "Session rows are unchanged" became: approval adds no session and deletes the applicant's pre-user rows (`DeletePreUserSessions`, in the approval's transaction). "Still resolves as a non-member" became: the old cookie resolves as nobody, and the middleware clears it. `TestApproveIssuesNoSession` checks both, checks that another applicant's session is untouched, and checks that `identity.Login` then signs the member in through `IssueUser`. `TestOneDoorIntoSessions` passes with `doorCallers`, `insertCallers` and `issuerCallers` unchanged.

3. **Two departures from the groomed plan, both recorded in the revised plan:** a taken handle is 422 `validation-failed` with `errors[handle] = taken`, not 409, because only a 422 body carries field errors, and a bare 409 could not be told from "already decided" or "identity gone". The errors are coded with `problem.Coded` where they are raised, not mapped in `problem/domain.go`, because that would be an import cycle.

**agent:claude-code/phase2-run-cp2mg** at 2026-09-29T19:51:41Z

2026-09-29. This note supersedes one line of the revised plan: "The three codes join `forms.Codes` and docs/07's form-codes table." After the rebase onto f1129ce, the client lane's `web/client/src/forms/messages.test.ts` (TKT-01M29CP2NG) requires client wording for every code in that table. Adding the wording would rebuild `internal/web/spa/dist`, which this server-lane PR must not change. The codes `invalid_handle`, `reserved` and `taken` are now constants in `internal/service/applications` (a handle is not a form field), and docs/07 lists them with the approve route. The wire shape is unchanged: `validation-failed` with `errors: [{field: "handle", code}]`. The admin review screen (TKT-01M3NRX2NN) needs its own wording for these three codes.

## Summary

Staff now review approval-mode applications through six routes under `/api/v1/admin/applications`, all behind the staff gate. They list, read, note, approve, deny (optionally with a block), and unblock. The service (`internal/service/applications/review.go`) checks `member.RequireStaff` again.

Approval is one transaction that runs these steps in order: CheckHandle, confirm the application is pending, confirm the identity can be linked, HandleFree, CreateUser (active, member, human), link the identity, DecideApplication (status and user_id in one statement, guarded on `pending`), DeletePreUserSessions, and write `application.approved`. It issues no session, and the chokepoint lists are unchanged.

Deny requires a note, which the applicant never sees. A block must mark exactly one identity, or the denial rolls back. Unblock writes `identity.unblocked`, which uses a new audit target type, `identity`.

Handle refusals are 422 with `errors[handle]`, carrying one of three new form codes. Docs 04, 05 and 07 are updated. The dto goldens and the client contract types have the new shapes, and the client dist is unchanged.

Criterion 7 is unticked because a suspended moderator gets 401, not 403. See the 2026-09-29 note.
