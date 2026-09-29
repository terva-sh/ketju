---
schema: 3
id: TKT-01M29CKRG9XYJS4HM1VY1QSJ1V
title: Implement pre-user sessions restricted to the application flow
type: task
status: done
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
  - ref: pr:149
    path: null
claim: null
archive: null
created_at: 2026-09-11T23:24:59Z
updated_at: 2026-09-29T16:01:41Z
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

- [x] A pre-user session is refused on every route outside web.PreUserRoutes, by a gate in the middleware chain after session resolution: an API path answers 403 forbidden and any other page answers 303 to /apply/status
- [x] TestPreUserRoutesAreExactlyTheAllowlist walks every route in api.Routes and auth.Routes plus the SPA, /blobs, /events and /assets paths, and shows that a pre-user is accepted only by GET /api/v1/me, the sign-in routes, POST /auth/logout, /assets, and the /apply and /apply/status pages, and that each allowlist entry names a registered route
- [x] No query can change a session's kind or user_id (chokepoint scan); once the identity has a user, its pre-user cookie is refused at lookup; and for a pending, suspended or deactivated user the next sign-in writes no member session (TestAPreUserCannotBecomeAMember)
- [x] In approval mode, a new identity from a provider without auto_approve gets an identity row with user_id NULL and a pre-user session, written in one transaction by session.IssuePreUserWith, and the callback answers 303 to /apply; a known applicant identity gets 303 to /apply/status
- [x] chokepoint_test.go lists the callers of every session issuer (issuerCallers) and the insert callers exactly, and the lists match the code
- [x] With pre_user set in /me, the client renders the apply-status screen on every route except /apply (routes.test.tsx), and the committed build is regenerated
- [x] A pre-user request for a thread page is refused before the response cache is consulted (TestAPreUserNeverReachesTheResponseCache)

## Implementation plan

Groomed 2026-09-29 for Phase 2 (TKT-01M3M9RR26) against main at 7af2c5a. Revised at claim against ccc05ad; the settled items below are carried unchanged.

### Settled in grooming

- A pre-user session carries an identity and no user, and "may only reach the application form, the application status page, and logout" (docs/05 "mode = approval" step 1; docs/04 "sessions": "accepted by the application endpoints alone"). A pre-user session is therefore refused on public reads too, even though an anonymous visitor may read public forums (docs/10 item 11). An applicant who wants to read as a guest signs out.
- The allowlist is positive. A route added later is closed to a pre-user session until someone adds it to the list (ticket description).
- `GET /api/v1/me` answers a pre-user session with `user: null` plus `pre_user: {provider}`, and a null user stays the only signed-out signal (docs/07 "Resources", which supersedes the 2026-09-24 note from TKT-01M29CA99R). `internal/service/users/me.go` and `dto.PreUser` already implement this. The golden file is `internal/dto/testdata/golden/me.pre_user.json`.
- Revisiting the site with a pending identity shows the status page (docs/05 step 4).
- A pre-user session never becomes a member session in place. The chokepoint scan already fails the build on any query that sets `sessions.kind`, `user_id` or `identity_id` (`internal/auth/session/chokepoint_test.go`, `scanSQL`/`badUpdates`). After approval the applicant signs in again, and `session.IssueUser` checks their status (docs/09, the approval-bypass row; TKT-01M29C7ZZR).
- Pre-user sessions expire by the same idle and hard rules as any other session, and the sweeper removes them (`internal/auth/session/session.go` `Sweep`).
- A pre-user session is refused before any thread endpoint runs, so it never reaches the response cache. It adds no viewer class to the cache key (docs/07 "The response cache": `anon` for anyone who is not an active member).
- The per-identity rate limit on application submission belongs to the approval-mode epic, together with the form it protects (docs/05 "Sessions and CSRF"). It is not this ticket's.

### Approach (revised at claim, 2026-09-29, against main at ccc05ad)

The groomed approach held. What changed on reading the code is marked "Revised".

