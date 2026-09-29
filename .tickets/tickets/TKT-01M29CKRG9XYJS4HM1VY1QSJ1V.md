---
schema: 3
id: TKT-01M29CKRG9XYJS4HM1VY1QSJ1V
title: Implement pre-user sessions restricted to the application flow
type: task
status: in-progress
status_reason: null
priority: high
due_on: null
labels:
  - area:identity
  - effort:M
assignees: []
milestone: phase-2
parent: TKT-01M29BKBM38VTYY4Y91018GTH6
origin: null
dependencies:
  - TKT-01M29C5Y3E5SKHFR2SHJ8Y60TP
blocks_on: none
references:
  - ref: design:approval-mode
    path: docs/05-identity-and-accounts.md
claim:
  actor: agent:claude-code/phase2-run-ckrg9
  branch: run/ckrg9-pre-user-sessions
  worktree: /home/sothr/workspace/git.local.sothr.com/terva-sh/ketju/.claude/worktrees/agent-a946fd6ede1643fec
  commit: ccc05adb32252b1e51ea6ebb6131a26edbd3ae92
  session: null
  claimed_at: 2026-09-29T15:14:52Z
  expires_at: null
archive: null
created_at: 2026-09-11T23:24:59Z
updated_at: 2026-09-29T15:14:55Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase2-run-ckrg9
  name: ""
extensions: {}
---

## Description

In approval mode an authenticated visitor with no user gets a session of kind `pre_user` carrying an identity_id and no user_id. It reaches the application form, the status page, and logout. Nothing else.

The allowlist has to be positive rather than a set of exclusions. A new endpoint added next year must be closed to a pre-user session by default, because the failure mode is a pending applicant posting in a forum.

## Acceptance criteria

- [ ] A pre-user session is refused on every route outside web.PreUserRoutes, by a gate in the middleware chain after session resolution: an API path answers 403 forbidden and any other page answers 303 to /apply/status
- [ ] TestPreUserRoutesAreExactlyTheAllowlist walks every route in api.Routes and auth.Routes plus the SPA, /blobs, /events and /assets paths, and shows that a pre-user is accepted only by GET /api/v1/me, the sign-in routes, POST /auth/logout, /assets, and the /apply and /apply/status pages, and that each allowlist entry names a registered route
- [ ] No query can change a session's kind or user_id (chokepoint scan); once the identity has a user, its pre-user cookie is refused at lookup; and for a pending, suspended or deactivated user the next sign-in writes no member session (TestAPreUserCannotBecomeAMember)
- [ ] In approval mode, a new identity from a provider without auto_approve gets an identity row with user_id NULL and a pre-user session, written in one transaction by session.IssuePreUserWith, and the callback answers 303 to /apply; a known applicant identity gets 303 to /apply/status
- [ ] chokepoint_test.go lists the callers of every session issuer (issuerCallers) and the insert callers exactly, and the lists match the code
- [ ] With pre_user set in /me, the client renders the apply-status screen on every route except /apply (routes.test.tsx), and the committed build is regenerated
- [ ] A pre-user request for a thread page is refused before the response cache is consulted (TestAPreUserNeverReachesTheResponseCache)

## Implementation plan

Groomed 2026-09-29 for Phase 2 (TKT-01M3M9RR26) against main at 7af2c5a.

### Settled in grooming

- A pre-user session carries an identity and no user, and "may only reach the application form, the application status page, and logout" (docs/05 "mode = approval" step 1; docs/04 "sessions": "accepted by the application endpoints alone"). A pre-user session is therefore refused on public reads too, even though an anonymous visitor may read public forums (docs/10 item 11). An applicant who wants to read as a guest signs out.
- The allowlist is positive. A route added later is closed to a pre-user session until someone adds it to the list (ticket description).
- `GET /api/v1/me` answers a pre-user session with `user: null` plus `pre_user: {provider}`, and a null user stays the only signed-out signal (docs/07 "Resources", which supersedes the 2026-09-24 note from TKT-01M29CA99R). `internal/service/users/me.go` and `dto.PreUser` already implement this. The golden file is `internal/dto/testdata/golden/me.pre_user.json`.
- Revisiting the site with a pending identity shows the status page (docs/05 step 4).
- A pre-user session never becomes a member session in place. The chokepoint scan already fails the build on any query that sets `sessions.kind`, `user_id` or `identity_id` (`internal/auth/session/chokepoint_test.go`, `scanSQL`/`badUpdates`). After approval the applicant signs in again, and `session.IssueUser` checks their status (docs/09, the approval-bypass row; TKT-01M29C7ZZR).
- Pre-user sessions expire by the same idle and hard rules as any other session, and the sweeper removes them (`internal/auth/session/session.go` `Sweep`).
- A pre-user session is refused before any thread endpoint runs, so it never reaches the response cache. It adds no viewer class to the cache key (docs/07 "The response cache": `anon` for anyone who is not an active member).
- The per-identity rate limit on application submission belongs to the approval-mode epic, together with the form it protects (docs/05 "Sessions and CSRF"). It is not this ticket's.

