---
schema: 3
id: TKT-01M29C8005084NNVCQ6YFCSXYR
title: Implement open-mode registration with handle selection
type: task
status: done
status_reason: null
priority: high
due_on: null
labels:
  - area:identity
  - effort:M
assignees: []
milestone: phase-1
parent: TKT-01M29BKBKZW26HNHFM1YFFGVZC
origin: null
dependencies:
  - TKT-01M29C5Y3E5SKHFR2SHJ8Y60TP
  - TKT-01M29BYYH73ZBC9WTS7HN03PKJ
  - TKT-01M29C7ZZR3FJMZ7P7VA0CWNJ2
blocks_on: none
references:
  - ref: design:identity-registration
    path: docs/05-identity-and-accounts.md
  - ref: pr:102
    path: null
claim: null
archive: null
created_at: 2026-09-11T23:18:33Z
updated_at: 2026-09-28T07:18:39Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase1-run-438e26c1
  name: ""
extensions: {}
---

## Description

The `mode = "open"` path: a one-field screen asking for a handle, prefilled from `preferred_username` or the email local part, checked for uniqueness, then a user with status active, an identity, and a session.

The handle is chosen here rather than taken from the IdP, which is the decision that lets a user keep their identity when an organization changes providers. Uniqueness is case-insensitive, and the check needs to happen at the database rather than only in the form, because two people can submit the same handle in the same second.

## Acceptance criteria

- [x] A handle is validated for character set, length, and case-insensitive uniqueness before the user is created
- [x] A collision between two concurrent registrations produces one user and one clear error, not two users
- [x] Reserved handles such as admin and deleted-user prefixes are refused
- [x] The user, the identity, and the session are created atomically or not at all

## Implementation plan

Revised at claim (2026-09-28), from the code as it stands on main at 3d7c3a5.

### Approach
1. Callback, signed-out not-found branch (`identity.Register` outcome): read `registration.mode` through `settings.RegistrationMode`. When it is `"open"`, seal the verified claims (provider, issuer, subject, email, email_verified, name, preferred_username) and the flow's `return_to` into `__Host-ketju_reg` (purpose `registration`, 15 minutes, same attributes as the login cookie) and 303 to `/auth/register`. Otherwise keep the existing 501 "Registration is not open yet" page (approval mode is TKT-01M29CKRG9; auto_approve is TKT-01M29CKRE9, phase-2, and is not treated as open here).
2. The handle screen is a server-rendered page in `internal/web/auth`, like the other sign-in pages, not a client screen: no client ticket owns one, and the auth package already serves plain pages so that signing in does not depend on the client loading. `GET /auth/register` shows one field prefilled by `identity.Prefill`; `POST /auth/register` takes the form field `handle`. CrossOriginProtection and the /auth rate limit already cover it.
3. `identity.CheckHandle`: lowercase ASCII letters only (strings.ToLower folds the Kelvin sign to `k`), trim space, `domain.ValidateHandle`, then the reserved list. The trailing-`-` handle stays valid (TKT-01M3ANX1NF owns that); the prefill trims one so it never suggests an unmentionable handle.
4. `identity.Register` runs `session.IssueUserWith`, a new door that runs a create function in the transaction that inserts the session, then applies IssueUser's exact checks. Inside it: the identity must not exist (`ErrAlreadyRegistered`), the handle must be free under NOCASE (`ErrHandleTaken`), then CreateUser, CreateIdentity, RecordIdentityLogin. A unique violation on either insert maps to the same errors (`db.IsUniqueViolation`, in driver.go). Then the shared `settle`: end the replaced session, `ApplyBootstrap` for the new user; failure ends the new session, as in Login.
5. POST refuses a browser that is a member by then (linking page, 409), so registering never makes a second account.

### Files
New: internal/service/identity/handle.go, register.go; internal/web/auth/register.go; tests beside each. Changed: session/issue.go (IssueUserWith; chokepoint list), identity/login.go (settle shared), db/driver.go (IsUniqueViolation), web/auth auth.go and page.go. Docs: 04 (reserved list), 05 (mode = open flow), 07 (/auth/register route and statuses).

### Tests
- AC1: table-driven CheckHandle (charset, 2/3/30/31 length, case, Kelvin sign), CheckHandleKeepsTheRule over domain.HandleCases, "TAKEN" against "taken" at the service and "ALICE" at the screen.
- AC2: eight goroutines register alice in eight casings against a real SQLite file under -race: one user, seven ErrHandleTaken. Mutation: dropping the pre-check leaves the unique index to catch it; dropping both fails the test.
- AC3: admin/Admin/ADMIN, each route word, deleted-user, deleted-user-7, Deleted-User-7 refused; near misses accepted.
- AC4: a trigger that aborts the session insert, and one that aborts the identity insert, leave zero users, identities and sessions.
- Bootstrap: a config-named subject registers as admin with one audit row; others stay members; a failing bootstrap ends the session.
- HTTP: whole flow with prefill and return_to, refusals re-render with 422/409, expired/tampered/flow cookie refused, double submit, closed midway, member in another tab, replaced pre-user session ended, cross-site form refused.

### Settled in grooming (2026-09-24)

