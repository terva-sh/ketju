---
schema: 3
id: TKT-01M3NTSCMN7YYA0M8TMMQX6H6E
title: "Prove Phase 2's exit: approval registration and agent credentials e2e"
type: task
status: done
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
references:
  - ref: pr:194
    path: null
claim: null
archive: null
created_at: 2026-09-29T05:39:18Z
updated_at: 2026-09-30T03:30:20Z
created_by:
  id: agent:claude-code/phase1-run-438e26c1
  name: ""
updated_by:
  id: agent:claude-code/phase2-run-exit
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

- [x] TestPhase2ExitApprovalRegistration, through the real serve and oidctest: a stranger's pre-user session is 403 on the sampled member and admin routes and reaches the apply routes; after a seeded moderator approves, the pre-user cookie still has no user, and the next sign-in gives a member session that posts (201) while the old cookie is refused
- [x] TestPhase2ExitDeniedApplicant: a denied applicant signing in again gets no member session, no answer to their session carries the deny note, and after a deny with block GET /api/v1/apply/form is 403
- [x] TestPhase2ExitSuspendedMember: a suspended member's cookie loses access on the next request, signing in shows account unavailable with the reason and sets no session cookie, and after unsuspend signing in gives a member session
- [x] TestPhase2ExitAgentCredential: an admin provisions an agent with a credential; its bearer post is 201, sets no cookie, and is served with machine_authored true and author.kind agent; after revocation the same request is 401 and log_length is unchanged
- [x] e2e/phase2-exit.spec.ts at 390 px and 1440 px: a stranger applies, sees pending, a seeded moderator approves from /admin/queue, the stranger sees approved and signs in as a member, with axe reporting no violations on the form, status and queue screens
- [x] TestOneDoorIntoSessions passes with its caller lists unchanged, and the PR quotes the CI step log lines showing TestPhase2Exit and phase2-exit.spec.ts ran and passed on the head

## Implementation plan

Groomed 2026-09-29 for the Phase 2 autonomous run (TKT-01M3M9RR26), against main at 5fe97ee. Revised 2026-09-30 on claim, against main at ffddc6b, where every dependency is done.

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

### Decided since grooming, and binding here

- **An approved applicant's pre-user session retires** (maintainer decision 2026-09-29, TKT-01M3Q2V25E, option 1). `LookupSession` stops resolving it once the identity has a user, and the session middleware clears the cookie. So after approval the pre-user cookie is signed out: `/me` has no user and no `pre_user`, and `GET /api/v1/apply/status` is 401. The plan's "the status says approved" and "the stranger's status page, reloaded, says approved" are replaced by that: the status page shows its signed-out state, which points at signing in, and the decision waits as the `application_decided` notification (TKT-01M29CP2N0) once they sign in as a member. Criteria 1 and 5 are read that way.

### Engineering decisions, with the alternative that lost

