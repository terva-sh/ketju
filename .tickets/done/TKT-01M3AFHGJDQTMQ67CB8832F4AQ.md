---
schema: 3
id: TKT-01M3AFHGJDQTMQ67CB8832F4AQ
title: Test the OIDC callback against a pending user
type: chore
status: done
status_reason: null
priority: normal
due_on: null
labels: []
assignees: []
milestone: null
parent: TKT-01M29BKBKZW26HNHFM1YFFGVZC
origin: null
dependencies:
  - TKT-01M29C7ZZ9KFSNZ8WC8YP1HAYV
blocks_on: none
references:
  - ref: pr:128
    path: null
claim: null
archive: null
created_at: 2026-09-24T19:51:07Z
updated_at: 2026-09-28T19:31:55Z
created_by:
  id: agent:claude-code/phase1-run-415af6aa
  name: ""
updated_by:
  id: agent:claude-code/phase1-run-438e26c1
  name: ""
extensions: {}
---

## Description

TKT-01M29C7ZZR ("Route every session creation through one status-checking function") has AC3: "The OIDC callback path is tested explicitly against a pending, a suspended, and a deactivated user". The callback does not exist until TKT-01M29C7ZZ9, whose own AC4 tests suspended and deactivated users. A pending user is in neither.

docs/05 says approval mode never creates a pending user row, but the schema allows `status = 'pending'` (docs/04), and docs/09's Gitea row is exactly the case where a path the design says cannot happen does. So the callback should be tested against it too.

`session.IssueUser` already refuses a pending user with a `*NotActiveError{Status: "pending"}`, tested in internal/auth/session/issue_test.go. This ticket is about the callback's handling of that refusal.

## Acceptance criteria

- [x] With the fake IdP, signing in with the identity of a user whose status is pending sets no session cookie, leaves the sessions row count unchanged, and shows the same unavailable page as suspended and deactivated
- [x] For pending, suspended and deactivated alike, a callback sign-in also records no login on the identity, and afterwards GET /api/v1/me shows no user and a member-only endpoint answers 401
- [x] A member's session that outlives a change to pending, suspended or deactivated carries no member rights; signing in again from that browser is refused and issues no second session; after RevokeUser the cookie is cleared
- [x] Registration (/auth/register) issues no session for an identity that a pending, suspended or deactivated user holds, and a pending user's own sign-in never reaches the handle screen
- [x] GET /auth/signin never issues a session, whoever asks

## Implementation plan

No `### Settled in grooming` existed; the ticket had no plan section. The orchestrator's brief for the sweep (2026-09-28) widened the scope from the one criterion to docs/09's approval-bypass row as a whole, tested through the real HTTP routes.

### What the code does today
- `session.IssueUserWith` is the only function that writes a user session. `IssueUser` and registration both go through it. It reads the user's status inside the transaction that inserts the row and refuses anything but `active`.
- The callback (`GET /auth/{provider}/callback`) passes a refusal on as `identity.Unavailable` and answers 403 "Account unavailable".
- A session row outlives a status change until something revokes it, which is suspension's job (docs/05) and is not built yet. `Session.Viewer()` marks an inactive user as not `Active`, so `Member()` is false and `/api/v1/me` answers the anonymous shape.
- `/auth/signin` issues nothing. It either redirects or renders a page.
- Registration always creates an `active` user. The only way it meets a pending user is when that user already holds the identity, and then `ErrAlreadyRegistered` refuses it.

### Tests (new file internal/web/auth/pending_test.go)
The test world gains the API group (users, threads and forums services), so a test can follow a sign-in with `GET /api/v1/me` and `GET /api/v1/drafts/{id}` through the same middleware chain.
1. For each of pending, suspended and deactivated, the callback answers 403 with the unavailable page. It sets no session cookie and writes no session row, and it does not record the sign-in on the identity. Afterwards, /me shows no user and the member-only draft endpoint refuses. An active control proves the same probes pass for a member.
2. An active member signs in and then becomes suspended, with the session row still present. /me shows no user and the draft endpoint refuses. Signing in again answers "Account unavailable" and issues no second session. After `RevokeUser`, the cookie is dead and gets cleared.
3. Registration: a registration started for an identity that a pending user then comes to hold (seeded between the callback and the submit) is refused as already registered, with no session. A pending user's own sign-in never reaches the handle screen.
4. `/auth/signin`: for an anonymous reader, a pending holder of a session, and an active member, it sets no session cookie and writes no row.