1. **Issuing.** `internal/auth/session/issue.go` gains `IssuePreUserWith(ctx, meta, create)`, the pre-user twin of `IssueUserWith`: `create` writes the identity in the transaction that inserts the session, and the holder check refuses an identity with a user (`ErrHasUser`). `IssuePreUser` is a one-line call to it. `insertCallers` becomes `{issue.go:IssuePreUserWith, issue.go:IssueUserWith}`. Alternative: a separate `CreateIdentity` write before `IssuePreUser`. It lost because a failed session insert would orphan the identity, and the mode check would sit in a different transaction from the write (review 1145).
2. **Issuer-caller list.** `chokepoint_test.go` gains `issuerCallers` and `TestTheIssuersAreKnown`: every non-test selector outside `internal/auth/session` naming one of the four issuers, as `file:func Issuer`, compared exactly. A method value counts as well as a call. Revised: the list names `login.go:applicant IssuePreUser`, because Login's applicant branch is its own function.
3. **The applicant path.** `internal/service/identity/apply.go` `BeginApplication(ctx, c Claims, meta, replaces)`. Inside `IssuePreUserWith` it reads `registration.mode` (refusing `ErrRegistrationClosed` unless `approval`), refuses an existing identity (`ErrAlreadyRegistered`), writes the identity with `user_id` NULL, and records the first sign-in. It ends `replaces` before writing anything (review 1397): the identity it writes would outlive a failure to end the old session afterwards. Revised: the fixation step is factored out of `settle` as `replace`/`abandon`, which a sign-in, a registration and a returning applicant share. The new `Outcome` is `Applying`.
4. **Login's Applicant outcome.** `ErrNoUser` from `IssueUser` now leads to `applicant`, which calls `IssuePreUser`, ends `replaces`, records the sign-in, and returns `Applicant` with the token. The `Sessions` interface gains both pre-user issuers.
5. **The callback.** `beginRegistration` takes the provider. With registration closed and a provider that does not auto-approve, it calls `beginApplication`: cookie, 303 to `/apply`. Its two refusals are races (another tab wrote the identity; registration opened) and answer 409 with the "Sign-in failed, try again" page. An auto_approve provider keeps the 501 page for TKT-01M29CKRE9. The `Applicant` outcome sets the cookie and answers 303 to `/apply/status`. Revised: `pageApplicant` is gone, now unused.
6. **Lookup.** `LookupSession` joins `identities` and requires `s.kind = 'user' OR (s.kind = 'pre_user' AND i.id IS NOT NULL AND i.user_id IS NULL)`. `DeletePreUserSessions` and `Store.RevokePreUser(ctx, identityID)` are there for TKT-01M29CP2MG; its approval transaction can call `q.DeletePreUserSessions` directly. No migration: nothing here needs an index.
7. **The gate.** `internal/web/preuser.go`: `web.Route{Method, Path, Handler}`, `web.PreUserRoutes`, `web.ApplyStatusPath`, and `PreUserGate(allow)`. The gate compiles `allow` into a ServeMux and lets a pre-user through only when that mux names one of `allow`'s own patterns for the request. It runs at chain position 7, between session (6) and CSRF (now 8), and is not an option. Refusals: 403 `forbidden` problem under `/api/`, 303 to `/apply/status` for any other GET or HEAD, 403 `forbidden` otherwise. Revised: the field is `Path` rather than `Pattern`, and `Route.Pattern()` gives the ServeMux form.
8. **Route tables.** `api.build` registers from `h.routes()`, a `[]web.Route`. Admin routes still register through `adminRoute`, which now records its gated handler. `api.Routes(d)` returns both. `auth.New` registers from `auth.Routes(h)`. Behaviour does not change.
9. **Client.** `app.tsx` renders `ApplicantShell` when `viewer.pre_user` is set. It has a wordmark, a "Sign out" form posting to `/auth/logout`, and no forum navigation. It shows the route table's `/apply` screen at `/apply` and the `/apply/status` screen everywhere else. `internal/web/spa/dist` is regenerated.
10. **Docs.** docs/05 "mode = approval" step 1 names the list, the gate and its position. docs/07 "URL scheme" says where a pre-user goes and what the client draws. The docs/02 diagram lists the chain in order, gate included, and its "Read a thread page" flow names the gate.

### Tests

- `internal/web/preuser_test.go` (package `web_test`):
  - `TestPreUserRoutesAreExactlyTheAllowlist` pins `PreUserRoutes` to the list in AC2. It walks every route in `api.Routes` and `auth.Routes`, `/assets/`, `/blobs/`, `/events`, client pages, and unserved API paths, with GET routes probed as HEAD too. On a server built from the same tables with recording handlers, a member reaches every handler; a pre-user reaches exactly the allowlisted ones and is refused elsewhere before any handler runs. On the real server (real `api.New`, `auth.New`, shell and assets), each refusal has the right status and body, and each allowed route is not refused.
  - Its subtest "each entry names a route" checks each entry against the route tables, the `/assets/` group, and the client's route table.
  - `TestANewRouteIsClosedToAPreUserByDefault` uses a throwaway route table.
  - `TestThePreUserGateMatchesMethodsAsTheRouterDoes` covers methods.
  - `TestTrickyPathsReachOnlyTheAllowlist` sends dot segments, escapes, doubled slashes and case, and checks that nothing reaches a handler off the list.
- `TestTheMiddlewareRunsInTheDocumentedOrder` shows the gate stops a pre-user after the session and before CSRF.
- `internal/auth/session`:
  - `TestAPreUserCannotBecomeAMember`: pending, suspended, deactivated, and an active control.
  - `TestIssuePreUserWithIsAtomic`: a trigger on the session insert leaves no identity; an identity with a user gets `ErrHasUser`; a refusal from `create` writes nothing.
  - `TestRevokePreUser`.
  - `TestTheIssuersAreKnown`. The fixture tree gains unlisted issuer callers, a method value among them, which the scan reports. An in-package caller and a test-file caller are exempt.
