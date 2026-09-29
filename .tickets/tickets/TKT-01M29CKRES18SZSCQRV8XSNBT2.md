---
schema: 3
id: TKT-01M29CKRES18SZSCQRV8XSNBT2
title: Implement account linking
type: task
status: ready
status_reason: null
priority: normal
due_on: null
labels:
  - area:identity
  - effort:M
assignees: []
milestone: phase-2
parent: TKT-01M29BKBKZW26HNHFM1YFFGVZC
origin: null
dependencies:
  - TKT-01M29C7ZZ9KFSNZ8WC8YP1HAYV
  - TKT-01M3NRYCP5BSA717S1HANWY1AA
blocks_on: none
references:
  - ref: design:identity-login
    path: docs/05-identity-and-accounts.md
claim: null
archive: null
created_at: 2026-09-11T23:24:59Z
updated_at: 2026-09-29T07:18:23Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase2-run-orchestrator
  name: ""
extensions: {}
---

## Description

Completing a login with an unknown identity while already signed in attaches that identity to the current user. One user, many identities, as a first-class operation rather than a support request.

The rule this protects is the one in docs/05: email never auto-links identities across providers. Linking happens only when a signed-in user performs it deliberately, which is what closes the hole where an attacker controls an IdP that asserts the victim's email.

## Acceptance criteria

- [ ] A link whose identity already belongs to another user, or to an applicant with no user, answers 409 with a page saying which, and changes no identities row
- [ ] Each link writes one audit_log row, identity.linked, naming the provider and identity id, in the transaction that writes the identity; with an injected audit failure no identity row is written
- [ ] Signed out, a second provider asserting the same verified email as an existing member takes the registration path and never signs in as that member (TestTwoProvidersWithOneEmailDoNotLink)
- [ ] After a link, signing in with the new identity goes through session.IssueUser, and for a pending, suspended or deactivated user writes no session row; a member who is not active cannot link
- [ ] The link path calls no session issuer, and doorCallers, insertCallers and issuerCallers in chokepoint_test.go are unchanged
- [ ] An unknown identity arriving through a link flow the member started with POST /auth/{provider}/link is attached to that member's user id, with the users and sessions counts unchanged; without that intent, a signed-in callback with an unknown identity answers 409 and writes nothing
- [ ] Linking writes no last_login_at or raw_claims_json on the identity; the first sign-in with it sets them (review 1342, finding 2)

## Implementation plan

Groomed 2026-09-29 for Phase 2 (TKT-01M3M9RR26) against main at 7af2c5a.

### Settled in grooming

- **When a link happens.** An identity that is not found, arriving while the browser is signed in as a member, is an account link: the identity is attached to the current user (docs/05 "Login flow" step 3). This now holds only when the member started the flow as a link ("Explicit link intent" below). `identity.Login` already returns `Outcome` `Link` for exactly this case (`internal/service/identity/login.go`). The callback answers it with a 501 `pageLinking` today.
- **Email never links.** Two providers asserting the same email are two identities until the user links them while signed in (docs/05 "Login flow"). `Login` looks up by `(provider_key, subject)` only.
- **A found identity is never re-attached.** One that belongs to another user signs in as that user, as it does today. One that exists with no user is an applicant (TKT-01M29CKRG9). Linking reaches only identities that are not found (docs/05 step 3). This ticket's AC2, "refused with a clear message", applies to an explicit link attempt.
- **Registration never makes a second account.** A member who signed in during registration gets a 409, because a member adds an identity by linking it (docs/07 "URL scheme", `/auth/register`, already implemented).
- **The domain gate applies to linking.** `allowed_email_domains` and the `email_verified` rule of TKT-01M29CKRE9 run at every callback before `Login`, so a gated provider cannot be linked around its gate.
- **Linking issues no session.** The member already has one. The identity is later used to sign in through `session.IssueUser`, which checks the user's status in the transaction that inserts the row (TKT-01M29C7ZZR).
- **Linking is audited** (AC3). The action is `identity.linked`, and it is the record of when the link happened.
- **Explicit link intent** (maintainer decision 2026-09-29). A link happens only through an explicit "link a sign-in" action. It starts from a member's `POST /auth/{provider}/link`, which seals the member's user id into the flow cookie, and a server page `GET /auth/link` offers it. An unknown identity that arrives while a member is signed in, without that intent, is refused with 409 and nothing is written. Linking on any signed-in callback, which is how docs/05 step 3 reads today, lost: a member switching accounts would link silently, and a login-CSRF at a weak IdP could plant an attacker's identity on a victim's account. docs/05 step 3 gains the intent clause in the implementing PR.
- **A link is not a sign-in** (review 1342, finding 2). The link transaction writes the identity with the email and `email_verified` the IdP asserted, but it does not call `RecordIdentityLogin`, so `last_login_at` and `raw_claims_json` stay empty until the identity is first used to sign in.
- **Removing an identity** is `DELETE /settings/identities/{id}`, together with the refusal to remove the last one. That is TKT-01M29CCC0X, in phase 4.

