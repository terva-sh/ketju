---
schema: 3
id: TKT-01M29BKBM38VTYY4Y91018GTH6
title: Approval-mode registration and the forms system
type: epic
status: done
status_reason: null
priority: high
due_on: null
labels:
  - area:identity
  - effort:L
assignees: []
milestone: phase-2
parent: null
origin: null
dependencies:
  - TKT-01M29BKBKZW26HNHFM1YFFGVZC
blocks_on: children
references:
  - ref: design:identity-approval
    path: docs/05-identity-and-accounts.md
  - ref: pr:263
    path: null
claim: null
archive: null
created_at: 2026-09-11T23:07:17Z
updated_at: 2026-10-01T05:14:18Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase2-run-orchestrator
  name: ""
extensions: {}
---

## Description

Registration gated behind a moderator decision, and the small declarative form system that carries it.

In approval mode a visitor who authenticates but has no user gets an identity row with user_id NULL and a pre-user session bound to that identity. A pre-user session reaches the application form, the status page, and logout, and nothing else. Only on approval does a users row exist.

The form is data rather than code: a JSON field list stored in form_versions, validated server-side from the schema, rendered by the Preact client from the same schema, and renderable by a future html/template client with no new work. Versioning means old applications stay readable after an admin edits the form.

This epic is the exit criterion for Phase 2: a club can open registration to strangers safely.

## Acceptance criteria

- [ ] A pre-user session is rejected by every endpoint except the application form, the status page, and logout
- [x] Approving an application creates the users row, links the identity, and writes an audit_log entry
- [x] Denying with 'block this subject' keeps the identity with user_id NULL and refuses re-application
- [x] Editing a form creates a new version and applications submitted against the old version still render
- [x] Server-side validation is driven by the stored schema, not by hand-written per-field code
- [x] Application submission is rate limited per identity as well as per address, using the pre-user session as the key; this is the second limiter stage docs/08 describes, and it runs after session resolution

## Notes

**agent:claude-code/phase2-run-orchestrator** at 2026-09-30T23:53:59Z

Closing evidence, 2026-10-01 (orchestrator). All 10 children are done. The tests below were read on main at f3ba70d.

1. **Unticked.** A pre-user reaches more than the application form, the status page and logout. The allowlist in `internal/web/preuser.go:41-61` also admits `GET /api/v1/me`, the sign-in routes (`/auth/signin`, `/auth/{provider}/start`, `/auth/{provider}/callback`) and `/assets/`. That is the list docs/05 names (lines 241-246), and docs/07 has `/me` answer a pre-user with `pre_user`. The design allows these, so the behaviour is as intended. The criterion's own wording is narrower, though, so the box stays empty for the maintainer to accept. `TestPreUserRoutesAreExactlyTheAllowlist` (`internal/web/preuser_test.go:278`) pins the list and walks every route, and `TestANewRouteIsClosedToAPreUserByDefault` keeps a new route closed. `/auth/link` is refused. Delivered by TKT-01M29CKRG9.
2. **Ticked.** `TestApproveIsOneTransactionOrNone` (`internal/service/applications/review_test.go:124`) covers the users row, `identities.user_id` and the `application.approved` audit row, all in one transaction. Delivered by TKT-01M29CP2MG.
3. **Ticked.** `TestDenyWithBlockRefusesReapplication` (`review_test.go:333`) and `TestBlockedIdentityIsRefusedAtTheForm` (`apply_test.go:224`). Delivered by TKT-01M29CP2MG and TKT-01M29CP2KZ.
4. **Ticked.** `TestCreateVersionKeepsOldVersions` (`internal/service/forms/forms_test.go:129`), `TestOldVersionStillRenders` (`review_test.go:562`), and the version test in `ApplicationDetail.test.tsx`. Delivered by TKT-01M29CP2KE, TKT-01M29CP2MG and TKT-01M3NRX2NN.
5. **Ticked.** `TestANewSchemaNeedsNoCode` (`internal/forms/validate_test.go:150`), with submit validating against the stored active version (`apply.go:279-287`). Delivered by TKT-01M29CP2KE and TKT-01M29CP2KZ.
6. **Ticked, with a weaker half.**
   - The per-identity stage runs after session resolution and is keyed `identity:<id>`. `TestApplyIsLimitedPerIdentity` (`internal/web/api/apply_test.go:281`) proves it through the real chain.
   - The per-address half is the general per-address limit on every API write (`ratelimit.go:285`). It is tested with `/threads` (`TestAuthAndWritesAreLimitedSeparately`), not with `/apply`.
   - Delivered by TKT-01M29CP2KZ.

## Comments

**agent:claude-code/web** at 2026-09-19T23:21:33Z

The new rate-limit criterion arrived here from TKT-01M29C800X "Set the CSRF
posture and write rate limits", which built the transport-level limiter and
could not build this half.

docs/05 asks for limits on `/auth/*` and application submission "keyed by IP and
identity". The IP half shipped in C800X. The identity half cannot live where
that limiter does: it runs at chain position 5, deliberately outside session
resolution, so that an unauthenticated flood does not cost a database query per
request. No viewer exists at that point, so there is nothing to key on.

Application submission is the one place an unapproved visitor does have an
identity -- the pre-user session this epic defines -- which is why the criterion
belongs to this epic rather than to the middleware.

TKT-01M29P9RSW "Implement agent accounts, provisioning, and the agent
credential" needs the same post-session stage for its per-agent token bucket.
Whichever of the two lands first should build the stage rather than a
single-purpose limiter, and say in its summary that it did.

## Summary

Closed 2026-10-01 once all ten children were done (docs/13: the Phase 2 epics close once their children do). Criteria 2-6 are met by named tests on main; the note on this ticket cites them. Criterion 1 stays unticked. A pre-user reaches the design's allowlist (docs/05), which adds /me, the sign-in routes and assets to the three endpoints the criterion names. That is intended behaviour, but it is wider than the wording, so the maintainer is asked to accept it.
