---
schema: 3
id: TKT-01M29CP2KZPR0880MGFV3CPXR4
title: Implement application submit, status, and withdraw
type: task
status: done
status_reason: null
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
  - ref: pr:155
    path: null
  - ref: pr:156
    path: null
claim: null
archive: null
created_at: 2026-09-11T23:26:15Z
updated_at: 2026-09-29T18:21:47Z
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

- [x] Submitting stores the answers normalized by forms.Validate against the active registration form version and sets status pending; a submission naming another version answers 409 and writes nothing
- [x] For an identity with a pending application, GET /apply/form and POST /apply answer 409 and GET /apply/status answers pending; the client's landing on the status page is TKT-01M29CP2NG's
- [x] An identity may submit one application per rolling 24 hours, after a withdrawal or a denial alike; an earlier submission answers 429 with Retry-After, tested at +1 h and +24 h
- [x] A blocked identity gets 403 from GET /apply/form and from POST /apply, and no row is written
- [x] POST /apply and POST /apply/withdraw are limited per identity by a general post-session stage (web.KeyedLimit) keyed on the pre-user session's identity, tested across changing addresses; the summary says the stage was built, for TKT-01M29P9RSW
- [x] The four routes are on TKT-01M29CKRG9's pre-user allowlist and its route-enumeration test names them; anonymous gets 401 and an active member 403; every response is no-store
- [x] This ticket issues no session: TestOneDoorIntoSessions passes with doorCallers and insertCallers unchanged
- [x] ApplicationStatus carries no decision note (maintainer decision 2026-09-29)
- [x] An identity that already has a user is refused by GET /apply/form and POST /apply with 409, checked again inside Submit's transaction; GET /apply/status answers approved with sign_in only while the pre-user session still resolves, and never after approval: a pre-user session held past approval plus 25 hours no longer resolves, so every /apply route answers it 401 and writes nothing (maintainer decision 2026-09-29, TKT-01M3Q2V25E)

## Implementation plan

Groomed 2026-09-29 for the Phase 2 autonomous run (TKT-01M3M9RR26), against main at 7af2c5a. Revised at claim on 2026-09-29 against main at 3eab1e9, and carried onto 9d5ef44.

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

Engineering decisions taken in grooming, kept:
- **One service owns the rules: `internal/service/applications`** (docs/02 lists `service/applications/`). The handlers parse and write, as `internal/web/api` does everywhere.
- **The status route says what is possible, not only what happened.** `ApplicationStatus` carries `can_withdraw`, `can_apply` and `can_apply_at`, so the client and the future HTML client need no copy of the 24-hour rule.
- **A submission names the form version it answered.** A version other than the active one is `conflict`, and the client reloads the form. Accepting the answers against the new version lost, because the applicant would have answered questions they never saw.
- **The per-identity stage is `web.KeyedLimit`**, a general middleware beside the address limiter, reusing its `limiter` and its bounded two-generation map. A limiter private to the apply handler lost, because TKT-01M29P9RSW would then build a second one.

### Revised at claim (2026-09-29, main at 3eab1e9; carried onto main at 9d5ef44 as PR #154's successor)

