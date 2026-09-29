---
schema: 3
id: TKT-01M3NTSCMN7YYA0M8TMMQX6H6E
title: "Prove Phase 2's exit: approval registration and agent credentials e2e"
type: task
status: draft
status_reason: null
priority: high
due_on: null
labels: []
assignees: []
milestone: phase-2
parent: null
origin: null
dependencies:
  - TKT-01M29CKRG9XYJS4HM1VY1QSJ1V
  - TKT-01M29CP2MGC6FD35PJHR2Y2WNA
  - TKT-01M29CP2NGWH32C6M718QHB30X
  - TKT-01M29CRJ4E4AZ079KEHVGJ01DS
  - TKT-01M29CP2QKKZSK4PDB40EJHC3P
  - TKT-01M29P9RSW21SCES335WERTBSF
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-29T05:39:18Z
updated_at: 2026-09-29T07:22:45Z
created_by:
  id: agent:claude-code/phase1-run-438e26c1
  name: ""
updated_by:
  id: agent:claude-code/phase2-groom
  name: ""
extensions: {}
---

## Description

Phase 2's exit criterion (docs/09): "a club can open registration to strangers safely, and an admin can give an agent an account and a credential". This ticket proves it end to end, as TKT-01M29CGBY1 proved Phase 1.

### What the tests must show

- **Approval mode, through the real HTTP stack**, in a Go integration test with a fake OIDC provider:
  - a stranger signs in and gets a pre-user session;
  - that session is refused on every member route, and reaches the apply routes;
  - the stranger submits the form;
  - a moderator approves the application;
  - the applicant signs in again and gets a member session, and can post.
- **A denied applicant** cannot get a member session.
- **A suspended member** loses their session and cannot sign in, and can sign in again after the suspension is lifted.
- **An agent account**: an admin provisions it and issues a credential; the agent posts with its bearer token, and the post is marked machine-authored; after the admin revokes the credential, the agent is refused.
- **Playwright**, at 390 px and 1440 px: the apply screen, the status screen, and the moderator queue approve flow, with axe run on each.

## Acceptance criteria

- [ ] TestPhase2ExitApprovalRegistration, through the real serve and oidctest: a stranger's pre-user session is 403 on the sampled member and admin routes and reaches the apply routes; after a seeded moderator approves, the pre-user cookie still has no user, and the next sign-in gives a member session that posts (201) while the old cookie is refused
- [ ] TestPhase2ExitDeniedApplicant: a denied applicant signing in again gets no member session, no answer to their session carries the deny note, and after a deny with block GET /api/v1/apply/form is 403
- [ ] TestPhase2ExitSuspendedMember: a suspended member's cookie loses access on the next request, signing in shows account unavailable with the reason and sets no session cookie, and after unsuspend signing in gives a member session
- [ ] TestPhase2ExitAgentCredential: an admin provisions an agent with a credential; its bearer post is 201, sets no cookie, and is served with machine_authored true and author.kind agent; after revocation the same request is 401 and log_length is unchanged
- [ ] e2e/phase2-exit.spec.ts at 390 px and 1440 px: a stranger applies, sees pending, a seeded moderator approves from /admin/queue, the stranger sees approved and signs in as a member, with axe reporting no violations on the form, status and queue screens
- [ ] TestOneDoorIntoSessions passes with its caller lists unchanged, and the PR quotes the CI step log lines showing TestPhase2Exit and phase2-exit.spec.ts ran and passed on the head

## Implementation plan

Groomed 2026-09-29 for the Phase 2 autonomous run (TKT-01M3M9RR26), against main at 5fe97ee.

What exists there:
- **A fake OpenID provider**, `internal/auth/oidc/oidctest` (`IdP`, `New(t)`, `Start()`, `Authorize(t, authURL)`, `Subject`, `Users`). It really signs its tokens. `cmd/ketju/serve_signin_test.go` already drives a whole sign-in through the real `serve`: `writeConfig`, `start(t, path, serveOpts{idpClient: idp.Client(), getenv: siteURL})`, `/auth/fake/start`, `idp.Authorize`, and the callback with the flow cookie added by hand. No new fake provider is needed.
- **The e2e build**, `cmd/ketju/e2e.go` (tag `e2e`), serves Playwright: a fresh data directory, a seeded forum and thread, alice and carol, and `oidctest.Start()` with a sign-in page listing `idp.Users`. `web/client/e2e/session.ts` signs in through it and keeps sessions in `stateFile(who)`.
- `registration.mode` defaults to `approval` (`serve_signin_test.go` relies on it).

