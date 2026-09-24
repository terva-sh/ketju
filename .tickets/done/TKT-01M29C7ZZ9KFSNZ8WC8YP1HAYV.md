---
schema: 3
id: TKT-01M29C7ZZ9KFSNZ8WC8YP1HAYV
title: Implement the OIDC start and callback flow
type: task
status: done
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
  - ref: pr:72
    path: null
claim: null
archive: null
created_at: 2026-09-11T23:18:33Z
updated_at: 2026-09-24T20:45:10Z
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

- [x] A tampered state, a replayed nonce, a wrong audience, and an expired token are each rejected with their own test
- [x] return_to accepts only a same-origin path, and an absolute URL to another host is refused
- [x] A fake in-process issuer supports the full flow in tests with no network access
- [x] A suspended or deactivated user is shown the reason and gets no session

## Implementation plan

Revised at claim (2026-09-24), after reading internal/auth/oidc, internal/auth/session, internal/web and cmd/ketju/serve.go as they stand.

### Approach
1. **`internal/auth/seal`**: a generic `Sealer`. It is AES-256-GCM over JSON, with the purpose as additional data and the seal time inside. `Open(purpose, value, maxAge, &v)` returns `ErrInvalid` or `ErrExpired`.
   - It is its own package, not `oidc.Sealer`, because TKT-01M29C8005's `__Host-ketju_reg` cookie uses it too, and the purpose binding keeps one cookie from being replayed as the other.
   - `LoadOrCreateKey(path)` creates 32 random bytes, mode 0600 in a 0700 directory, written to a temp file and linked into place so racing starts agree. It warns on a group- or world-accessible key.
2. **Config.** `cookie_key_file` (env `KETJU_COOKIE_KEY_FILE`, relative to the config file). `Config.CookieKeyPath()` defaults to `<data_dir>/secrets/cookie.key`.
3. **`internal/web/auth`**:
   - `GET /auth/{provider}/start` seals `Flow{provider, state, nonce, PKCE verifier, return_to}` into `__Host-ketju_login` (10 minutes, Lax, HttpOnly, Secure) and redirects 302 to `AuthCodeURL`.
   - `GET /auth/{provider}/callback`:
     - clears that cookie first, whatever the outcome;
     - opens it, requires the path's provider, and compares state in constant time;
     - handles an IdP `error`;
     - calls `Provider.Exchange` (signature, issuer, audience, expiry and nonce, through go-oidc and `Verify`), then `EmailAllowed`, then `identity.Service.Login`;
     - on success revokes any session the browser held (fixation), sets the session cookie, and redirects 303 to `return_to`.
   - `POST /auth/logout` revokes the session and clears the cookie. The CSRF middleware in front holds it to this site.
   - `SafeReturnPath` requires a leading slash, a parse (which refuses control characters), no host, and an unescaped path that neither starts with `//` nor holds a backslash.
   - Pages are plain server HTML with no inline script or style, which the CSP forbids.
4. **`internal/service/identity.Login`** finds the identity by (provider, sub), never by email.
   - Found: `session.IssueUser`. A `*NotActiveError` becomes Unavailable, and `ErrNoUser` becomes Applicant. On success it records `last_login_at`, the claims, the email and `email_verified` (new query `RecordIdentityLogin`); a failed record is logged and the sign-in stands.
   - Not found: Link if a member is signed in, else Register.
   - Register, Link and Applicant answer 501 pages until TKT-01M29C8005, TKT-01M29CKRES and TKT-01M29CKRG9 land.
5. **`serve`** gets a `sign-in` step before the listener. It loads or creates the key and, when providers are configured, runs `oidc.New` (which aborts if every provider fails, as docs/05 says), `Start`, and a "close providers" closer. With none configured it logs that nobody can sign in, and `/auth/*` answers "unavailable".
6. **`internal/auth/oidc/oidctest`** is a flow-capable fake IdP:
   - authorize, then redirect with code and state;
   - a token endpoint that checks client auth, PKCE S256 and the redirect_uri, and makes codes single use;
   - knobs for subject, claims, audience, expiry and a replayed nonce.

   The existing fake stays inside oidc's own tests, which depend on its private fields. A guard test fails if anything but a test imports oidctest.

### Tests
- **AC1**, one test each: a state altered by a byte (no exchange happens), a GCM tag altered in the flow cookie (no exchange), an earlier sign-in's nonce replayed into the token, a wrong `aud`, and an expired token. Also:
  - a missing cookie;
  - an expired flow;
  - a callback replayed with its cookie;
  - another provider's callback;
  - an IdP error;
  - an unknown provider;
  - the email-domain gate.