### Approach

1. **Starting a link.** In `internal/web/auth/auth.go`, add `POST /auth/{provider}/link`. It refuses a viewer who is not `Member()` with 403, and an unknown or disabled provider with 404 (`pageNoProvider`). It seals a `Flow` with a new `LinkUser int64` field, sets `__Host-ketju_login`, and answers 303 to the IdP. Add `GET /auth/link` in `internal/web/auth/link.go`: a server page listing `Providers.Enabled()` as one-button forms. It is a 303 to `/auth/signin` for a viewer who is not a member.
2. **The callback.** `Login` gains a `linkUser int64` argument, the flow's `LinkUser`, 0 when there is none. A field on `session.Meta` was the alternative. It lost because `Meta` is what a session records about itself, and a link intent is not part of that. `Login` returns `Link` only when the identity is not found, the viewer is a member, and `LinkUser == viewer.UserID`. A found identity during a link flow returns `LinkRefused`, with `Reason` `LinkOtherUser` or `LinkApplicant`. It does not sign in. An unknown identity while signed in without intent returns `LinkNoIntent`.
3. **The write.** Add `(*Service).Link(ctx, userID int64, c Claims) error` in `internal/service/identity/link.go`. It is one `s.DB.Write`:
   - Re-read `GetUser(userID)` and refuse with `ErrNotActive` unless `status = 'active'`, so a member suspended between start and callback cannot link.
   - `GetIdentityBySubject` again; a row found now is `ErrLinkTaken`.
   - `CreateIdentity` with `user_id = userID` and the asserted email and `email_verified`; a unique violation is `ErrLinkTaken`. `RecordIdentityLogin` is not called, because no sign-in happened (review 1342, finding 2).
   - `AppendAudit{Action: "identity.linked", ActorID: userID, TargetType: "user", TargetID: userID, DetailsJson: {"provider", "identity": id}}`.

   It calls no `Issue*` function and writes no session. The existing session keeps working, and bootstrap is not applied at link time: `ApplyBootstrap` runs at the next sign-in with that identity, in `settle`, as for any sign-in.
4. **The callback's answers.** A link answers 303 to the flow's `return_to` with no cookie change. `LinkOtherUser` answers 409 with "This sign-in already belongs to another account." `LinkApplicant` answers 409 with "This sign-in belongs to an application to join." `LinkNoIntent` answers 409 as above. A link that the gate refuses gets the gate's 403. `pageLinking` is deleted. Each refusal writes nothing, and a new page constant in `page.go` covers each.
5. **Chokepoint.** The link path adds no caller to `issuerCallers`, `doorCallers` or `insertCallers` in `internal/auth/session/chokepoint_test.go`. The chokepoint test's unchanged lists are part of this ticket's evidence.
6. **Docs.** docs/05 "Login flow" step 3 gets the intent clause. docs/07 "URL scheme" gains `/auth/link` and `POST /auth/{provider}/link`. docs/04 "identities" names the `identity.linked` audit action.

