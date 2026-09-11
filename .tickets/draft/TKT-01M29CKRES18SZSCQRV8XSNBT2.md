---
schema: 3
id: TKT-01M29CKRES18SZSCQRV8XSNBT2
title: Implement account linking
type: task
status: draft
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
blocks_on: none
references:
  - ref: design:identity-login
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

Completing a login with an unknown identity while already signed in attaches that identity to the current user. One user, many identities, as a first-class operation rather than a support request.

The rule this protects is the one in docs/05: email never auto-links identities across providers. Linking happens only when a signed-in user performs it deliberately, which is what closes the hole where an attacker controls an IdP that asserts the victim's email.

## Acceptance criteria

- [ ] An unknown identity arriving during a signed-in session links to that user and does not create a second one
- [ ] An identity already attached to another user is refused with a clear message
- [ ] Linking is recorded in audit_log
- [ ] A test proves two providers asserting the same email do not auto-link
