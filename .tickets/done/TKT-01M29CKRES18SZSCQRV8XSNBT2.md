---
schema: 3
id: TKT-01M29CKRES18SZSCQRV8XSNBT2
title: Implement account linking
type: task
status: done
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
  - ref: pr:177
    path: null
  - ref: pr:178
    path: null
claim: null
archive: null
created_at: 2026-09-11T23:24:59Z
updated_at: 2026-09-29T23:53:35Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase2-run-KRES
  name: ""
extensions: {}
---

## Description

Completing a login with an unknown identity while already signed in attaches that identity to the current user. One user, many identities, as a first-class operation rather than a support request.

The rule this protects is the one in docs/05: email never auto-links identities across providers. Linking happens only when a signed-in user performs it deliberately, which is what closes the hole where an attacker controls an IdP that asserts the victim's email.

## Acceptance criteria

- [x] A link whose identity already belongs to another user, or to an applicant with no user, answers 409 with a page saying which, and changes no identities row
- [x] Each link writes one audit_log row, identity.linked, naming the provider and identity id, in the transaction that writes the identity; with an injected audit failure no identity row is written
- [x] Signed out, a second provider asserting the same verified email as an existing member takes the registration path and never signs in as that member (TestTwoProvidersWithOneEmailDoNotLink)
- [x] After a link, signing in with the new identity goes through session.IssueUser, and for a pending, suspended or deactivated user writes no session row; a member who is not active cannot link
- [x] The link path calls no session issuer, and doorCallers, insertCallers and issuerCallers in chokepoint_test.go are unchanged
- [x] An unknown identity arriving through a link flow the member started with POST /auth/{provider}/link is attached to that member's user id, with the users and sessions counts unchanged; without that intent, a signed-in callback with an unknown identity answers 409 and writes nothing
- [x] Linking writes no last_login_at or raw_claims_json on the identity; the first sign-in with it sets them (review 1342, finding 2)

## Implementation plan

Groomed 2026-09-29 for Phase 2 (TKT-01M3M9RR26) against main at 7af2c5a. Revised at claim, 2026-09-29, against main at 2e2fe7c.

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

Revised at claim against main 2e2fe7c. What differs from the groomed plan is marked "(claim)".

1. **Starting a link.** `POST /auth/{provider}/link` in `internal/web/auth/link.go`, in the `auth.Routes` table. It refuses a viewer who is not `Member()` with 403, and a request authenticated by an agent credential with 403 (claim: docs/05 says agents never use OIDC, and `session.IssueUser` does not check `kind`, so an agent must not be able to link an identity it could later sign in with). An unknown or disabled provider is 404 (`pageNoProvider`). It seals a `Flow` with a new `LinkUser int64` field, sets `__Host-ketju_login`, and answers 303 to the IdP. `return_to` comes from the form and goes through `SafeReturnPath`. `GET /auth/link` is a server page listing `Providers.Enabled()` as one-button forms posting there; a viewer who is not a member gets 303 to `/auth/signin`. Neither route is on `web.PreUserRoutes`, so the pre-user gate closes both to an applicant.
2. **The callback.** `Login` gains a `linkUser int64` argument, the flow's `LinkUser`, 0 for a plain sign-in. With `linkUser != 0` it never signs in and never registers (claim): a found identity is `LinkRefused` with `Refusal` `LinkOtherUser`, `LinkApplicant`, or `LinkAlready` when it is already this member's; a not-found identity is `Link` only when `viewer.Member()` and `viewer.UserID == linkUser`, and otherwise `LinkRefused` with `LinkNotYou`. With `linkUser == 0` a found identity signs in as today, and a not-found one is `LinkRefused` with `LinkNoIntent` when the viewer is a member, and `Register` otherwise. The refusal is a new `Result.Refusal` field rather than `Result.Reason`, which carries a suspension's reason.
3. **The write.** `(*Service).Link(ctx, userID int64, c Claims, ip string) (LinkRefusal, error)` in `internal/service/identity/link.go`, one `s.DB.Write`:
   - `GetUser(userID)`; refuse with `ErrCannotLink` unless `status = 'active'` and `kind = 'human'` (claim: the kind check, as above), so a member suspended between start and callback cannot link.
   - `GetIdentityBySubject` again; a row found now is refused with the `LinkRefusal` it would have had at the callback, and nothing is written. A unique violation on `CreateIdentity` is `ErrLinkTaken`.
   - `CreateIdentity` with `user_id = userID` and the asserted email and `email_verified`. `RecordIdentityLogin` is not called (review 1342, finding 2).
   - `audit.Append(..., audit.By{Actor: userID, IP: ip}, "identity.linked", audit.User(userID), {"provider", "identity": id})`.

   It calls no `Issue*` function and writes no session.
