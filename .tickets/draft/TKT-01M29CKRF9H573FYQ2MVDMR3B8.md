---
schema: 3
id: TKT-01M29CKRF9H573FYQ2MVDMR3B8
title: Map IdP group claims onto local groups
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
  - TKT-01M29BYYHGZY39T1F2NGJX8MED
blocks_on: none
references:
  - ref: design:identity-roles
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

`claims.groups` names the claim and `group_map` translates its values onto local groups, re-evaluated at every login. This is how an organization manages its moderators from its directory instead of in two places.

Mapped memberships are marked with source `idp` so re-evaluation never clobbers a membership an admin added by hand. Losing a claim removes only the mapped memberships.

## Acceptance criteria

- [ ] Mapped memberships are created and removed on each login according to the current claim
- [ ] A manually added membership survives a login that does not assert it
- [ ] An unmapped claim value is ignored rather than creating a group
- [ ] Group changes through mapping are recorded in audit_log