### Settled in grooming

- **The exit criterion** (docs/09): "a club can open registration to strangers safely, and an admin can give an agent an account and a credential". docs/13 makes this ticket the gate: the run is done when it is `done`.
- **Approval mode** (docs/05 "mode = approval"): a stranger gets a pre-user session bound to the identity, which reaches only the application form, the status page and logout. The allowlist is positive (TKT-01M29CKRG9).
- **Approval issues no session.** The applicant's next sign-in makes the member session through `session.IssueUser`, which checks status (TKT-01M29CP2MG). The session chokepoint's caller lists do not change.
- **A denied applicant sees the status only, never the moderator's note** (maintainer decision 2026-09-29, docs/08).
- **Suspension** (docs/05 "Account lifecycle", decided 2026-09-29): moderators suspend members; sessions are revoked at once; signing in shows "account unavailable" with the reason and writes no session; unsuspending lets the member sign in again.
- **Agents** (docs/05 "Agent accounts", docs/07): an admin provisions the account and issues a bearer credential, stored hashed, which makes no session and sets no cookie. The server sets the machine-authored flag from `users.kind`. Revoking the credential stops the agent. Its bucket allows a burst of 20 and refills one token every 30 s (decided 2026-09-29, docs/08), which a single post does not reach.
- **The Playwright suite runs at 390 px and 1440 px with axe**, as TKT-01M29CGBY1 proved Phase 1.
- **A green run summary is not evidence** (AGENTS.md): the step log must show these tests ran.

Engineering decisions taken here, with the alternative that lost:
- **The Go test runs the real `serve`**, in `cmd/ketju`, through `start()`. The exit claim is about the assembled server: middleware order, the pre-user gate, the admin gates, the bearer middleware and the session chokepoint together. Lost: a test in `internal/web/api` over a hand-built `web.New`, which can pass while `serve` wires something differently.
- **Reuse `oidctest.IdP`.** Each actor is its own subject, set on `idp.Subject` before that actor's sign-in; the subtests run in order on one server. Lost: a second fake, or `Users` with a scripted sign-in page, which only a browser needs.
- **Cookies are carried by hand.** An `actor` keeps cookies by name and adds them to each request, because `net/http/cookiejar` withholds `Secure` cookies such as `__Host-ketju_session` from the plain-HTTP loopback listener. `serve_signin_test.go` already adds the flow cookie by hand.
- **Staff are seeded before `start`**, as `e2e.go` seeds: a user and an identity per subject through dbq, and the role through `perm.Promote`, so the rows are audited. Lost: `bootstrap.admin_subjects`, which ties the test to how bootstrap meets approval mode, a separate question.
- **One sample of member routes, not a walk.** TKT-01M29CKRG9's `TestPreUserRoutesAreExactlyTheAllowlist` walks every route. The exit test checks a sample through the assembled server: creating a thread, replying, reading a thread, the mention lookup, a draft, and an admin route.

### Approach

1. `cmd/ketju/flow_test.go`, helpers for this test and later ones:
   - `type actor struct{ name string; cookies map[string]*http.Cookie; bearer string }` and `(a *actor) do(t, method, path string, body any) *http.Response`. It sends JSON with `Content-Type: application/json`, no redirects followed, and records every `Set-Cookie`, deleting a cookie the server clears.
   - `signIn(t, r *running, idp *oidctest.IdP, a *actor, subject string) *http.Response`: `/auth/fake/start`, `idp.Authorize`, then the callback with the flow cookie. It returns the callback's answer.
   - `seedStaff(t, data, subject, handle, role string)`: before `start`, opens the database, migrates, and writes the user and identity; `perm.Promote` sets the role.
   - If the `/auth/*` limiter (docs/08: burst 10 per address) is wired into `serve` by then, add `serveOpts.limiterNow func() time.Time` so the test moves the limiter's clock instead of sleeping. The test signs in about eight times.