- **The Go tests run the real `serve`**, in `cmd/ketju`, through `start()`. The exit claim is about the assembled server: middleware order, the pre-user gate, the admin gates, the bearer middleware and the session chokepoint together. Lost: a test in `internal/web/api` over a hand-built `web.New`, which can pass while `serve` wires something differently.
- **One serve per test**, not one for all four. Each test then runs alone under `-run`, and none depends on what an earlier one left. Lost: one server with ordered subtests, which the groomed plan named; a failure in the first would have cascaded into the rest, and nothing asserted needs shared state.
- **Reuse `oidctest.IdP`, with `Users` set once** before `serve` starts, and each sign-in choosing its subject with `login_as`, as the provider's own sign-in page links do. Lost: setting `idp.Subject` between sign-ins, which writes a field the provider's handler goroutine reads.
- **Cookies are carried by hand.** An `actor` keeps cookies by name and adds them to each request, because `net/http/cookiejar` withholds `Secure` cookies such as `__Host-ketju_session` from the plain-HTTP loopback listener. It drops a cookie the server clears, and `holding` makes an actor with a cookie value the first has since replaced, for "the old cookie is refused".
- **Staff and members are seeded before `start`**, as `e2e.go` seeds: a user and an identity per subject through dbq, and the role through `perm.ChangeRole` (which promotes to admin through `promote`), so the rows are audited. Lost: `bootstrap.admin_subjects`, which ties the test to how bootstrap meets approval mode.
- **No limiter clock seam in `serve`.** The groomed plan allowed `serveOpts.limiterNow` if the `/auth/*` limiter was wired. It is, but with one serve per test no test signs in more than five times, which is the burst of ten requests exactly. Lost: the seam, a production-code change the tests do not need.
- **The moderator writes the handle.** The fake provider sends no `preferred_username` or `email`, so `suggested_handle` is empty for a stranger; the moderator types one, as the review screen allows. Lost: global `idp.Claims`, which would give every subject the same claims.
- **One sample of member routes, not a walk.** TKT-01M29CKRG9's `TestPreUserRoutesAreExactlyTheAllowlist` walks every route.
- **The Playwright spec waits out a 429 on `/auth/*`.** The setup project spends eight of the ten `/auth/*` requests one address may make in a burst, and the stranger's two sign-ins cost five more. Each navigation through `/auth/*` waits as the 429's `Retry-After` says and loads the page again, and records the wait as a test annotation. Lost: a fixed sleep, which waits whether or not it is needed; and a clock seam in the e2e build, which would stop the suite exercising the limiter as deployed.
- **A CI step names the Go exit tests.** `go test -race ./...` prints one line per passing package and names no test, so criterion 6 cannot be read from it. A new step in the Build and Test job runs `go test -race ./cmd/ketju/ -run '^TestPhase2Exit' -count=1 -v` (docs/12 "Changing CI" allows adding a step).

### Approach

1. `cmd/ketju/flow_test.go`: `actor` (`do`, `keep`, `session`, `me`, and `bodies`, every answer it was given), `holding`, `signIn(t, a, idp, subject)`, and `seedPeople(t, data, issuer, people...)`, which also writes the public forum `general`.
2. `cmd/ketju/phase2_exit_test.go`:
   - `TestPhase2ExitApprovalRegistration`: mona (moderator) signs in and starts a thread. A stranger signs in: 303 to `/apply`, `/me` has no user and `pre_user.provider` `fake`. Refused, each 403: starting a thread, replying, reading the thread, `/forums`, the mention lookup, a draft, `/admin/applications`, `/admin/queue`; `/f/general` is 303 to `/apply/status`. The form is 200, applying 201 pending, the status pending. mona finds the one pending application in `/admin/queue`, reads it, and approves it as `newcomer`. The pre-user cookie is then signed out (`/me` no user and no pre-user; status 401). The stranger signs in again: 303 to `/`, a new session, `/me` names `newcomer` as a member with 1 notification; their reply is 201 and the thread page shows it under `@newcomer`. The old pre-user cookie's reply is 401 and its `/me` is signed out.
   - `TestPhase2ExitDeniedApplicant`: stranger2 applies; mona denies with `note-7f3a`, no block. The status says denied with no sign-in. Signing in again is 303 to `/apply/status` with a pre-user session; `/me` has no user; member and admin reads are 403; no answer the applicant was given contains `note-7f3a`. stranger3 applies and is denied with block: `/apply/form` is 403 in that session, and again after signing in.
   - `TestPhase2ExitSuspendedMember`: sam (member) posts, mona suspends sam with a reason. sam's old cookie's reply is 401, `/me` signed out. Signing in answers 403 "Account unavailable" with "Reason: …", and sets no session cookie. After unsuspend, sam signs in and posts.
   - `TestPhase2ExitAgentCredential`: ada (admin) signs in, starts a thread, and creates agent `helperbot` with a credential. The bearer reply is 201 and sets no cookie; the thread page serves it with `machine_authored` true and `author.kind` agent. After `DELETE …/credential` (204) the same reply is 401, sets no cookie, and `log_length` is unchanged.
