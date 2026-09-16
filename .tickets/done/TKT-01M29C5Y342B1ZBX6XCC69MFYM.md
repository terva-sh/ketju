---
schema: 3
id: TKT-01M29C5Y342B1ZBX6XCC69MFYM
title: Build the OIDC provider registry with discovery and JWKS refresh
type: task
status: done
status_reason: null
priority: urgent
due_on: null
labels:
  - area:identity
  - effort:M
assignees: []
milestone: phase-1
parent: TKT-01M29BKBKZW26HNHFM1YFFGVZC
origin: null
dependencies:
  - TKT-01M29BW9QFZ2C7Q24KN1JW5MFW
blocks_on: none
references:
  - ref: design:identity-providers
    path: docs/05-identity-and-accounts.md
claim: null
archive: null
created_at: 2026-09-11T23:17:26Z
updated_at: 2026-09-16T18:50:29Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase1
  name: ""
extensions: {}
---

## Description

Load the provider blocks from config into a registry, fetch each discovery document and JWKS at startup, and refresh on a key-rotation verification failure or every 24 hours.

Providers are an allowlist by construction: only configured issuers are ever contacted and the login page shows only configured providers. A provider that fails discovery at startup is disabled with a loud log line rather than aborting the server, because one broken provider should not take a forum offline for everyone else.

The provider quirks in docs/09 belong here: the Azure issuer mismatch needing go-oidc's `InsecureIssuerURLContext` behind a per-provider flag, and Google's `hd` claim.

## Acceptance criteria

- [x] Discovery and JWKS are fetched at startup for every configured provider
- [x] A provider failing discovery is disabled and logged, and the server still starts and serves the others
- [x] A signature verification failure against an unknown key id triggers one JWKS refresh and a retry, not a refresh per request
- [x] The issuer-mismatch escape hatch exists per provider and is off by default

## Notes

**agent:claude-code/phase1** at 2026-09-16T18:50:29Z

Two findings worth carrying into the flow ticket (TKT-01M29C7ZZ9) and the
multi-provider ticket (TKT-01M29CKRE9).

### The key-set refresh needed a bound that the library does not provide

go-oidc's RemoteKeySet verifies against cached keys and refetches only when a
token's key id misses the cache, coalescing concurrent refetches. That is
correct for rotation and is what criterion 3 asks for in the honest case.

It does not bound the sequential case, and the key id is a field in a token
supplied by whoever called the callback endpoint. Measured with the bound
removed, 50 tokens carrying invented key ids provoked 50 fetches at the IdP.
An unauthenticated caller could therefore point ketju at someone else's
identity provider.

internal/auth/oidc/keyset.go wraps the library's key set and rate limits only
the key ids that have never verified anything. A real rotation presents an
unknown key id once, is allowed its fetch, and is remembered; invented ones
share one fetch per minute. The legitimate path is not slowed, because a
legitimate token carries a key id that is already known.

### The pinned algorithm list does not do what it looks like it does

The obvious claim -- that pinning the signing algorithms prevents key confusion,
an HS256 token verified against a key set of public keys -- is wrong here, and
was checked rather than assumed. go-oidc filters HS256 and `none` out of the
algorithms it takes from a discovery document, and go-jose refuses an HMAC
algorithm against a public key beneath that. Removing the pin does not make the
confusion test fail.

What the pin actually prevents is a liveness failure: gooidc.NewVerifier with an
empty list accepts RS256 alone, so a provider configured with an elliptic-curve
key would have every token refused. TestAnEllipticCurveProviderWorks is the test
that fails when the pin is removed, and the comments say which layer refuses
what, so the next reader does not inherit a belief in a defence that is not
there.

### Boundary

Exchange and Verify live here rather than in the handlers, because the client
secret is unwrapped in this package and the oauth2 config is unexported. The
flow ticket owns state, the nonce cookie, return_to validation and the
three-way branch on identities; it calls AuthCodeURL and Exchange and never
holds the secret.

`hd` is recorded on Identity but not acted on. A domain gate that trusts `hd` is
a different decision from one that trusts the email domain, and it belongs with
TKT-01M29CKRE9.

## Summary

internal/auth/oidc: registry over the config provider blocks, built on go-oidc and x/oauth2. Startup discovery and key fetch per provider; a failure disables that provider with its reason kept and retries it on each refresh; every provider failing is a startup error. Key-set refreshes provoked by an unrecognised key id are rate limited, which the library does not do and which an unauthenticated caller could otherwise drive. The client secret is unwrapped only here. 30 tests against a fake issuer that really signs.
