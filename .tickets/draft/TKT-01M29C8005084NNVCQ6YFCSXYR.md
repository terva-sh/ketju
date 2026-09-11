---
schema: 3
id: TKT-01M29C8005084NNVCQ6YFCSXYR
title: Implement open-mode registration with handle selection
type: task
status: draft
status_reason: null
priority: high
due_on: null
labels:
  - area:identity
  - effort:M
assignees: []
milestone: phase-1
parent: TKT-01M29BKBKZW26HNHFM1YFFGVZC
origin: null
dependencies:
  - TKT-01M29C5Y3E5SKHFR2SHJ8Y60TP
  - TKT-01M29BYYH73ZBC9WTS7HN03PKJ
blocks_on: none
references:
  - ref: design:identity-registration
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

The `mode = "open"` path: a one-field screen asking for a handle, prefilled from `preferred_username` or the email local part, checked for uniqueness, then a user with status active, an identity, and a session.

The handle is chosen here rather than taken from the IdP, which is the decision that lets a user keep their identity when an organization changes providers. Uniqueness is case-insensitive, and the check needs to happen at the database rather than only in the form, because two people can submit the same handle in the same second.

## Acceptance criteria

- [ ] A handle is validated for character set, length, and case-insensitive uniqueness before the user is created
- [ ] A collision between two concurrent registrations produces one user and one clear error, not two users
- [ ] Reserved handles such as admin and deleted-user prefixes are refused
- [ ] The user, the identity, and the session are created atomically or not at all