3. `cmd/ketju/e2e.go`: `seedStrangers`, `stranger-phone` and `stranger-desktop`, added to `idp.Users` unseeded. mona and ada are already seeded there (TKT-01M3NRX2NN, TKT-01M29CP2P1), and so is the registration form (migration 0011).
4. `web/client/e2e/phase2-exit.spec.ts`, in both projects: the stranger signs in, lands on `/apply` (axe), applies, and sees pending on `/apply/status` (axe). mona, in a second context at the same width from `stateFile('mona')`, finds the application's id through the admin API by the name answered, opens `/admin/queue`, opens the item, writes the handle (axe with the decision open), approves, and sees the notice and the item gone (axe). The stranger's status page, reloaded, is signed out and offers Sign in (axe). Following it signs them in as the member: `/me` names the handle, role member, 1 notification, and the Alerts badge shows it.
5. `.forgejo/workflows/ci.yml`: the step above. The PR quotes the step log lines for `TestPhase2Exit` and `phase2-exit.spec.ts` on the head.

### Tests

The tests above are the deliverable. Beside them:
- `TestOneDoorIntoSessions` passes with its caller lists unchanged: this ticket adds no session path and no production code.
- Mutations, each caught: opening the pre-user gate (Approval and Denied fail); a credential revoke that writes nothing (Agent fails on the 201 and on `log_length`); suspension that deletes no sessions (Suspended fails: the cookie is not cleared); a denied status carrying the note text (Denied fails).

### Out of scope

- Every feature under test: TKT-01M29CKRG9 (pre-user sessions), TKT-01M29CP2MG (review API), TKT-01M29CP2NG (apply and status screens), TKT-01M29CRJ4E (queue), TKT-01M29CP2QK (suspension), TKT-01M29P9RSW (agents), all dependencies. A failure here goes back to the owning ticket as a bug.
- The exhaustive route walk for pre-users: TKT-01M29CKRG9's own test.
- `auto_approve` providers: TKT-01M29CKRE9 tests them. The exit criterion is about strangers.

## Notes

**agent:claude-code/phase2-run-exit** at 2026-09-30T02:39:00Z

Built on main at ffddc6b. Four Go tests over the real serve (cmd/ketju/phase2_exit_test.go, helpers in flow_test.go), one Playwright spec at both widths (web/client/e2e/phase2-exit.spec.ts), two subjects added to the e2e seed, and one CI step. No production code changed.

How criteria 1 and 5 are read. The maintainer decision of 2026-09-29 on TKT-01M3Q2V25E (option 1) retires an approved applicant's pre-user session, and it postdates this ticket's grooming. This ticket's plan said "the status says approved" and "the stranger's status page, reloaded, says approved". Under the decision, the pre-user cookie after approval is signed out:
- `/me` has no user and no `pre_user`;
- `GET /api/v1/apply/status` is 401;
- the status page shows its signed-out state, which points at signing in.

The approval is then seen as the member session and the `application_decided` notification (TKT-01M29CP2N0): `/me` notifications 1, and the Alerts badge shows it. Criterion 1's "the pre-user cookie still has no user" holds as written. Criterion 5's "the stranger sees approved" is read as the signed-out status page and then the notification after signing in, as the decision describes.

Other findings:
- A stranger from the fake provider gets no suggested handle, because it sends no `preferred_username` or `email`. The moderator writes one in both the Go test and the spec. The product works as designed.
- The groomed limiter seam (`serveOpts.limiterNow`) was not added. With one serve per test, no test exceeds the `/auth/*` burst.
- The spec waits out a 429 on `/auth/*` as `Retry-After` says. Run alone, the phone project met one, because setup spends 8 of the 10.

