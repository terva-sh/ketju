---
schema: 3
id: TKT-01M3QVWKAGAG6H77VNE0WEC3SH
title: Give signed-in members a Sign out control
type: task
status: ready
status_reason: null
priority: normal
due_on: null
labels:
  - area:client
  - effort:S
assignees: []
milestone: phase-2
parent: TKT-01M29BKBKZW26HNHFM1YFFGVZC
origin: TKT-01M3QS0TMVM2XWYTAPB7ZRJZZG
dependencies:
  - TKT-01M3QS0TMVM2XWYTAPB7ZRJZZG
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-30T00:37:01Z
updated_at: 2026-09-30T01:44:28Z
created_by:
  id: agent:claude-code/phase2-groom
  name: ""
updated_by:
  id: agent:claude-code/phase2-run-orchestrator
  name: ""
extensions: {}
---

## Description

A signed-in member cannot sign out from the client. Only the applicant shell has a Sign out control (`shell/ApplicantShell.tsx`, a form posting to `/auth/logout`). The member's shell has none, and the session list with "sign out here and everywhere" belongs to TKT-01M29CXK4 (Build the user settings screens), which is phase-4. Until then, a member on a shared computer has no way to end their session short of clearing cookies.

Found while grooming TKT-01M3QS0TMV (Link to the link-a-sign-in page from the client) in Phase 2 grooming (TKT-01M3M9RR26). By the maintainer decision of 2026-09-29, the control goes on the member's own profile page, next to "Link a sign-in", because there is no account menu.

The server route exists: `POST /auth/logout` revokes this browser's session, clears the cookie, and answers 303 to `/` (`internal/web/auth/auth.go`, `logout`). The server's cross-origin check holds it to this site.

## Acceptance criteria

- [ ] On their own profile page a signed-in member finds a form posting to /auth/logout with a Sign out button, and it is absent on another member's profile and for a signed-out reader (component test)
- [ ] At 390 px and 1440 px a member reaches their profile in one press, presses Sign out, and lands on / signed out, with /api/v1/me answering user null and a reload not restoring the session (e2e/sign-out.spec.ts)
- [ ] ApplicantShell and the profile page render the same SignOutForm from shell/SignOut.tsx, and routes.test.tsx's applicant sign-out test still passes
- [ ] The dist from go generate ./internal/web/spa is committed and the thread-view bundle stays under threadViewGzipBytes in budget.json

## Implementation plan

Groomed 2026-09-30 for Phase 2 (TKT-01M3M9RR26), against main at 43a844d. It builds on TKT-01M3QS0TMV (Link to the link-a-sign-in page from the client), which makes the own-profile page and its Account block.

### Settled in grooming

- **Placement** (maintainer decision 2026-09-29, relayed by the Phase 2 coordinator on PR #181). Sign out goes on the member's own profile page (`/u/{handle}`, the profile placeholder), next to "Link a sign-in", because there is no account menu. The desktop top bar's handle and the phone's Me tab reach that page in one press.
- **A plain form post to `POST /auth/logout`**, as `ApplicantShell` already does. The server revokes this browser's session (`Sessions.Revoke`), clears the cookie, and answers 303 to `/`, `no-store`. The cross-origin check in front of every write (`internal/web/csrf.go`, `http.CrossOriginProtection`) holds it to this site. The browser leaves the page, so the client's viewer state is rebuilt from the next page's bootstrap and nothing in the client must be cleared by hand.
- **This browser only.** "Sign out everywhere" and the session list are TKT-01M29CXK4 (Build the user settings screens, phase-4).
- **The built client is committed.** Run `go generate ./internal/web/spa` and commit `internal/web/spa/dist`. Only one lane may have a PR open that changes the dist (docs/13 "Running two lanes").

Engineering choices made here, each with the alternative that lost:
- **One sign-out form for both shells.** A new `shell/SignOut.tsx` exports `SignOutForm({class?})`: `<form method="post" action="/auth/logout"><button type="submit">Sign out</button></form>`. `ApplicantShell` and the profile page both use it. Copying the form into the profile page lost, because two copies of a security-relevant form drift.
- **A form, not a `fetch`.** A `fetch` POST followed by a client-side reset lost. The server's answer is a redirect meant for a page load. A page load also drops every signal, cached resource and draft still in memory, where a reset in place could miss one.
- **No confirmation.** Signing out loses nothing that signing in again does not bring back, because drafts are stored on the server. A confirm step lost for that reason.

### Approach

1. **`shell/SignOut.tsx`**: `SignOutForm`, with a comment naming the cross-origin check and the 303.
2. **`shell/ApplicantShell.tsx`**: replace its inline form with `<SignOutForm class="sign-out" />`. The markup the tests read is unchanged: `form[action="/auth/logout"]`, and a submit button reading "Sign out".
3. **`screens/ProfilePlaceholder.tsx`** (from TKT-01M3QS0TMV): the Account block (`data-account`), shown only on the viewer's own profile, renders `<SignOutForm />` after `<LinkSignIn />`.
4. **CSS**: the button meets `var(--target)` at 390 px.
5. Run `go generate ./internal/web/spa` and commit `internal/web/spa/dist`.

### Tests

- **Client, `shell/SignOut.test.tsx` or the profile page's test**:
  - `a member can sign out from their profile`: on `/u/{their handle}` there is a `form` with `method="post"` and `action="/auth/logout"` whose submit button reads "Sign out".
  - `no sign-out elsewhere`: none on another member's profile, and none for a signed-out reader.
- **`routes.test.tsx`**: `can sign out, and has no forum navigation` (the applicant shell) still passes with the shared form.
- **e2e, new `e2e/sign-out.spec.ts`**, run in both Playwright projects (390 px and 1440 px):
  - A signed-in member opens their profile from the handle or the Me tab and presses Sign out.
  - They land on `/` signed out: the sign-in hint shows, and `/api/v1/me` answers `user: null`.
  - Going back and reloading does not restore the session.
- **Invariants held by the server's tests, which stay green:**
  - The session is revoked, not only the cookie cleared: `TestLogout` in `internal/web/auth/auth_test.go` and `TestAnApplicantCanSignOut`.
  - A cross-site sign-out post is refused by the CSRF middleware's tests in `internal/web/csrf_test.go`.
  - This ticket issues no session and changes no server code, so `TestOneDoorIntoSessions` is untouched.

### Out of scope

- Signing out of every session, and the session list: TKT-01M29CXK4.
- An account menu, which the maintainer did not choose.
- Any change to `POST /auth/logout`.
