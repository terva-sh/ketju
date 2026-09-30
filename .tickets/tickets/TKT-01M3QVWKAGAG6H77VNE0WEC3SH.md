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
updated_at: 2026-09-30T08:54:26Z
created_by:
  id: agent:claude-code/phase2-groom
  name: ""
updated_by:
  id: agent:claude-code/phase2-groom
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

Groomed 2026-09-30 for Phase 2 (TKT-01M3M9RR26), against main at 43a844d. It builds on TKT-01M3QS0TMV (Link to the link-a-sign-in page from the client), which makes the own-profile page and its Account block. Refreshed against main at 5124209 on 2026-09-30: the form's class and the e2e spec's session corrected. Nothing this plan builds has landed.

### Settled in grooming

- **Placement** (maintainer decision 2026-09-29, relayed by the Phase 2 coordinator on PR #181). Sign out goes on the member's own profile page (`/u/{handle}`, the profile placeholder), next to "Link a sign-in", because there is no account menu. The desktop top bar's handle and the phone's Me tab reach that page in one press.
- **A plain form post to `POST /auth/logout`**, as `ApplicantShell` already does. The server revokes this browser's session (`Sessions.Revoke`), clears the cookie, and answers 303 to `/`, `no-store`. The cross-origin check in front of every write (`internal/web/csrf.go`, `http.CrossOriginProtection`) holds it to this site. The browser leaves the page, so the client's viewer state is rebuilt from the next page's bootstrap and nothing in the client must be cleared by hand.
- **This browser only.** "Sign out everywhere" and the session list are TKT-01M29CXK4 (Build the user settings screens, phase-4).
- **The built client is committed.** Run `go generate ./internal/web/spa` and commit `internal/web/spa/dist`. Only one lane may have a PR open that changes the dist (docs/13 "Running two lanes, changed", `docs/13-phase2-run.md:104`).

Engineering choices made here, each with the alternative that lost:
- **One sign-out form for both shells.** A new `shell/SignOut.tsx` exports `SignOutForm({class?})`: `<form method="post" action="/auth/logout"><button type="submit">Sign out</button></form>`. `ApplicantShell` and the profile page both use it. Copying the form into the profile page lost, because two copies of a security-relevant form drift.
- **A form, not a `fetch`.** A `fetch` POST followed by a client-side reset lost. The server's answer is a redirect meant for a page load. A page load also drops every signal, cached resource and draft still in memory, where a reset in place could miss one.
- **No confirmation.** Signing out loses nothing that signing in again does not bring back, because drafts are stored on the server. A confirm step lost for that reason.

- **`class` goes on the button** (settled in the refresh of 2026-09-30). In `ApplicantShell` today the class is on the `<button>`, not the `<form>` (`<button type="submit" class="sign-out">`), and `shell.css` styles `.topbar.applicant .sign-out` (`:193`) with the form's margin at `:190`. So `SignOutForm({class})` puts `class` on the button, and the applicant's styling holds unchanged. Moving the class to the form lost, because it changes a selector for no gain.
- **The e2e spec signs out a session of its own** (settled in the refresh of 2026-09-30). Signing out revokes the session on the server, so a spec that loaded `stateFile('carol')` and pressed Sign out would revoke the session every later spec loads from that file. The spec starts a new browser context with no stored state and signs in as carol with `signIn` (`e2e/session.ts:9`), which makes a new session; the stored one survives. Each `/auth/*` navigation, the sign-in's and `POST /auth/logout`, goes through `through()`, which TKT-01M3QS0TMV moves into `e2e/session.ts`, because the setup has already spent 8 of the 10 `/auth/*` requests allowed in a burst. `signIn` calls `page.goto` and clicks directly today, so it is changed to make both navigations through `through()`; the setup project's sign-ins then wait out a 429 too, which changes nothing while they stay inside the burst. Running the spec last lost, because Playwright's order across projects is not a contract. A new seed member for this spec alone lost, because a fresh session of an existing member does the same without a Go change.
- **Local draft copies need no hook here.** TKT-01M3NS1931 (Autosave reply drafts from the composer) keeps a browser-local copy of an unsent draft, keyed by handle, and clears copies that are not the current viewer's on the next page load. A plain form post runs no client code, and needs none: the page after sign-out loads with no viewer, and that load clears them.

### Approach

1. **`shell/SignOut.tsx`**: `SignOutForm`, with a comment naming the cross-origin check and the 303.
2. **`shell/ApplicantShell.tsx`**: replace its inline form with `<SignOutForm class="sign-out" />`, whose button carries the class. The markup the tests read is unchanged: `form[action="/auth/logout"]`, and a submit button with `class="sign-out"` reading "Sign out".
3. **`screens/ProfilePlaceholder.tsx`** (from TKT-01M3QS0TMV): the Account block (`data-account`), shown only on the viewer's own profile, renders `<SignOutForm />` after `<LinkSignIn />`.
4. **CSS**: the button meets `var(--target)` (44 px) at 390 px.
5. Run `go generate ./internal/web/spa` and commit `internal/web/spa/dist`. The thread view was 47415 B of 60000 B after PR #202 (TKT-01M3QQWA5Z); record the new figure in a note.

### Tests

- **Client, `shell/SignOut.test.tsx` or the profile page's test**:
  - `a member can sign out from their profile`: on `/u/{their handle}` there is a `form` with `method="post"` and `action="/auth/logout"` whose submit button reads "Sign out".
  - `no sign-out elsewhere`: none on another member's profile, and none for a signed-out reader.
- **`routes.test.tsx`**: `can sign out, and has no forum navigation` (`:290`, the applicant shell) still passes with the shared form.
- **e2e, new `e2e/sign-out.spec.ts`**, run in both Playwright projects (390 px and 1440 px):
  - In a new context with no stored state, the spec signs in as carol through `signIn`, as settled, so the session it revokes is its own.
  - carol opens her profile from the handle (1440 px) or the Me tab (390 px) and presses Sign out.
  - She lands on `/` signed out: the sign-in hint shows, and `/api/v1/me` answers `user: null`, checked as `phase2-exit.spec.ts:100-101` does.
  - Going back and reloading does not restore the session.
- **Invariants held by the server's tests, which stay green:**
  - The session is revoked, not only the cookie cleared: `TestLogout` (`internal/web/auth/auth_test.go:532`) and `TestAnApplicantCanSignOut` (`internal/web/auth/apply_test.go:201`).
  - A cross-site sign-out post is refused by the CSRF middleware's tests in `internal/web/csrf_test.go`, `TestACrossOriginFormPostIsRefused` and `TestACrossOriginRequestByOriginHeaderIsRefused`.
  - This ticket issues no session and changes no server code, so `TestOneDoorIntoSessions` is untouched.

### Out of scope

- Signing out of every session, and the session list: TKT-01M29CXK4.
- An account menu, which the maintainer did not choose.
- Any change to `POST /auth/logout`.

## Notes

**agent:claude-code/phase2-groom** at 2026-09-30T08:54:26Z

Plan refreshed against main at 5124209 on 2026-09-30. Nothing it builds has landed. The Settled items are carried unchanged except the docs/13 heading, now 'Running two lanes, changed'. Settled as engineering choices, with the alternatives that lost in the plan: SignOutForm puts its class on the button, where ApplicantShell has it today and where shell.css's .topbar.applicant .sign-out styles it (the earlier plan read as if the class were on the form); the e2e spec signs out a session of its own, signed in fresh as carol in a new context, because signing out from stateFile('carol') would revoke the session every later spec loads; its /auth/* navigations go through the through() helper TKT-01M3QS0TMV moves into e2e/session.ts, and signIn is changed to use it, since the setup spends 8 of the 10-request /auth/* burst. The browser-local draft copies of TKT-01M3NS1931 need no hook in the form: that ticket clears copies not belonging to the current viewer on the next page load. Test references now carry files and lines (TestLogout auth_test.go:532, TestAnApplicantCanSignOut apply_test.go:201, the two csrf_test.go tests, routes.test.tsx:290). Budget reference: 47415 of 60000 B. The criteria are unchanged and still checkable; criterion 2's e2e/sign-out.spec.ts is the spec described.