2. `cmd/ketju/phase2_exit_test.go`, one server for all subtests: config with `registration.mode` approval, provider `fake`, and `mona` (moderator) and `ada` (admin) seeded.
   - `TestPhase2ExitApprovalRegistration`:
     - `stranger` signs in. The callback answers 303 to `/apply` and sets the session cookie. `GET /api/v1/me` has `user: null` and `pre_user.provider: "fake"`.
     - Refused, each 403: `POST /api/v1/forums/general/threads`, `POST /api/v1/threads/{id}/posts`, `GET /api/v1/threads/{id}`, `GET /api/v1/users?prefix=a`, `PUT /api/v1/drafts/{id}`, `GET /api/v1/admin/applications`. A page such as `/f/general` answers 303 to `/apply/status`.
     - Reached: `GET /api/v1/apply/form` 200; `POST /api/v1/apply` with that version and valid answers, 201; `GET /api/v1/apply/status` shows pending.
     - `mona` signs in, finds the application in `GET /api/v1/admin/queue`, and approves it with `POST /api/v1/admin/applications/{id}/approve` and the suggested handle.
     - The stranger's pre-user cookie still answers `/me` with `user: null`, and the status says approved.
     - The stranger signs in again: a member session; `/me` names the handle; `POST /api/v1/threads/{id}/posts` is 201, and the thread page shows the post under that handle. The old pre-user cookie is refused (`/me` anonymous).
   - `TestPhase2ExitDeniedApplicant`: `stranger2` applies and `mona` denies with the note `note-7f3a` and no block. Signing in again gives a pre-user session, never a member one (`/me` `user: null`). The status says denied, and no answer to that session contains `note-7f3a`. A deny with block, for `stranger3`, makes `GET /api/v1/apply/form` 403.
   - `TestPhase2ExitSuspendedMember`: `mona` suspends the member from the first test with a reason (`POST /api/v1/admin/users/{handle}/suspend`). Their cookie's next `/me` is anonymous and a post is 401. Signing in answers the "account unavailable" page with the reason and sets no session cookie. After `…/unsuspend`, signing in gives a member session.
   - `TestPhase2ExitAgentCredential`: `ada` posts `POST /api/v1/admin/users` with `{kind: "agent", handle: "helperbot", display_name, issue_credential: true}` and gets a token. With `Authorization: Bearer <token>` and no cookie, `POST /api/v1/threads/{id}/posts` is 201 and sets no cookie. `GET /api/v1/threads/{id}` shows that post with `machine_authored: true` and `author.kind: "agent"`. After `DELETE /api/v1/admin/users/helperbot/credential`, the same post is 401, and the thread's `log_length` is unchanged.
3. `cmd/ketju/e2e.go`: add `mona` (moderator), if TKT-01M3NRX2NN has not, and the subjects `stranger-phone` and `stranger-desktop` to `idp.Users`, unseeded, so each Playwright project has its own applicant. The active registration form comes from TKT-01M29CP2KE's seed; if that ticket seeds none, `e2e.go` writes one version.
4. `web/client/e2e/phase2-exit.spec.ts`, run in both projects:
   - The project's stranger signs in through the fake provider and lands on `/apply`. Axe on the form. Submitting lands on `/apply/status`, pending. Axe.
   - A second context from `stateFile('mona')` opens `/admin/queue`, finds the application, and approves it. Axe on the queue.
   - The stranger's status page, reloaded, says approved and offers Sign in. Axe. Signing in lands on `/` as a member, and `/api/v1/me` names the handle.
   - `session.setup.ts` signs `mona` in once. The spec leaves the member it made, whose handle differs per project.
5. CI: the tests run in the required `ci / Build and Test (pull_request)` job with the rest of `go test ./...` and Playwright. The PR's note quotes the step log lines for `TestPhase2Exit` and `phase2-exit.spec.ts`.

### Tests

The tests above are the deliverable. Beside them:
- `TestOneDoorIntoSessions` passes with its caller lists unchanged: this ticket adds no session path.
- The helpers get no tests of their own; the exit tests fail loudly if a helper is wrong.

### Out of scope

- Every feature under test: TKT-01M29CKRG9 (pre-user sessions), TKT-01M29CP2MG (review API), TKT-01M29CP2NG (apply and status screens), TKT-01M29CRJ4E (queue), TKT-01M29CP2QK (suspension), TKT-01M29P9RSW (agents), all dependencies. A failure here goes back to the owning ticket as a bug.
- The exhaustive route walk for pre-users: TKT-01M29CKRG9's own test.
- `auto_approve` providers: TKT-01M29CKRE9 tests them. The exit criterion is about strangers.
