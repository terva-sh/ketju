---
schema: 3
id: TKT-01M29C7ZZ9KFSNZ8WC8YP1HAYV
title: Implement the OIDC start and callback flow
type: task
status: ready
status_reason: null
priority: urgent
due_on: null
labels:
  - area:identity
  - effort:L
assignees: []
milestone: phase-1
parent: TKT-01M29BKBKZW26HNHFM1YFFGVZC
origin: null
dependencies:
  - TKT-01M29C5Y342B1ZBX6XCC69MFYM
  - TKT-01M29C5Y3E5SKHFR2SHJ8Y60TP
  - TKT-01M29C7ZZR3FJMZ7P7VA0CWNJ2
blocks_on: none
references:
  - ref: design:identity-login
    path: docs/05-identity-and-accounts.md
claim: null
archive: null
created_at: 2026-09-11T23:18:33Z
updated_at: 2026-09-24T19:51:19Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase1-run-415af6aa
  name: ""
extensions: {}
---

## Description

`GET /auth/{provider}/start` generates state, nonce, and a PKCE verifier, stores them in a short-lived encrypted SameSite=Lax cookie, and redirects. `GET /auth/{provider}/callback` verifies state, exchanges the code with PKCE, and verifies the ID token for issuer, audience, nonce, signature, and expiry.

Then the three-way branch on `identities (provider_key, sub)`: found loads the user, not-found while signed in is an account link, and not-found while signed out enters the registration path.

`return_to` is captured at start and validated as a same-origin path before the redirect. An open redirect through a login flow is the classic way this endpoint becomes a phishing tool.

Tests run against a tiny in-process fake issuer, which is also what makes the Playwright smoke tests possible later.

## Acceptance criteria

- [ ] A tampered state, a replayed nonce, a wrong audience, and an expired token are each rejected with their own test
- [ ] return_to accepts only a same-origin path, and an absolute URL to another host is refused
- [ ] A fake in-process issuer supports the full flow in tests with no network access
- [ ] A suspended or deactivated user is shown the reason and gets no session

## Implementation plan

Pre-groom plan (2026-09-24). Revise at claim, after reading the code as it stands then.

### Approach
1. Add a pre-login state sealer to internal/auth/oidc: `FlowState{Provider, State, Nonce, PKCEVerifier, ReturnTo, IssuedAt}`, sealed with AES-256-GCM from the standard library. Authenticated encryption makes the cookie both secret and tamper-evident, as decided in docs/05 and TKT-01M29BSPEF. Cookie `__Host-ketju_login`, SameSite=Lax, Max-Age about 10 minutes, cleared on callback whatever the outcome.
2. `GET /auth/{provider}/start`: look the provider up with `Registry.Lookup` and answer 404 for an unknown or disabled one. Generate state, nonce, and verifier with crypto/rand. Validate `return_to` with `SafeReturnPath`: it must start with a single `/`, carry no `//`, no backslash, no scheme or host after `url.Parse`, and no control characters; anything else becomes `/`. Seal the state, set the cookie, and 302 to `Provider.AuthCodeURL(state, nonce, verifier)`.
3. `GET /auth/{provider}/callback`: open the cookie and compare `state` with `subtle.ConstantTimeCompare`. Refuse if the provider in the cookie differs from the path. Then call `Provider.Exchange(ctx, hc, code, nonce, verifier)`, which already checks signature, issuer, audience, expiry, and nonce through go-oidc, then `Provider.EmailAllowed`.
4. Branch on `identities(provider_key, subject)`, looked up with the sqlc `IdentityByProviderSubject`, in a new `internal/service/identity` package:
   - Found: load the user. If the status is not active, render the reason and create no session.
   - Not found and signed in: hand off to account linking.
   - Not found and signed out: hand off to registration (TKT-01M29C8005).
   - Session creation goes only through the TKT-01M29C7ZZR chokepoint. Update `identities.last_login_at`/`raw_claims_json`, call the bootstrap hook (TKT-01M29C800G), then 302 to ReturnTo.
5. Mount the handlers as `web.Groups.Auth` in a new `internal/web/auth` package. Request logging already drops the query string (`?code=&state=`), so check that and add nothing.

### Files
New: internal/auth/oidc/flowstate.go, internal/service/identity/login.go, internal/web/auth/{start,callback}.go. Reuses `Registry`, `Provider.AuthCodeURL/Exchange/EmailAllowed/LocalGroups`, `oidc.Identity`, and the fake IdP in internal/auth/oidc/fakeidp_test.go. That fake lives in a _test file, so move it into a test-only package (for example internal/auth/oidc/oidctest) so that web/auth tests and later Playwright can import it.

### Tests
- AC1, one httptest case each against the fake issuer: a state flipped by one byte, a cookie whose GCM tag was altered, an ID token minted with a previous login's nonce, a wrong `aud`, and an `exp` in the past (fake clock through `Options.Now`).
- AC2: table-driven `SafeReturnPath` cases (`//evil.com`, `/\evil.com`, `https://evil.com/`, `/%2F%2Fevil.com`, `javascript:`, and a valid `/t/abc/slug?page=2`), plus one full-flow test that ends at the evil value and gets redirected to `/`.
- AC3: the full start→IdP→callback round trip with `httptest.Server` in TLS mode and no external network.
- AC4: suspended and deactivated users each get the reason page and no `Set-Cookie` for the session; the `sessions` row count stays unchanged.

### Design
Reread docs/05 "Login flow", "Provider configuration", and "Sessions and CSRF", and docs/02 "Observability" on query strings.

### Interactions
Consumes TKT-01M29C5Y3E (Viewer, cookie) and TKT-01M29C7ZZR (session constructors); the ticket should depend on C7ZZR, which is not recorded today. Defines `oidc.FlowState`/`oidc.Sealer` and `identity.Login` for TKT-01M29C8005, TKT-01M29C800G, TKT-01M29CKRES (linking), and TKT-01M29CKRG9 (approval branch).
Open: where the sealing key comes from. config.toml has none; a per-process random key breaks in-flight logins at restart. Also open: the route, the shape, and the page that "show the reason" uses. No SPA route exists, and docs/04 `users` has no suspension reason or until column. Also open: /auth/logout has no owning ticket.
Open: the approval-mode branch belongs to Phase 2; it should answer 501 until then.
Note: a stateless cookie cannot make a nonce strictly one-time. Replay is bounded by the cookie TTL and by single-use codes at the IdP.

### Settled in grooming (2026-09-24)

- The cookie sealing key is 32 bytes at data/secrets/cookie.key, created mode 0600 on first start when absent, with an optional config path. It is never logged.
- This ticket also owns /auth/logout.
- Suspended or deactivated users get a generic 'account unavailable' page. Reason and end date are Phase 2 moderation columns.
- The login cookie is cleared on callback, so it is single use.

## Notes

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T19:51:19Z

The session constructors this ticket consumes landed with TKT-01M29C7ZZR (internal/auth/session/issue.go). IssueUser(ctx, identityID, Meta) signs in whoever the identity belongs to. It refuses with *NotActiveError (errors.Is ErrNotActive; .Status says which, for the unavailable page), ErrNoUser for an identity with no user (the approval branch: IssuePreUser), or ErrNoIdentity. It decides inside the transaction that inserts, so the callback needs no status read of its own for the gate. C7ZZR's AC3, testing this callback against pending, suspended and deactivated users, is carried here: suspended and deactivated by this ticket's AC4, and pending by draft TKT-01M3AFHGJD (Test the OIDC callback against a pending user).