- The handle rule is in docs/04. Reserve handles that collide with top-level routes: api, auth, admin, assets, blobs, events, healthz, me, new.
- The verified claims travel from the callback to the handle submit in the sealed `__Host-ketju_reg` cookie.
- Define the route and shape in docs/07 in this PR.

## Notes

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T07:27:10Z

From TKT-01M29BYYH7, 2026-09-24: the handle rule is `domain.ValidateHandle` in `internal/domain`. It returns an error that wraps ErrInvalidHandle and says which part failed. It does not lowercase: normalise first, then validate what will be stored. The users CHECK enforces the same rule, and `domain.HandleCases` is the shared test table.

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T07:13:00Z

Gaps settled while building, each written into the doc named:

- Where the handle screen lives (docs/07, docs/05): a server-rendered page at `/auth/register`, not a client screen or a JSON endpoint. Alternatives: a JSON `POST /api/v1/register` plus a client screen, as the pre-groom plan had it. That lost because no ticket owns the client screen, so the flow would have ended at a page nothing renders, and because `internal/web/auth` already serves its sign-in pages as plain HTML so that signing in does not depend on the client loading. The route sits under /auth, so the existing /auth rate limit and CrossOriginProtection cover the form post with no new middleware. No DTO was added, so there are no DTO goldens.
- Registration cookie lifetime, 15 minutes (docs/05): a limit with no product meaning beyond "long enough to type a handle".
- The prefill algorithm (docs/05). It trims a trailing `-` so it never suggests a handle TKT-01M3ANX1NF (Handles ending in '-' can never be mentioned) makes unmentionable. The rule itself is unchanged: a member may still type `bob-`, as the draft says, and this PR does not decide it.
- Display name: the IdP's `name` trimmed, or the handle when there is none. No length rule exists for display names, and none is invented here.
- Lowercasing is ASCII-only (docs/04), so the Kelvin sign is refused rather than folded to `k` by strings.ToLower.
- auto_approve under approval mode is not treated as open. docs/05 says it behaves as open, but that is TKT-01M29CKRE9 (phase-2, AC3), and TKT-01M29BSPEX has not decided whether it skips the screen.

Structural change: `session.IssueUserWith` is a new exported door beside IssueUser. It runs the caller's writes in the session's transaction, then IssueUser's own status check on what they wrote. IssueUser now calls it, and chokepoint_test's insertCallers names it in IssueUser's place. This is what makes AC4 hold. The alternative was two transactions with a compensating delete, and it lost: users are never deleted (docs/04), and a crash between the two would leave a user with no way in.

Bootstrap: Register calls ApplyBootstrap for the new user straight after the commit, through the same `settle` step Login now shares. If the promotion fails, the new session is ended and registration fails. The account stands, and the next sign-in retries the promotion, as for Login.

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T07:15:43Z

Review 1145 (run fc532a90-d988-4da4-b339-c195c733f6d5, Actions run #556, head 82bf6f0aaecad356e64e154b36ca157c6c8b5e5a, base 3d7c3a5c2dfd5acf2db12fef199a871d5c998e38) found one medium issue: registration.mode was read before the account transaction, so an admin closing registration in between would be missed. Accepted and fixed by changing the flagged behaviour. settings gains Key.In, which reads a setting inside a transaction the caller holds. Register now reads registration.mode inside the IssueUserWith create function, in the transaction that writes the account, and the earlier read is gone. TestRegistrationClosedJustBeforeTheWrite closes registration between Register's start and its transaction and gets ErrRegistrationClosed with nothing written. Moving the check back out makes that test fail.

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T07:18:39Z

Review 1146 (run 18ebac77-b8b0-4b51-bb03-faa637ed0c0d, Actions run #558, head ff6ce5d0c832e9b562f96a3061703996f0b7ddda, base 3d7c3a5c2dfd5acf2db12fef199a871d5c998e38) marked review 1145's finding resolved and found one new medium issue: unbounded IdP claims sealed into the registration cookie could exceed the size a browser keeps, so a valid sign-in would reach /auth/register with no registration. Accepted and fixed by changing the flagged behaviour. sealRegistration keeps the sealed value within MaxRegistrationCookie (3800 bytes). While it is too big, it sheds in order: name and preferred_username (the display name falls back to the handle, the prefill starts empty), then return_to (becomes /). If what the account needs still does not fit, the callback refuses with the sign-in-failed page rather than setting a cookie the browser would drop. No display-name length rule was invented. The bound and the order are in docs/05. Tests: TestLargeClaimsStillFitTheCookie, TestALongNameKeepsTheReturnTo and TestClaimsThatCannotFitAreRefused. Both mutations are caught: the bound lifted, and return_to shed first.

## Summary

Open registration works end to end. With registration.mode = open, a new identity's callback seals its claims into __Host-ketju_reg and redirects to the server-rendered handle screen at /auth/register. The screen is prefilled from preferred_username or the email's local part. identity.Register checks the handle (ASCII-lowercased, domain rule, reserved list), then writes the user, identity and session in one transaction through the new session.IssueUserWith door, and applies the bootstrap to the new user. Races resolve to one account plus ErrHandleTaken, with the NOCASE unique index behind the in-transaction check. docs/04, 05 and 07 carry the reserved list, the flow and the route. Approval mode and auto_approve are unchanged (TKT-01M29CKRG9, TKT-01M29CKRE9).
