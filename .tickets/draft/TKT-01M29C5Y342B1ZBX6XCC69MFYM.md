---
schema: 3
id: TKT-01M29C5Y342B1ZBX6XCC69MFYM
title: Build the OIDC provider registry with discovery and JWKS refresh
type: task
status: draft
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
updated_at: 2026-09-11T23:17:26Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:terva/mieli
  name: Mieli
extensions: {}
---

## Description

Load the provider blocks from config into a registry, fetch each discovery document and JWKS at startup, and refresh on a key-rotation verification failure or every 24 hours.

Providers are an allowlist by construction: only configured issuers are ever contacted and the login page shows only configured providers. A provider that fails discovery at startup is disabled with a loud log line rather than aborting the server, because one broken provider should not take a forum offline for everyone else.

The provider quirks in docs/09 belong here: the Azure issuer mismatch needing go-oidc's `InsecureIssuerURLContext` behind a per-provider flag, and Google's `hd` claim.

## Acceptance criteria

- [ ] Discovery and JWKS are fetched at startup for every configured provider
- [ ] A provider failing discovery is disabled and logged, and the server still starts and serves the others
- [ ] A signature verification failure against an unknown key id triggers one JWKS refresh and a retry, not a refresh per request
- [ ] The issuer-mismatch escape hatch exists per provider and is off by default
