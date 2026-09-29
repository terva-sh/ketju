---
schema: 3
id: TKT-01M29CP2KZPR0880MGFV3CPXR4
title: Implement application submit, status, and withdraw
type: task
status: blocked
status_reason: "Terva reviews 1425 and 1427 on PR #154 raise a medium session-contract finding: after approval GET /apply/status cannot answer approved, because LookupSession and session.Middleware retire the pre-user cookie. Fixing it is a security/product decision; see TKT-01M3Q2V25E"
priority: high
due_on: null
labels:
  - area:identity
  - area:api
  - effort:M
assignees: []
milestone: phase-2
parent: TKT-01M29BKBM38VTYY4Y91018GTH6
origin: null
dependencies:
  - TKT-01M29CKRG9XYJS4HM1VY1QSJ1V
  - TKT-01M29CKRDQHB528ZW1FFBN4SA1
  - TKT-01M29CP2KEEB60VEVBPY9H2GBD
  - TKT-01M3Q2V25EXKYCTJZ24PM8RP7T
blocks_on: none
references:
  - ref: design:approval-mode
    path: docs/05-identity-and-accounts.md
  - ref: pr:154
    path: null
claim:
  actor: agent:claude-code/phase2-run-cp2kz
  branch: run/cp2kz-block
  worktree: /home/sothr/workspace/git.local.sothr.com/terva-sh/ketju/.claude/worktrees/agent-a03bb477c04f0b009
  commit: 3eab1e9f08455cf00d271b59a5e0f298a1eaf0da
  session: null
  claimed_at: 2026-09-29T17:28:33Z
  expires_at: null
archive: null
created_at: 2026-09-11T23:26:15Z
updated_at: 2026-09-29T17:29:03Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase2-run-cp2kz
  name: ""
extensions: {}
---

## Description

`GET /apply/form`, `POST /apply`, `GET /apply/status`, and `POST /apply/withdraw`, all reachable from a pre-user session only.

Re-visiting the site with a pending identity lands on the status page. A pending applicant can withdraw and re-apply once per 24 hours, which is the anti-spam limit rather than a courtesy.

## Acceptance criteria

- [ ] Submitting stores the answers normalized by forms.Validate against the active registration form version and sets status pending; a submission naming another version answers 409 and writes nothing
- [ ] For an identity with a pending application, GET /apply/form and POST /apply answer 409 and GET /apply/status answers pending; the client's landing on the status page is TKT-01M29CP2NG's
- [ ] An identity may submit one application per rolling 24 hours, after a withdrawal or a denial alike; an earlier submission answers 429 with Retry-After, tested at +1 h and +24 h
- [ ] A blocked identity gets 403 from GET /apply/form and from POST /apply, and no row is written
- [ ] POST /apply and POST /apply/withdraw are limited per identity by a general post-session stage (web.KeyedLimit) keyed on the pre-user session's identity, tested across changing addresses; the summary says the stage was built, for TKT-01M29P9RSW
- [ ] The four routes are on TKT-01M29CKRG9's pre-user allowlist and its route-enumeration test names them; anonymous gets 401 and an active member 403; every response is no-store
- [ ] This ticket issues no session: TestOneDoorIntoSessions passes with doorCallers and insertCallers unchanged
- [ ] An identity that already has a user is refused by GET /apply/form and POST /apply with 409, checked again inside Submit's transaction, and GET /apply/status answers approved with sign_in; tested with a pre-user session held past approval plus 25 hours
- [ ] ApplicationStatus carries no decision note (maintainer decision 2026-09-29)

## Implementation plan

Groomed 2026-09-29 for the Phase 2 autonomous run (TKT-01M3M9RR26), against main at 7af2c5a.

What exists there:
- `domain.Viewer` has `PreUser` and `IdentityID`, and `session.IssuePreUser` exists (`internal/auth/session/issue.go`). The pre-user session flow and its allowlist are TKT-01M29CKRG9's.
- `GET /me` already answers a pre-user session with `user: null` and `pre_user: {provider}` (`internal/service/users/me.go`, docs/07).
- `internal/web/ratelimit.go` has a private `limiter` with `allow(key)`, used only at chain position 5, keyed by address.

### Settled in grooming