- `internal/service/identity`: `TestBeginApplicationWritesAnApplicant`, `TestBeginApplicationRefuses`, and `TestRegistrationOpenedJustBeforeTheApplication` (review 1145's interleaving).
- `internal/web/auth`:
  - `TestApprovalModeGivesAnApplicantAPreUserSession`.
  - `TestAnApplicantSignInEndsTheSessionItReplaces` and `TestAnApplicantSignInThatCannotEndTheOldSessionFails`, each for both paths.
  - `TestAPreUserCannotRegister`, `TestAnApplicantCanSignOut`, and the auto_approve case kept at 501.
  - Revised: `TestRegistrationEndsTheSessionItReplaces` now replaces a suspended user's session. A pre-user never reaches `/auth/register`.
- `internal/web/api`:
  - `TestAPreUserNeverReachesTheResponseCache`: 403 on the page, post, replies and history endpoints, and `respcache` stats do not move; a member control shows the counters are live.
  - `TestEveryRouteIsInTheTable`, an AST scan.
  - The existing `me_test.go` pre-user case already runs `/me` through `web.New`, which now always carries the gate.
- `cmd/ketju` `TestServeSignsInThroughAProvider`: a new sign-in under the default approval mode lands on `/apply` with a pre-user cookie that reaches `/me` and is refused `/api/v1/forums`.
- Client `routes.test.tsx`: with `pre_user` set, `/`, `/f/general`, `/t/abc/x` and other addresses render `apply-status`, and `/apply` renders `apply`. The bar has the sign-out form and no navigation.

### Out of scope

- The `/apply` API endpoints, the 24-hour withdraw rule, and blocked identities: TKT-01M29CP2KZ. It adds its routes to `PreUserRoutes` and to `theAllowlist` in `internal/web/preuser_test.go`.
- The applications and forms schema: TKT-01M29CKRDQ.
- Approving an application, and calling `RevokePreUser` (or `q.DeletePreUserSessions`) in the same transaction: TKT-01M29CP2MG.
- The real apply and status screens: TKT-01M29CP2NG.
- The auto_approve branch of `beginRegistration`: TKT-01M29CKRE9.
- The per-identity limit on application submission: approval-mode epic TKT-01M29BKBM3.

## Notes

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T11:59:38Z

From TKT-01M29CA99R (Implement GET /me): GET /me, and the shell's bootstrap block, return the anonymous shape (user null, zero counts) for a pre-user session, because dto.Me makes a nil user the only signed-out signal and no pre-user shape is documented. The viewer does carry the distinction (domain.Viewer.PreUser and IdentityID, set by internal/auth/session). If the approval screens need /me to say 'signed in as an applicant', decide the field in docs/07 and dto.Me; CA99R's AC1 stays unticked on exactly that clause until then.

**agent:claude-code/phase2-run-ckrg9** at 2026-09-29T16:01:32Z

Review 1397 (run e7314907-3189-4a6c-ab83-4a44dde22f85, Actions run 914), head 1a3564deb45e0f1c1b4628c72f9acbc40ab7f0aa, base cffbe5005e73ec08587e4436f1d957c5ca0e21f0. One finding, medium: 'Roll back a new applicant identity if session replacement fails'. Accepted and fixed. BeginApplication ended the replaced session after IssuePreUserWith had committed the identity. If that revoke failed, the new session was ended but the identity stayed, so the retry took the returning-applicant path to /apply/status for an application that never began. BeginApplication now ends the old session before it writes anything, so a failure writes nothing and the retry is still someone new. The alternative, deleting the identity in abandon, lost for two reasons: a second write that can fail as the first did, and a DELETE on identities outside the transaction that made the row. TestAnApplicantSignInThatCannotEndTheOldSessionFails now also asserts that no identity is left and that the retry lands on /apply. Put back to the old order, the test fails on both assertions.

## Summary

Approval mode gives someone new an identity with no user and a pre-user session. The new session.IssuePreUserWith writes both in one transaction, through identity.BeginApplication, and the callback answers 303 to /apply. An applicant signing in again gets a fresh pre-user session and 303 to /apply/status. Both paths end the session they replace. An auto_approve provider under approval mode keeps the 501 page for TKT-01M29CKRE9. web.PreUserGate, chain position 7 between session and CSRF, is not optional. It admits a pre-user only to the positive web.PreUserRoutes: GET /api/v1/me, the sign-in routes, POST /auth/logout, /assets/, /apply and /apply/status. Anything else gets 403 forbidden under /api/, 303 to /apply/status for a page, and 403 otherwise, before any handler or the response cache runs. LookupSession stops resolving a pre-user session once its identity has a user. RevokePreUser and DeletePreUserSessions are ready for TKT-01M29CP2MG. The API and auth routes are now tables (api.Routes, auth.Routes, web.Route), so TestPreUserRoutesAreExactlyTheAllowlist walks every route, on a recording server and on the real one. TestEveryRouteIsInTheTable fails the build on a route registered outside a table, and issuerCallers lists every caller of the session issuers. The client draws only the application flow while /me says pre_user, and the rebuilt thread-view bundle is 38548 of 60000 B. Next: TKT-01M29CP2KZ adds its /api/v1/apply routes to web.PreUserRoutes and to theAllowlist in internal/web/preuser_test.go. TKT-01M29CP2NG replaces the placeholders in routes.tsx only. A new sign-in path must add itself to issuerCallers.