### Approach

1. **Issuing.** In `internal/auth/session/issue.go`, add `IssuePreUserWith(ctx, meta, create func(ctx, *dbq.Queries) (int64, error))`, the pre-user twin of `IssueUserWith`. `create` writes the identity in the transaction that inserts the session. The holder check follows: an identity that has a user is refused with `ErrHasUser`. `IssuePreUser` becomes a one-line call to `IssuePreUserWith`, as `IssueUser` does to `IssueUserWith`. In `chokepoint_test.go`, `insertCallers` becomes `{issue.go:IssuePreUserWith, issue.go:IssueUserWith}`. Alternative: a separate `CreateIdentity` write before `IssuePreUser`. It lost because a failed session insert would leave an orphaned identity row, and because the registration-mode check would then sit in a different transaction from the write (review 1145).
2. **Issuer-caller list.** In `chokepoint_test.go`, add `issuerCallers`: every non-test function outside `internal/auth/session` that calls `IssueUser`, `IssueUserWith`, `IssuePreUser` or `IssuePreUserWith`, compared exactly, the same way `doorCallers` is. It starts as `identity/login.go:Login` (IssueUser, IssuePreUser), `identity/register.go:Register` (IssueUserWith), and `identity/apply.go:BeginApplication` (IssuePreUserWith). Every later ticket that adds a sign-in path shows up in this list in its diff.
3. **The applicant path.** Add `internal/service/identity/apply.go` with `BeginApplication(ctx, c Claims, meta, replaces) (Result, error)`. It runs `IssuePreUserWith`, and the create func does four things inside the transaction. It reads `settings.RegistrationMode.In` and refuses with `ErrRegistrationClosed` unless the mode is `approval`. It refuses with `ErrAlreadyRegistered` an identity that already exists, which covers a race with another tab. It writes `CreateIdentity` with `user_id` NULL. It writes `RecordIdentityLogin`. Then `BeginApplication` revokes `replaces`, following the fixation rule `settle` uses, and does not run bootstrap or group mapping, since there is no user. Add `Outcome` `Applying`, and give `Result` its token and session.
4. **Login's Applicant outcome.** `Login` currently returns `Applicant` with no session. Change it to call `s.Sessions.IssuePreUser(ctx, ident.ID, meta)` and return `Applicant` with the token. It revokes `replaces` as step 3 does. The `Sessions` interface in `login.go` gains `IssuePreUser` and `IssuePreUserWith`.
5. **The callback** (`internal/web/auth/auth.go`, `register.go`). `beginRegistration` with registration closed is currently a 501 `pageRegistration`. In approval mode, and for a provider that does not auto-approve, it becomes `BeginApplication`: set the session cookie and answer 303 to `/apply`. The auto_approve branch belongs to TKT-01M29CKRE9. The `identity.Applicant` case becomes: set the cookie, answer 303 to `/apply/status`. `TestTheBranchesStillToCome` loses its registration and applicant subtests, and they move into the tests below.
6. **Ending a pre-user session once the identity has a user.** `LookupSession` (`internal/store/db/queries/identity.sql`) gains `LEFT JOIN identities i ON i.id = s.identity_id` and `AND (s.kind = 'user' OR i.user_id IS NULL)`. A pre-user cookie then stops resolving at the moment approval links the identity, even if the approval code forgets to delete it. Add the query `DeletePreUserSessions :exec` (`DELETE FROM sessions WHERE kind = 'pre_user' AND identity_id = ?`) and `Store.RevokePreUser(ctx, identityID)` for moderator approval (TKT-01M29CP2MG) to call in its own transaction. Regenerate dbq with sqlc.
7. **The allowlist gate.** Add `internal/web/preuser.go` with `PreUserGate(allow []Route) func(http.Handler) http.Handler`. It runs in `chain` between session resolution (6) and CSRF (7), and the chain comment and `TestTheMiddlewareRunsInTheDocumentedOrder` gain the position. For a viewer with `PreUser` set, a request that matches no entry is refused. A path under `/api/` gets `problem.CodeForbidden`. Any other GET or HEAD, which is an SPA page, gets 303 to `/apply/status`. Anything else gets 403 forbidden. The list lives in `web.PreUserRoutes`, next to the gate, as exact method-and-pattern pairs:
   - `GET /api/v1/me`
   - `GET /auth/signin`, `GET /auth/{provider}/start`, `GET /auth/{provider}/callback`, `POST /auth/logout`
   - `GET /assets/` (subtree)
   - `GET /apply`, `GET /apply/status` (the SPA pages)

   TKT-01M29CP2KZ adds `GET /api/v1/apply/form`, `POST /api/v1/apply`, `GET /api/v1/apply/status` and `POST /api/v1/apply/withdraw` when it registers them. `/auth/register`, `/blobs/`, `/events` and every other API route stay closed. Alternative: keep `member.Require` in each handler, the exclusion model in use today. It lost because a new read handler is open to a pre-user by default, which is the failure this ticket exists to close.
