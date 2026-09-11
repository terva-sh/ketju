---
schema: 3
id: TKT-01M29C7ZZR3FJMZ7P7VA0CWNJ2
title: Route every session creation through one status-checking function
type: chore
status: draft
status_reason: null
priority: urgent
due_on: null
labels:
  - area:identity
  - area:test
  - effort:S
assignees: []
milestone: phase-1
parent: TKT-01M29BKBKZW26HNHFM1YFFGVZC
origin: null
dependencies:
  - TKT-01M29C5Y3E5SKHFR2SHJ8Y60TP
blocks_on: none
references:
  - ref: design:risks
    path: docs/09-roadmap.md
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

Exactly one function in the codebase creates a session row, and it checks that the user's status is active before it does.

This exists as its own ticket because docs/09 names the bug it prevents, which Gitea shipped: an approval-gated instance where one code path created a session without checking approval, and a pending applicant walked straight in. The defense is structural rather than careful review. If there is one door, the check is on the door.

Approval mode makes this sharper, because a pre-user session is a legitimate session for a user that does not exist yet. The function must distinguish "no user, by design" from "user exists but is not active".

## Acceptance criteria

- [ ] Exactly one function constructs a session row, enforced by a test or a lint that fails on a second one
- [ ] The function refuses to create a user session for a status other than active
- [ ] The OIDC callback path is tested explicitly against a pending, a suspended, and a deactivated user
- [ ] Creating a pre-user session is a distinct call that cannot accidentally produce a member session
