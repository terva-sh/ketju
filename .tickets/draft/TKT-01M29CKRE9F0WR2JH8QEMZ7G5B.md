---
schema: 3
id: TKT-01M29CKRE9F0WR2JH8QEMZ7G5B
title: Support multiple providers with domain gates and auto-approve
type: task
status: draft
status_reason: null
priority: high
due_on: null
labels:
  - area:identity
  - effort:M
assignees: []
milestone: phase-2
parent: TKT-01M29BKBKZW26HNHFM1YFFGVZC
origin: null
dependencies:
  - TKT-01M29C5Y342B1ZBX6XCC69MFYM
  - TKT-01M29C7ZZ9KFSNZ8WC8YP1HAYV
blocks_on: none
references:
  - ref: design:identity-providers
    path: docs/05-identity-and-accounts.md
claim: null
archive: null
created_at: 2026-09-11T23:24:59Z
updated_at: 2026-09-11T23:24:59Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:terva/mieli
  name: Mieli
extensions: {}
---

## Description

Several providers on the login page at once, `allowed_email_domains` as an optional gate per provider, and `auto_approve` letting an organization's own SSO behave as open registration while everyone else goes through approval.

A domain gate only means something when the provider asserts `email_verified`. Trusting an unverified email claim from a public IdP turns the gate into decoration.

## Acceptance criteria

- [ ] An email outside allowed_email_domains is refused at callback with a clear reason
- [ ] The domain gate requires email_verified and refuses when the claim is absent
- [ ] An auto_approve provider skips the application flow while others still require it
- [ ] The login page lists exactly the configured and healthy providers