8. **A route table the test can walk.** `api.New` registers from an exported `api.Routes(h)` table (`[]web.Route{Method, Pattern, Handler}`) rather than inline `HandleFunc` calls. `auth.New` does the same with `auth.Routes`. Behaviour does not change. The table exists so that a test can enumerate the routes, which a `ServeMux` cannot.
9. **Client.** In `web/client/src/app.tsx`, when the viewer's `pre_user` is non-null, every route except `/apply` renders the `apply-status` screen. The two screens stay the placeholders in `routes.tsx` until TKT-01M29CP2NG builds them. The header shows "Sign out", which posts to `/auth/logout`, and no forum navigation. Run `go generate ./internal/web/spa` and commit the built client.
10. **Docs.** docs/05 "mode = approval" step 1 names the gate and its position in the chain. docs/07 "URL scheme" says that a pre-user session is sent to `/apply/status` from any other page. docs/02's middleware list gains the step.

### Tests

- `internal/web/preuser_test.go` `TestPreUserRoutesAreExactlyTheAllowlist`. It builds the real server (`web.New` with the real `api.New` and `auth.New`) and a pre-user cookie from `sessiontest.PreUser`. It walks `api.Routes` and `auth.Routes` plus one SPA path and one `/blobs/`, `/events` and `/assets/` path each, with path values filled in. Every route outside `PreUserRoutes` answers 403 `forbidden` for an API path or 303 to `/apply/status` for a page. Every route on the list answers something other than 403. It also asserts that each `PreUserRoutes` entry matches a registered route, so a typo cannot open or close nothing.
- `TestANewRouteIsClosedToAPreUserByDefault`. It registers a throwaway route through a test-only table and shows that a pre-user is refused there without any code change to the gate.
- `internal/auth/session` `TestAPreUserCannotBecomeAMember`. For each of `pending`, `suspended` and `deactivated`: issue a pre-user session, attach the identity to a user in that status (the step approval would take), and assert three things. `Lookup` of the pre-user cookie is `ErrNoSession`. `IssueUser` for the identity is a `*NotActiveError`. No `kind = 'user'` row exists. A control subtest with `active` shows that the next sign-in does get a member session and the old pre-user cookie is still refused.
- `internal/auth/session` `TestIssuePreUserWithIsAtomic`. An injected trigger on `sessions` insert leaves no identity row. An identity that already has a user is `ErrHasUser` and writes nothing.
- `chokepoint_test.go`: `TestOneDoorIntoSessions` passes with the new `insertCallers`, and `TestTheIssuersAreKnown` checks `issuerCallers`. The fixture tree under `testdata/chokepoint` gains an extra caller of `IssuePreUserWith`, and the scan must report it.
- `internal/web/auth` `TestApprovalModeGivesAnApplicantAPreUserSession`. In approval mode with a provider that does not auto-approve, the callback writes an identity with `user_id` NULL, sets a cookie that resolves to `PreUser`, answers 303 `/apply`, and creates no `users` row. A second sign-in with the same identity answers 303 `/apply/status`.
- `TestAnApplicantSignInEndsTheSessionItReplaces`, the fixation case for both applicant paths.
- `internal/web/api` `TestAPreUserNeverReachesTheResponseCache`. A pre-user GET of `/api/v1/threads/{id}` is 403, and `respcache` `Stats` shows no lookup and no insert.
- `internal/web/api` `me_test.go` keeps the `me.pre_user.json` golden. Add a check that `/me` is reachable through the gate.
- Client: in `routes.test.tsx`, with `pre_user` set, `/`, `/f/general` and `/t/abc/x` render `apply-status`, and `/apply` renders `apply`.

### Out of scope

- The `/apply` API endpoints, the 24-hour withdraw rule, and blocked identities: TKT-01M29CP2KZ. It adds its routes to `PreUserRoutes`.
- The applications and forms schema: TKT-01M29CKRDQ.
- Approving an application, and calling `RevokePreUser` in the same transaction: TKT-01M29CP2MG.
- The real apply and status screens: TKT-01M29CP2NG.
- The auto_approve branch of `beginRegistration`: TKT-01M29CKRE9.
- The per-identity limit on application submission: approval-mode epic TKT-01M29BKBM3.

## Notes

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T11:59:38Z

From TKT-01M29CA99R (Implement GET /me): GET /me, and the shell's bootstrap block, return the anonymous shape (user null, zero counts) for a pre-user session, because dto.Me makes a nil user the only signed-out signal and no pre-user shape is documented. The viewer does carry the distinction (domain.Viewer.PreUser and IdentityID, set by internal/auth/session). If the approval screens need /me to say 'signed in as an applicant', decide the field in docs/07 and dto.Me; CA99R's AC1 stays unticked on exactly that clause until then.