- **AC2**: a `SafeReturnPath` table, plus a whole flow with `https://evil.example/phish` that lands on `/`.
- **AC3**: `TestTheWholeSignIn`, start to signed in against the in-process issuer, checking cookie attributes, the IdP request, the recorded login, and that the log carries no code or state. `TestServeSignsInThroughAProvider` runs the same through the real `serve` listener.
- **AC4**: suspended, deactivated and pending users each get 403 "Account unavailable", no session cookie, and no session row.

### Design
docs/05 "Login flow", "Provider configuration" and "Sessions and CSRF"; docs/02 "Observability".

### Interactions
- Consumes TKT-01M29C5Y3E and TKT-01M29C7ZZR.
- Hands `seal` and the Register outcome to TKT-01M29C8005, which replaces the registration page with its handle screen.
- TKT-01M29C800G adds the bootstrap hook to `identity.Login`'s success path; it is not called yet.
- TKT-01M29CKRES (linking) and TKT-01M29CKRG9 (applicants) replace their 501 pages.

### Settled in grooming (2026-09-24)

- The cookie sealing key is 32 bytes at data/secrets/cookie.key, created mode 0600 on first start when absent, with an optional config path. It is never logged.
- This ticket also owns /auth/logout.
- Suspended or deactivated users get a generic 'account unavailable' page. Reason and end date are Phase 2 moderation columns.
- The login cookie is cleared on callback, so it is single use.

## Notes

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T19:51:19Z

The session constructors this ticket consumes landed with TKT-01M29C7ZZR (internal/auth/session/issue.go). IssueUser(ctx, identityID, Meta) signs in whoever the identity belongs to. It refuses with *NotActiveError (errors.Is ErrNotActive; .Status says which, for the unavailable page), ErrNoUser for an identity with no user (the approval branch: IssuePreUser), or ErrNoIdentity. It decides inside the transaction that inserts, so the callback needs no status read of its own for the gate. C7ZZR's AC3, testing this callback against pending, suspended and deactivated users, is carried here: suspended and deactivated by this ticket's AC4, and pending by draft TKT-01M3AFHGJD (Test the OIDC callback against a pending user).

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T20:44:38Z

Built and verified (2026-09-24).

- `go test -race ./...` passes, and `go vet ./...` and gofmt are clean.
- 47 mutants over seal.go, returnto.go, auth.go, identity/login.go, the serve wiring and the config key path are all killed.
  - The first pass left five survivors, all in SafeReturnPath. Each was a layer another layer already covered: url.Parse refuses control characters, `//host` parses to a host, and a path starting with `/` has no scheme. So the function now keeps only the layers that do the work, and its comment says what url.Parse is relied on for.

Decisions made without a new decision from the user, none of them changing what a member sees beyond this ticket's settled scope:
- **The sealer is internal/auth/seal, not oidc.Sealer.** C8005's registration cookie is the second user, and purpose binding is what keeps the two cookies apart.
- **cookie_key_file is a top-level config key**, with env KETJU_COOKIE_KEY_FILE, and is resolved against the config file like data_dir. This is the "optional config path" settled in grooming.
- **The callback's outcome pages are plain server HTML**, not SPA routes. The callback is server-owned, the CSP forbids inline style and script, and adding SPA routes for them would mean new URLs in docs/07. All failures before the IdP's answer is trusted show one "Sign-in failed" page, and the reason goes to the log at warn.
- **Register, Link and Applicant answer 501 pages** naming what is not built yet, rather than 404 or a redirect.
- **A successful sign-in revokes any session the browser already held**, against session fixation.
- **Logout is POST only.** A GET is 405.
- **The flow-capable fake is a new package, oidctest.** The existing fake stays in oidc's tests (see the plan).
- **serve builds the registry only when providers are configured.** oidc.New refuses an empty list, and a server with none must still start for development and for every existing test.

**Criteria.** AC4 says "shown the reason". Grooming settled that the page is a generic "account unavailable" until the Phase 2 moderation columns exist. It is ticked on that settled reading: the page says the account cannot sign in, and why in detail is Phase 2's.

The callback test covers pending users as well as suspended and deactivated, which is what draft TKT-01M3AFHGJD asks for.

## Summary

Sign-in works end to end. /auth/{provider}/start seals state, nonce, PKCE verifier and a validated return_to into a ten-minute __Host-ketju_login cookie (internal/auth/seal, key at data/secrets/cookie.key or cookie_key_file). The callback clears it, checks it, exchanges and verifies the token, and hands the identity to identity.Login, which signs in an active user through session.IssueUser. An inactive account gets 'Account unavailable' and no session. Registration, linking and applicants answer 501 until their tickets land. POST /auth/logout ends the session. serve discovers the providers at startup, and oidctest is an in-process IdP for the whole flow.
