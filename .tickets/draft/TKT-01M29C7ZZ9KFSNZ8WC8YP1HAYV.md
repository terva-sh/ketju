---
schema: 3
id: TKT-01M29C7ZZ9KFSNZ8WC8YP1HAYV
title: Implement the OIDC start and callback flow
type: task
status: draft
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
blocks_on: none
references:
  - ref: design:identity-login
    path: docs/05-identity-and-accounts.md
claim: null
archive: null
created_at: 2026-09-11T23:18:33Z
updated_at: 2026-09-11T23:18:33Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:terva/mieli
  name: Mieli
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