What main holds now, and what changed from the groomed approach because of it:
- `api.Routes(d)` is a table and `web.PreUserRoutes` is a positive list checked against `theAllowlist` in `internal/web/preuser_test.go` (TKT-01M29CKRG9). The four routes go into all three.
- `LookupSession` stops resolving a pre-user session once its identity has a user, and the session middleware then clears the cookie (TKT-01M29CKRG9). So a pre-user session held past approval reaches the /apply routes as anonymous, 401. The maintainer kept that behaviour on 2026-09-29 (TKT-01M3Q2V25E): an approved applicant sees the signed-out shell and learns the decision by signing in, and AC8 was reworded to match. The service keeps its own user check (`ErrAlreadyMember`, 409, repeated inside `Submit`'s transaction) as defence in depth, and `Status` answers approved with `sign_in` only for a session that still resolves.
- `BeginApplication` recorded email and email_verified but not `raw_claims_json`. `identity.Claims` gains `RawClaims json.RawMessage` tagged `json:"-"` (so it never rides in the sealed registration cookie), `ClaimsOf` sets it, and `BeginApplication` records it. A returning applicant's sign-in already recorded it through `recordLogin`.
- `formsvc.Active` opens its own read. `Submit` must check the version active in its own write transaction, so `formsvc.ActiveIn(ctx, q, key)` is exported and `Active` calls it. `applications.Service` therefore needs no `Forms` field: `{DB, Now}`.
- Errors are coded where they are raised (`problem.Coded`, `problem.RetryAfter`), as `formsvc` does, rather than added to `problem/domain.go`'s table.
- `api.Deps` gains `Applications` (required; `New` panics without it, and `TestNewRequiresEveryService` names it) and `Now`, the per-identity limiter's clock.

### Approach

1. `internal/store/db/queries/applications.sql`: `ApplicantIdentity`, `LatestApplicationForIdentity`, `InsertApplication`, `WithdrawApplication` (`:execrows`). Regenerate dbq.
2. `internal/service/applications/apply.go`: `Form`, `Submit`, `Status`, `Withdraw`, `CooldownAfter = 24h`. Refusals in order: user (409), blocked (403), mode not approval (403), pending (409), within 24 h of the last submission (429 with Retry-After). `Submit` reads and checks everything inside its `DB.Write`, loads the active version there, validates with `forms.Validate` and the identity's claims, and stores the normalized answers; a unique violation on `applications_one_pending` is `ErrPending`. `Withdraw` writes no audit row and no notification. Anonymous is 401, any non-pre-user 403.
3. `internal/dto`: `ApplicationForm{form, prefill}` and `ApplicationStatus{status, provider, submitted_at, decided_at, can_withdraw, can_apply, can_apply_at, sign_in}`; five goldens; the matching types in `web/client/src/api/dto.ts` and `dto-contract.ts` (types only; the dist does not change).
4. `internal/web/ratelimit.go`: `KeyedLimit(rate, key, now)` and `ApplyRate = Rate{Burst: 5, Every: time.Minute}`.
5. `internal/web/api/apply.go`: the four routes, every answer `no-store`; the two POSTs share one `KeyedLimit` keyed `identity:<id>` for a pre-user viewer only.
6. `web.PreUserRoutes` and `theAllowlist`: add the four routes.
7. Docs: 07 (the /apply row), 05 (step 1's list, step 4's 24-hour rule, the second limiter stage), 08 (the transport-level ceiling).

### Tests

As groomed, in `internal/service/applications/apply_test.go`, `internal/web/api/apply_test.go` and `internal/web/ratelimit_test.go`, plus `TestAHeldPreUserSessionEndsAtApproval` (the 401 above), `TestBeginApplicationRecordsTheClaims`, and the allowlist test's expected set. The chokepoint test runs unchanged.

### Out of scope

Unchanged from grooming: the pre-user session and allowlist mechanism (TKT-01M29CKRG9), the form schema and validator (TKT-01M29CP2KE), notifications (TKT-01M29CP2N0), approve, deny and the block (TKT-01M29CP2MG), the screens (TKT-01M29CP2NG), lifting a block (TKT-01M29CP2MG), the per-agent bucket (TKT-01M29P9RSW).

## Notes

**agent:claude-code/phase2-run-cp2kz** at 2026-09-29T17:28:59Z

Blocked, with the work in PR #154 (branch run/cp2kz-apply, head 66b9669596826b4a00a667f8b9c48d6cd5fd1ae6, base 3eab1e9f08455cf00d271b59a5e0f298a1eaf0da), which is left open for a person. That branch carries the implementation, the revised plan with Settled in grooming unchanged, the notes, AC1 to AC7 and AC9 ticked, and the summary. CI and the reviews ran on it.

Terva review 1425 (on 8a976a5) raised two findings that are now fixed on 66b9669: the viewer is checked before the body is read, and an empty chunked withdraw body is accepted. Review 1427 (run 1f2a0e17-9635-4b20-91a2-5a8fc0866f61, on 66b9669) confirms both are resolved. Both reviews raise the same medium finding, "Approved applicants cannot retrieve their approved status": AC8's GET /apply/status cannot answer approved to the applicant's session after approval. The finding is about the session contract. Under docs/13 it must be fixed or the ticket blocked, never answered by argument. It cannot be fixed in scope. LookupSession stops resolving a pre-user session whose identity has a user (TKT-01M29CKRG9, docs/05 step 1), and session.Middleware clears any cookie that does not resolve, so the applicant's first request after approval deletes the cookie. Reaching "approved" would mean changing both, and TKT-01M29CKRG9's merged design contradicts TKT-01M29CP2MG's settled grooming on exactly this point. That choice is a security and product decision (docs/12 "Gaps the design leaves open", "When a ticket cannot proceed").

TKT-01M3Q2V25E (Decide what an approved applicant sees once their pre-user session ends) lays out the two choices. If the maintainer keeps the merged behaviour and rewords AC8, PR #154 can be rebased onto main (this ticket file will conflict: take the PR's done/ copy) and merged after a fresh review. If the maintainer chooses the retired-session route, that is a new security change, made in TKT-01M3Q2V25E or a successor.

**agent:claude-code/phase2-run-cp2kz** at 2026-09-29T17:29:03Z

in-progress to blocked: Terva reviews 1425 and 1427 on PR #154 raise a medium session-contract finding: after approval GET /apply/status cannot answer approved, because LookupSession and session.Middleware retire the pre-user cookie. Fixing it is a security/product decision; see TKT-01M3Q2V25E

**agent:claude-code/phase2-run-cp2kz** at 2026-09-29T18:19:05Z

Unblocked on 2026-09-29 by the maintainer's decision on TKT-01M3Q2V25E (option 1: keep the merged behaviour). AC8 is reworded to match: GET /apply/status answers approved only while the pre-user session still resolves, and never after approval. Because `ac --remove`/`--add` reorders the list, it is now the last criterion. docs/05 step 4 and the docs/07 /apply row say the same.

PR #154's base (3eab1e9) was stale, so its tree is carried onto main at 9d5ef44 as a fresh branch, run/cp2kz-apply-2. Only the code commits were cherry-picked (8898757 and ae60078). The ticket-store changes were redone on this branch, because main holds this ticket in its blocked state. Four files conflicted with TKT-01M29CXK1X's admin screens: `api.go` (the handlers struct and the groups service), `admin_test.go` (Deps), `dto-contract.ts` (both sets of goldens) and, in `apply_test.go`, a `problemType` helper name, renamed `applyProblem`. #154 is closed in favour of the new PR, with no force-push.

Carried from the notes on #154's branch, which never reached main:
- The client change is types only. The new goldens force `ApplicationForm` and `ApplicationStatus` into `dto.ts`, and their `matches` lines into `dto-contract.ts`. `go generate ./internal/web/spa` leaves the dist unchanged, and the dist stays with the client lane.
- Review 1425 (head 8a976a5, base 3eab1e9) had two medium findings and one low. The two write handlers now refuse the viewer before reading the body, and the withdraw handler accepts an empty chunked body. Both are fixed in ae60078, and review 1427 (head 66b9669) confirms them resolved. The remaining finding, "Approved applicants cannot retrieve their approved status", blocked the ticket. It is now answered by the maintainer's decision and the reworded AC8 rather than by argument. The earlier note on #154's branch that marked it "disagreement: pending maintainer" is superseded.

## Summary

The application API is built: `GET /api/v1/apply/form`, `POST /api/v1/apply`, `GET /api/v1/apply/status` and `POST /api/v1/apply/withdraw`. Only a pre-user session reaches them, and they are on `web.PreUserRoutes` and `theAllowlist`. The rules live in the new `internal/service/applications`. The form and the submit refuse, in this order: an identity with a user (409), a blocked identity (403), `registration.mode` other than approval (403), a pending application (409), and a submission within 24 hours of the last (429 with Retry-After). `Submit` repeats every check inside its write transaction, against the version active there (`formsvc.ActiveIn`, new), and stores the answers as `forms.Validate` normalizes them. Both write handlers refuse the viewer before reading the body (`applications.Applicant`). `ApplicationStatus` says what is possible (`can_apply`, `can_apply_at`, `can_withdraw`, `sign_in`) and carries no note. Withdrawing writes no audit row. No session is issued, and the chokepoint test runs unchanged.

**The post-session limiter stage was built here, for TKT-01M29P9RSW.** `web.KeyedLimit(rate, key, now)` is a general token bucket per key, where the key is read after session resolution; a request with no key passes. The two application writes share one at `web.ApplyRate` (burst 5, then one a minute), keyed `identity:<id>` for pre-user viewers only.

`identity.BeginApplication` now records the raw claim set, which the form's `from_claim` prefill uses. `identity.Claims.RawClaims` is `json:"-"`, so it never enters the registration cookie. `api.Deps.Applications` is required, and `api.Deps.Now` sets the limiter's clock. The client change is types only (`dto.ts`, `dto-contract.ts`), and the dist is unchanged.

An approved applicant's pre-user session retires: it no longer resolves, and every /apply route answers it 401 (maintainer decision 2026-09-29, TKT-01M3Q2V25E, carried into AC8, docs/05 step 4, TKT-01M29CP2MG and TKT-01M29CP2NG). The work first sat in PR #154 and was carried onto current main as a fresh PR.