### If a test exposes a bypass
Fix it in this PR only if the fix is small and in scope. Otherwise block the ticket, file a high-priority bug and report it.

## Notes

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T20:44:38Z

The pending case this draft asks for is covered by TKT-01M29C7ZZ9's TestAnInactiveAccountGetsNoSession/pending (internal/web/auth/auth_test.go). With the in-process IdP, a pending user's sign-in answers 403 'Account unavailable', sets no session cookie, and writes no session row. So this draft's criterion is met once that PR merges, and it can be archived rather than promoted.

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T19:07:30Z

No bypass found. Every route that can end in a session refused each status other than active. internal/web/auth/pending_test.go runs the real routes (callback, /auth/register, /auth/signin) against the in-process IdP, with the JSON API mounted behind the same session middleware. A test counts the browser as a member only when /api/v1/me names a user and GET /api/v1/drafts/{id} passes the member check.

### Mutations tried against the new tests
- IssueUserWith lets a `pending` status through: caught by the callback pending case, by the outliving-session pending case, and by registration's "signing in" case.
- `Session.Viewer()` always sets Active: caught by all three outliving-session cases.
- `/me` gates on `UserID != 0` instead of `Member()`: caught by all three outliving-session cases.
- Login turns a NotActiveError into Register: caught by the callback, outliving-session and registration tests.
- Register signs in as the existing identity instead of refusing ErrAlreadyRegistered: caught, since the answer is 500, not 409. No session was issued even then, because IssueUserWith still refused the status. That is the chokepoint working.

### Seen, not changed
`/auth/signin` treats any session that has a user id as signed in, active or not, and redirects straight back (`UserID != 0`, signin.go). If a suspended, deactivated or pending user still holds a session, the client offers them "Sign in", and the login page then bounces them back without issuing anything. It is not a bypass. docs/05 says suspending deletes sessions, so the case should not arise once suspension exists. No draft filed.

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T19:20:05Z

Review 1231 (run 33745816-825b-4b87-a052-b68fe362270b, Actions run 746, head 21118faa112afab7ff85e4de50065511b0c4fd1b, base 86e1e2e64ba90ed0e667fa0c411fffc8ef47ec65) was clean at the gate, with one low finding: TestTheLoginPageIssuesNoSession had no valid session belonging to a user who has since become inactive. Fixed. The table now includes a session for pat, who signed in while active and was then set to pending without the session being revoked, and the test afterwards checks that pat is not a member. The next head gets a fresh review.

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T19:31:55Z

CI run 751 on head d2fbb3680bf88a719087dbc2d862c96c83480c24 failed one Playwright test: [desktop] e2e/a11y.spec.ts, 'the thread page passes the scan in both themes'. It hit the 30 s test timeout while waiting to click '#post-1 button.reply-expander'. The other 52 tests passed. This PR touches no client code, only one Go test file and the ticket store. The same spec passed in run 745 on head 21118fa, and in main's push run 747 on 4302ad1, which is the PR's current base. This commit touches only the ticket store. It reruns CI, and its review is carried from head d2fbb36 (clean review run 11f27ea0-f6c3-4a62-bc20-ce4a6e9a28b0).

## Summary

The approval-bypass row of docs/09 is pinned end to end. internal/web/auth/pending_test.go drives the OIDC callback, /auth/register and /auth/signin through the real middleware against the in-process IdP, for pending, suspended and deactivated users and for a session that outlives a status change, and checks the cookie, the sessions table, the identity's last_login_at, /api/v1/me and a member-only endpoint. No bypass was found; no production code changed.