- **Routes** (docs/07 "Resources (v1)"): `GET /apply/form`, `POST /apply`, `GET /apply/status` and `POST /apply/withdraw` under `/api/v1`, reachable from a pre-user session only.
- **Submitting** creates an `applications` row with status `pending` against the active version of the `registration` form (docs/05 "mode = approval" steps 2 and 3).
- **A returning applicant sees the status page** (docs/05 step 4).
- **The 24-hour rule.** docs/05 step 4 says "withdraw and re-apply once per 24 h to avoid spam". It is settled here as counting submissions: an identity may submit at most one application per rolling 24 hours, so a re-application after a withdrawal or a denial waits until 24 hours after the previous submission. It is an anti-spam limit (this ticket's description), so it answers `rate-limited` with `Retry-After`.
- **A blocked identity is refused at the form**, not after submission (this ticket's criterion). The block is set by TKT-01M29CP2MG.
- **An identity that already has a user is refused** (review 1344). Approval deliberately leaves the applicant's pre-user session alive, so it can show "approved" (TKT-01M29CP2MG). Without this check, that session could submit a new application once the previous one is 24 hours old: a pending application for someone who is already a member, which approval could never link. `Form` and `Submit` both check `identities.user_id IS NULL`, and `Submit` checks it again inside its transaction.
- **Maintainer decision 2026-09-29: a denied or blocked applicant sees the status only, never the moderator's note** (recorded on TKT-01M29CP2MG). `ApplicationStatus` has no note field, and a blocked identity gets `can_apply: false` with no reason given.
- **Per-identity rate limiting is this epic's second limiter stage** (docs/05 "Sessions and CSRF", docs/08 "The transport-level ceiling"). It runs after session resolution and is keyed on the pre-user session's identity. The epic's comment asks that whichever of this ticket and TKT-01M29P9RSW lands first build it as a general stage, and say so in its summary.
- **Members get no per-session limit** (docs/08). This stage is keyed only for pre-user sessions.
- **The claims come from the identity row.** `email`, `email_verified`, and `raw_claims_json` for `name` and `preferred_username` feed `from_claim` prefill and the locked fields (docs/05 step 2).

Engineering decisions taken here, with the alternative that lost:
- **One service owns the rules: `internal/service/applications`** (docs/02 lists `service/applications/`). The handlers parse and write, as `internal/web/api` does everywhere.
- **The status route says what is possible, not only what happened.** `ApplicationStatus` carries `can_withdraw`, `can_apply` and `can_apply_at`, so the client and the future HTML client need no copy of the 24-hour rule.
- **A submission names the form version it answered.** A version other than the active one is `conflict`, and the client reloads the form. Accepting the answers against the new version lost, because the applicant would have answered questions they never saw.
- **The per-identity stage is `web.KeyedLimit`**, a general middleware beside the address limiter, reusing its `limiter` and its bounded two-generation map. A limiter private to the apply handler lost, because TKT-01M29P9RSW would then build a second one.

### Approach

1. `internal/store/db/queries/applications.sql`: `GetApplicantIdentity` (id, provider_key, email, email_verified, raw_claims_json, user_id, blocked_at), `LatestApplicationForIdentity`, `PendingApplicationForIdentity`, `InsertApplication`, and `WithdrawApplication`, an UPDATE … SET status='withdrawn', decided_at=? WHERE identity_id=? AND status='pending', as `:execrows`. Regenerate dbq.
2. `internal/service/applications/apply.go`:
   - `Service{DB *db.DB; Forms *formsvc.Service; Now func() time.Time}`. `CooldownAfter = 24 * time.Hour`.
   - The viewer checks: an anonymous viewer is `ErrNoApplicant`, which maps to unauthenticated. Any viewer that is not `PreUser` is `ErrNotApplicant`, which maps to forbidden: a member has nothing to apply for.
   - `Form(ctx, viewer) (dto.ApplicationForm, error)`, in one `DB.Read`:
     - Refusals, in this order: the identity already has a user (`ErrAlreadyMember`, conflict; the status route says approved and gives `sign_in`); the identity is blocked (`ErrBlocked`, forbidden); `registration.mode` is not "approval" (`ErrNotAccepting`, forbidden); a pending application exists (`ErrPending`, conflict); the latest application is younger than `CooldownAfter` (`*TooSoonError`, rate-limited, with `RetryAfter` for `problem.RetryAfterOf`).
     - Otherwise it loads `Forms.Active("registration")` and builds the prefill from the claims for every `from_claim` field.
   - `Submit(ctx, viewer, version int, answers map[string]json.RawMessage) (dto.ApplicationStatus, error)`, in one `DB.Write`:
     - It repeats every refusal inside the transaction, the user check included, so an approval, a block or a mode change between the form and the submit cannot be missed.
     - It requires `version` to equal the active version, or returns `ErrFormChanged` (conflict).
     - It runs `forms.Validate` with the identity's claims and inserts the application with `answers_json` as the normalized answers.
     - The `applications_one_pending` index stands behind the pending check: a unique violation is `ErrPending`.
     - TKT-01M29CP2N0 adds the moderators' notifications inside this transaction.
   - `Status(ctx, viewer) (dto.ApplicationStatus, error)`: the latest application, or `status: "none"`, with the fields below.
   - `Withdraw(ctx, viewer)`: `WithdrawApplication`, where zero rows is `ErrNotPending` (conflict). The row is the applicant's own act, so it writes no audit row; docs/04 audits moderation and administration. Withdrawing writes no notification; TKT-01M29CP2N0's count stops including the application once it is no longer pending.
   - Map each error in `internal/problem/domain.go`.
3. `internal/dto`:
   - `ApplicationForm{form Form, prefill map[string]string}`, using `dto.Form` from TKT-01M29CP2KE.
   - `ApplicationStatus{status, provider, submitted_at, decided_at, can_withdraw, can_apply, can_apply_at, sign_in}`. `status` is one of none, pending, approved, denied or withdrawn. The nullable fields are null when they do not apply. `sign_in` is set only when approved: `/auth/{provider}/start?return_to=/` for the session's provider. The applicant's member session comes from signing in again (TKT-01M29CP2MG).
   - Add goldens, and the types in `web/client/src/api/dto.ts` and `dto-contract.ts`. Run `go generate ./internal/web/spa` and commit any diff.
4. `internal/web/ratelimit.go`:
   - `KeyedLimit(rate Rate, key func(*http.Request) (string, bool), now func() time.Time) func(http.Handler) http.Handler`. A request with no key passes. Over the rate it answers `problem.WriteRetryAfter(…, CodeRateLimited, retry)`.
   - `ApplyRate = Rate{Burst: 5, Every: time.Minute}`, with a comment giving the reason: a person filling a form submits a handful of times, and validation failures included, five a minute is generous.
   - Update the comment block at the "Per-identity limiting" paragraph to say the stage exists.
5. `internal/web/api/apply.go`:
   - The four routes. `POST /apply` takes `{version, answers}` and returns 201 `ApplicationStatus`. `POST /apply/withdraw` takes an empty body and returns 200 `ApplicationStatus`. Every route answers `Cache-Control: no-store`.
   - `POST /apply` and `POST /apply/withdraw` are wrapped in `web.KeyedLimit(web.ApplyRate, identityKey, nil)`, where `identityKey` returns `"identity:<id>"` only for a pre-user viewer. The address stage at position 5 still applies.
   - Add `Applications *applications.Service` to `api.Deps` and wire it in `cmd/ketju/serve.go`.
6. The pre-user allowlist: add the four routes to the positive allowlist TKT-01M29CKRG9 builds, and set its route-enumeration test's expected set to exactly what it then accepts. That set is these four plus whatever CKRG9 already allows, such as `GET /me` and `POST /auth/logout`. If CKRG9 does not record `raw_claims_json` and `email` on the applicant's identity, record them at the sign-in, in `internal/service/identity`, and say so in a note.
7. Docs:
   - docs/07: expand the `/apply` row with the bodies, the codes (401, 403, 409, 422 with errors, 429 with Retry-After) and `no-store`.
   - docs/05 step 4: the 24-hour rule as settled.
   - docs/08 "The transport-level ceiling": say that the post-session stage exists and names `ApplyRate`.

### Tests

- `internal/service/applications/apply_test.go`:
  - `TestSubmitStoresAgainstTheActiveVersion`: the row has form_version = active, status pending and normalized answers_json. A submit naming version 1 while version 2 is active is 409 and writes no row.
  - `TestBlockedIdentityIsRefusedAtTheForm`: `Form` and `Submit` both return `ErrBlocked` (403), and no row is written.
  - `TestLinkedIdentityCannotApply`: approve an application, then move the clock 25 hours. With the pre-user session still held, `Form` and `Submit` return `ErrAlreadyMember` (409), no row is written, and `Status` answers approved with `sign_in`. A variant links the identity between `Form` and `Submit` and shows that `Submit`'s in-transaction check refuses it.
  - `TestPendingIdentityIsSentToStatus`: with a pending application, `Form` and `Submit` are 409 and `Status` is pending with `can_withdraw` set.
  - `TestOneApplicationPer24Hours`: after a submit and a withdraw, `Submit` at +1 h is 429 with Retry-After near 23 h, and at +24 h is 201. The same holds after a denial.
  - `TestWithdrawNeedsAPending`.
  - `TestApplicationsClosedOutsideApprovalMode`: with `registration.mode = open`, `Form` and `Submit` are 403.
  - `TestLockedFieldStoresTheIdentityClaim`, run end to end through `Submit`.
- `internal/web/api/apply_test.go`:
  - `TestApplyRoutesNeedAnApplicant`: anonymous is 401, an active member 403, a pre-user 200.
  - `TestApplyIsLimitedPerIdentity`: a sixth `POST /apply` in a minute from one identity is 429 with Retry-After even from a fresh address each time, while a second identity is unaffected.
  - `TestApplyResponsesAreNoStore`.
  - `TestMissingRequiredFieldIsFieldScoped`: the 422 body's `errors` names the key.
- `internal/web/ratelimit_test.go`: `TestKeyedLimitIsGeneral`, where a key function over a header shows the stage knows nothing of applications, and a request with no key passes.
- TKT-01M29CKRG9's route-enumeration test, updated to the new expected set.
- Chokepoint: this ticket issues no session. `TestOneDoorIntoSessions` passes with `doorCallers` and `insertCallers` unchanged, and a reviewer checks the diff leaves both lists alone.
- The goldens for ApplicationForm and ApplicationStatus, with the client contract test.

### Out of scope

- The pre-user session, its allowlist, and the callback's redirect for an applicant: TKT-01M29CKRG9.
- The form schema, the validator, and the `errors` extension: TKT-01M29CP2KE.
- Notifications on submit and withdraw: TKT-01M29CP2N0.
- Approve, deny, and the block itself: TKT-01M29CP2MG.
- The screens: TKT-01M29CP2NG.
- Lifting a block: TKT-01M29CP2MG (maintainer decision 2026-09-29). After an unblock, this ticket's rules apply unchanged.
- The per-agent bucket: TKT-01M29P9RSW, which reuses `KeyedLimit`.

## Notes

**agent:claude-code/phase2-run-cp2kz** at 2026-09-29T17:28:59Z

Blocked, with the work in PR #154 (branch run/cp2kz-apply, head 66b9669596826b4a00a667f8b9c48d6cd5fd1ae6, base 3eab1e9f08455cf00d271b59a5e0f298a1eaf0da), which is left open for a person. That branch carries the implementation, the revised plan with Settled in grooming unchanged, the notes, AC1 to AC7 and AC9 ticked, and the summary. CI and the reviews ran on it.

Terva review 1425 (on 8a976a5) raised two findings that are now fixed on 66b9669: the viewer is checked before the body is read, and an empty chunked withdraw body is accepted. Review 1427 (run 1f2a0e17-9635-4b20-91a2-5a8fc0866f61, on 66b9669) confirms both are resolved. Both reviews raise the same medium finding, "Approved applicants cannot retrieve their approved status": AC8's GET /apply/status cannot answer approved to the applicant's session after approval. The finding is about the session contract. Under docs/13 it must be fixed or the ticket blocked, never answered by argument. It cannot be fixed in scope. LookupSession stops resolving a pre-user session whose identity has a user (TKT-01M29CKRG9, docs/05 step 1), and session.Middleware clears any cookie that does not resolve, so the applicant's first request after approval deletes the cookie. Reaching "approved" would mean changing both, and TKT-01M29CKRG9's merged design contradicts TKT-01M29CP2MG's settled grooming on exactly this point. That choice is a security and product decision (docs/12 "Gaps the design leaves open", "When a ticket cannot proceed").

TKT-01M3Q2V25E (Decide what an approved applicant sees once their pre-user session ends) lays out the two choices. If the maintainer keeps the merged behaviour and rewords AC8, PR #154 can be rebased onto main (this ticket file will conflict: take the PR's done/ copy) and merged after a fresh review. If the maintainer chooses the retired-session route, that is a new security change, made in TKT-01M3Q2V25E or a successor.

**agent:claude-code/phase2-run-cp2kz** at 2026-09-29T17:29:03Z

in-progress to blocked: Terva reviews 1425 and 1427 on PR #154 raise a medium session-contract finding: after approval GET /apply/status cannot answer approved, because LookupSession and session.Middleware retire the pre-user cookie. Fixing it is a security/product decision; see TKT-01M3Q2V25E