Local evidence:
- `go test -race ./...` is green, TestOneDoorIntoSessions included.
- The four TestPhase2Exit tests pass with `-count=3`.
- The full Playwright suite passes on port 8871: 69 passed, phase2-exit in both projects.

Mutations tried against the tests, each caught:
- opening the pre-user gate: Approval and Denied fail;
- a credential revoke that writes nothing: Agent fails on the 201 and on log_length;
- a suspension that deletes no sessions: Suspended fails;
- a denied status carrying the note's text: Denied fails.

**agent:claude-code/phase2-run-exit** at 2026-09-30T03:10:56Z

PR #194, head db08a7f, base a827aef. CI run 1236 is attempt 1 of 3 under docs/12, and it failed.

- Playwright passed, with both projects' phase2-exit lines in the step log.
- `go test (race)` failed after 18m55s: `panic: test timed out after 10m0s`, `FAIL github.com/terva-sh/ketju/internal/perm 600.093s`. The test running at the time was `TestSyncIdPGroupsInvalidates`.

This PR changes nothing under `internal/perm`. The timeout dump shows that test runnable, inside a SQLite prepare under `Grant`, not blocked. PR runs 1240, 1242 and 1249 and main's push run 1253 were running at the same time, so the runner was contended. PR #195's run 1251, on the same base, passed in 9 minutes.

The follow-up commit that links the PR reruns CI.

**agent:claude-code/phase2-run-exit** at 2026-09-30T03:29:57Z

PR #194, head a9c8af4, base e425323.

Terva review run a6c2e23b-298a-4acc-86d3-1f895b8089da (request `ready-review-a9c8af4`, Actions run 1255) is clean. It has no findings at any severity, so nothing needs a disposition. `terva-review/code` is success on a9c8af4.

CI run 1254 on a9c8af4 is success, attempt 2. I read the step logs:
- "Phase 2 exit tests, by name" shows `--- PASS` for all four TestPhase2Exit tests.
- "Playwright smoke at 390 px and 1440 px" shows `phase2-exit.spec.ts` passing in both the phone and desktop projects, with 69 passed.
- "go test (race)" shows `ok` for `internal/auth/session`, whose TestOneDoorIntoSessions caller lists this PR does not touch.

The lines are quoted on the PR in comment 19728. Criterion 6 is ticked on that. The commit that follows touches only `.tickets/`, and it carries the review.

## Summary

Phase 2's exit is proven end to end in PR #194, on the assembled server and the real client, with no production code changed. The exit criterion is "a club can open registration to strangers safely, and an admin can give an agent an account and a credential".

- `cmd/ketju/phase2_exit_test.go` has four tests over the real `serve` and the fake OpenID provider, one server each:
  - approval registration: the pre-user session is refused on member and admin routes and reaches the apply routes; after approval its cookie is signed out, the next sign-in is a member session that posts, and the old cookie is refused;
  - a denied applicant, who never sees the moderator's note, and a blocked one;
  - a suspended member, then unsuspended;
  - an agent's credential, then revoked.
- `cmd/ketju/flow_test.go` holds the shared helpers: an actor carrying its own cookies, a sign-in through `oidctest` by `login_as`, and people seeded through `perm.ChangeRole`.
- `web/client/e2e/phase2-exit.spec.ts` runs at 390 px and 1440 px. A stranger applies, mona approves from `/admin/queue`, and the stranger signs in as a member with the approval notification waiting. Axe scans the form, the status page and the queue. A 429 on `/auth/*` is waited out as `Retry-After` says.
- `cmd/ketju/e2e.go` gains `seedStrangers`: `stranger-phone` and `stranger-desktop`.
- `ci.yml` gains the step "Phase 2 exit tests, by name", so the log names the Go tests.

Criteria 1 and 5 are read under the maintainer decision on TKT-01M3Q2V25E, which retires the pre-user session on approval (see the first note). All six criteria are ticked. CI run 1254 and Terva review run a6c2e23b passed on head a9c8af4, and the step log lines are quoted on the PR.
