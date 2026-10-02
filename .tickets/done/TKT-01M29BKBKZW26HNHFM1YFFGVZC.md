---
schema: 3
id: TKT-01M29BKBKZW26HNHFM1YFFGVZC
title: OIDC identity, sessions, and account lifecycle
type: epic
status: done
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
updated_at: 2026-10-02T15:41:58Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase4-run-orchestrator
  name: ""
extensions: {}
---

## Description

`internal/auth/oidc` and `internal/auth/session`: signing in through allowlisted identity providers, server-side sessions, and what happens to an account over its life.

A user is a local entity with a handle. Identities (provider_key plus IdP subject) attach to users, and nothing user-facing is keyed on an IdP subject or email. Email never auto-links identities across providers, which closes the hole where an attacker controls an IdP that asserts a victim's email.

Sessions are server-side so suspension and sign-out-everywhere are immediate. The cookie carries only a random id and the server stores its hash. One function creates sessions and it checks that the user status is active. docs/09 names the approval-bypass bug this prevents, and it is the reason that chokepoint is a ticket of its own.

## Acceptance criteria

- [x] A configured provider completes start, callback, and session creation against a fake in-process issuer in tests
- [x] State, nonce, and PKCE are verified on callback and a tampered value is rejected
- [x] Two providers asserting the same email produce two identities and never auto-link
- [x] Suspending a user deletes their sessions and the next request is signed out
- [x] Exactly one function in the codebase creates a session row, and a test proves the callback path cannot bypass its status check

## Notes

**agent:claude-code/phase4-run-orchestrator** at 2026-10-02T15:41:57Z

Verification on 2026-10-02, against origin/main at adeba570. The maintainer chose the Phase 1 cleanup as part of the burn-down. A read-only agent checked each criterion against the code and its tests. It read the tests and did not run them; all of them pass in CI on main.
1. **Fake in-process issuer: holds.** `TestTheWholeSignIn` (`internal/web/auth/auth_test.go:228`) drives start, the oidctest IdP and the callback through the real middleware.
2. **State, nonce and PKCE: holds.** State is compared in constant time (`auth.go:210`) and nonce is checked at `oidc.go:591`, with `TestATamperedStateIsRefused`, `TestAReplayedNonceIsRefused`, `TestANonceFromAnotherLoginIsRefused` and `TestATamperedFlowCookieIsRefused`. Caveats:
   - PKCE refusal rests on reading the code. The S256 challenge and verifier are sent correctly, and the IdP enforces them, but no test sends a wrong verifier.
   - `oidc.go:591` skips the nonce check for an empty nonce. The callback always passes a random 256-bit one, so this is latent.
3. **Same email from two providers never links: holds.** The only identity lookup is by provider and subject (`queries/identity.sql:17-18`). `TestTwoProvidersWithOneEmailDoNotLink`.
4. **Suspension signs out: holds.** `perm/lifecycle.go:154-157` deletes sessions in the same transaction. `TestPhase2ExitSuspendedMember`, `TestChokepointRefusesSuspended`.
5. **One door into sessions: holds for production code.** `Store.insert` is the only production caller of `CreateSession`. `TestOneDoorIntoSessions`, `TestTheIssuersAreKnown`, `TestTheScanFindsASecondDoor`, `TestAnInactiveAccountGetsNoSession`. The test-only `sessiontest` helper also creates sessions, which the scan allows explicitly, and the scan fails if non-test code imports it.

## Summary

Phase 1's identity epic. All 17 children are done. On 2026-10-02 all five criteria were verified against the code and its tests and ticked. See the verification note for the PKCE and test-helper caveats.