### Tests

- `internal/service/identity` `TestLinkAttachesAndAudits`. It checks the identity row's `user_id`, that its `last_login_at` and `raw_claims_json` are NULL after the link and set only by the first sign-in with it (review 1342, finding 2), that the `users` count is unchanged, that the `sessions` count is unchanged, and the single `identity.linked` audit row with its details.
- `TestLinkIsOneTransaction`. An injected `BEFORE INSERT ON audit_log` trigger (the pattern from `TestRegistrationIsAtomic`) leaves no identity row, and `Link` returns an error.
- `TestLinkRefusesAnInactiveMember`. For each of `pending`, `suspended` and `deactivated`, `Link` is refused and writes nothing.
- `internal/web/auth` `TestLinkingThenSigningIn`. Alice (active) links `other-sub` on the second provider, then signs out and signs in with `other-sub`. The session is Alice's, issued through `IssueUser`. Subtests set Alice to `pending`, `suspended` and `deactivated` after the link. Signing in with `other-sub` is then "Account unavailable" with no session row, which proves that linking gives no second way to a member session.
- `TestTwoProvidersWithOneEmailDoNotLink`. Alice registered through `club` with verified `a@example.org`. Signed out, a new identity on `other` asserting the same verified email gets the registration path (open mode) and never a session as Alice. The identities stay separate.
- `TestLinkRefusals`: an identity owned by Bob (409, no row changed), an applicant identity (409), a flow whose `LinkUser` is another user id (409), and a gated email (403).
- `TestNoLinkWithoutIntent`. An unknown identity arriving while signed in via plain `/start` gets 409 with no identity written. `TestTheLinkStartNeedsAMember`: `POST /auth/{p}/link` without a member is 403, and cross-site is refused by `CrossOrigin`.
- `internal/auth/session` `TestOneDoorIntoSessions`: unchanged lists, which shows no new door.

### Out of scope

- Listing and removing identities, and the refusal to remove the last one: TKT-01M29CCC0X (phase 4). The client's settings identities screen belongs to the phase-4 settings screens.
- The applicant path that makes a found identity with no user a pre-user: TKT-01M29CKRG9.
- The `email_verified` domain rule: TKT-01M29CKRE9.

## Notes

**agent:claude-code/phase2-groom** at 2026-09-29T05:06:41Z

Open decision from Phase 2 grooming (TKT-01M3M9RR26), 2026-09-29, and the plan's "### Open decision" section: is an explicit link intent required?

docs/05 says an unknown identity that arrives while a member is signed in is linked. It does not say the member must have asked for the link. This is a security choice, so it belongs to a person (docs/12 "Gaps the design leaves open").

- A (as written): any callback that brings an unknown identity from a signed-in browser links it.
- B (recommended): a link starts only from a member's `POST /auth/{provider}/link`, whose flow cookie is sealed with the user id. A server page `GET /auth/link` offers it. Without that intent, an unknown identity that arrives while signed in answers 409.

Why B: under A, a member who clicks another provider to switch accounts links it silently. A login-CSRF at a weak IdP could also plant an attacker's identity on a victim's account. The plan is written for B, and says which steps drop out under A.

**agent:claude-code/phase2-groom** at 2026-09-29T05:38:49Z

Maintainer decision 2026-09-29: linking happens only through an explicit 'link a sign-in' action (the recommended option B), so the open decision in the note above is settled. The plan now records it under Settled in grooming. Review 1342 finding 2 (medium) is also fixed in the plan: the link transaction no longer calls RecordIdentityLogin, because a link is not a sign-in. The identity's last_login_at and raw_claims_json stay empty until its first real sign-in, and a test and a criterion pin this.
