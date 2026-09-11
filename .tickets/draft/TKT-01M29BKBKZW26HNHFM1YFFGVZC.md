---
schema: 3
id: TKT-01M29BKBKZW26HNHFM1YFFGVZC
title: OIDC identity, sessions, and account lifecycle
type: epic
status: draft
status_reason: null
priority: high
due_on: null
labels:
  - area:identity
  - effort:XL
assignees: []
milestone: phase-1
parent: null
origin: null
dependencies: []
blocks_on: children
references:
  - ref: design:identity
    path: docs/05-identity-and-accounts.md
claim: null
archive: null
created_at: 2026-09-11T23:07:17Z
updated_at: 2026-09-11T23:31:54Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:terva/mieli
  name: Mieli
extensions: {}
---

## Description

`internal/auth/oidc` and `internal/auth/session`: signing in through allowlisted identity providers, server-side sessions, and what happens to an account over its life.

A user is a local entity with a handle. Identities (provider_key plus IdP subject) attach to users, and nothing user-facing is keyed on an IdP subject or email. Email never auto-links identities across providers, which closes the hole where an attacker controls an IdP that asserts a victim's email.

Sessions are server-side so suspension and sign-out-everywhere are immediate. The cookie carries only a random id and the server stores its hash. One function creates sessions and it checks that the user status is active. docs/09 names the approval-bypass bug this prevents, and it is the reason that chokepoint is a ticket of its own.

## Acceptance criteria

- [ ] A configured provider completes start, callback, and session creation against a fake in-process issuer in tests
- [ ] State, nonce, and PKCE are verified on callback and a tampered value is rejected
- [ ] Two providers asserting the same email produce two identities and never auto-link
- [ ] Suspending a user deletes their sessions and the next request is signed out
- [ ] Exactly one function in the codebase creates a session row, and a test proves the callback path cannot bypass its status check