4. **The callback's answers.** A link answers 303 to the flow's `return_to`, with no session cookie change. `LinkOtherUser` 409 "This sign-in already belongs to another account."; `LinkApplicant` 409 "This sign-in belongs to an application to join."; `LinkNoIntent` 409, offering `/auth/link`; `LinkNotYou` 409 (claim: the link was started by a member this browser is no longer signed in as); `LinkAlready` 200 (claim: nothing to do). `ErrCannotLink` is 403. A gated email gets the gate's 403, which runs before `Login`. `pageLinking` is deleted; the handle screen's "signed in meanwhile" 409 gets its own page pointing at `/auth/link`.
5. **Chokepoint.** No new entry in `issuerCallers`, `doorCallers` or `insertCallers`.
6. **Docs.** docs/05 step 3 already carries the intent clause (grooming); it gains the two routes and the agent refusal. docs/07 "URL scheme" gains `/auth/link` and `POST /auth/{provider}/link`. docs/04 "audit_log" names `identity.linked`.

### Tests

- `internal/service/identity` `TestLinkAttachesAndAudits`: the identity row's `user_id`; `last_login_at` and `raw_claims_json` NULL after the link and set by the first sign-in with it (review 1342, finding 2); `users` and `sessions` counts unchanged; one `identity.linked` row with its details.
- `TestLinkIsOneTransaction`: a `BEFORE INSERT ON audit_log` trigger leaves no identity row, and `Link` fails.
- `TestLinkRefusesAnInactiveMember`: `pending`, `suspended`, `deactivated`, and an agent, each refused with nothing written.
- `TestLinkFindsTheIdentityTaken`: an identity written between the callback's read and the write is refused with its refusal, nothing written.
- `internal/web/auth` `TestLinkingThenSigningIn`: Alice links `other-sub` on a second provider, signs out, signs in with it; the session is Alice's. Subtests set Alice `pending`, `suspended`, `deactivated` after the link: "Account unavailable", no session row.
- `TestTwoProvidersWithOneEmailDoNotLink`.
- `TestLinkRefusals`: Bob's identity (409, no row changed), an applicant identity (409), a flow whose `LinkUser` is another user (409), signed out during the flow (409, no registration), a gated email (403).
- `TestNoLinkWithoutIntent`: an unknown identity while signed in via plain `/start` is 409 with nothing written. `TestTheLinkStartNeedsAMember`: anonymous 403, cross-site refused by `CrossOrigin`, `GET /auth/link` 303 to sign-in when signed out and lists providers when signed in.
- `internal/auth/session` `TestOneDoorIntoSessions` and `TestTheIssuersAreKnown`: unchanged lists.

### Out of scope

- Listing and removing identities, and the refusal to remove the last one: TKT-01M29CCC0X (phase 4). The client's settings control that links to `/auth/link` gets a filed draft.
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

**agent:claude-code/phase2-run-KRES** at 2026-09-29T23:49:07Z

Implementation notes, 2026-09-29, for whoever reads the plan against the code.

- The link start answers 200 with a "Continue to {provider}" page (meta refresh 0 and a link), not the 303 the groomed plan named. The page policy carries `form-action 'self'`, and browsers apply form-action to the redirect that follows a form submission, so a 303 from the POST to the IdP's origin would be blocked. Two alternatives lost: widening form-action on /auth/link's response to each provider's origin (changes the security headers per route, for one page), and a GET start with its own CSRF token (the maintainer's decision names POST /auth/{provider}/link). A meta refresh with no delay is not governed by any CSP directive and is WCAG technique H76.
- An agent cannot link (claim, recorded in the plan). docs/05 says agents never use OIDC, and session.IssueUser checks status but not kind, so an identity linked to an agent would be an OIDC sign-in into it. startLink refuses a request authenticated by an agent credential, and identity.Link refuses any account whose kind is not human, in the transaction.
- Two refusals the groomed plan did not name: LinkNotYou (409), for a link whose sealed member is no longer who this browser is signed in as, active; and LinkAlready (200 "Already linked"), for the member's own identity. Both write nothing.
- docs/05 step 3 already carried the intent clause from grooming; this PR adds the routes, the one-transaction write, and the agent refusal under it.
- Filed TKT-01M3QS0TMV (Link to the link-a-sign-in page from the client), a phase-2 draft: the client has no settings screen until TKT-01M29CXK4 (phase 4), so /auth/link is reachable today only by address.

**agent:claude-code/phase2-run-KRES** at 2026-09-29T23:53:35Z

PR #177 was closed unmerged before any review ran: main moved under it (the suspension lane, TKT-01M29CP2QK) and docs/04 conflicted. PR #178 carries the same tree onto main 4d793b2 as one commit, with the docs/04 audit list holding both lanes' entries. The full race suite passed on the new base.

## Summary

Account linking landed through the explicit link action only. GET /auth/link offers each enabled provider; POST /auth/{provider}/link seals the member's id into the login cookie and answers a continue page to the IdP (form-action 'self' rules out a 303). identity.Login takes linkUser: a link attempt never signs in or registers, links an unknown identity only while the browser is still that active member, and refuses a found one as another account's, an applicant's, or already linked; without intent a signed-in callback with an unknown identity is 409 and writes nothing. identity.Link writes the identity and one identity.linked audit row in one transaction, refuses a non-active or non-human account, records no sign-in, and calls no issuer (chokepoint lists unchanged). docs/04, 05 and 07 updated. Follow-up draft TKT-01M3QS0TMV puts a client entry point on /auth/link before the phase-4 settings screens.
