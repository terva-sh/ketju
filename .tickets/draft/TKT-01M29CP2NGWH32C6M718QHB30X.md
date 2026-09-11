---
schema: 3
id: TKT-01M29CP2NGWH32C6M718QHB30X
title: Build the apply and status screens
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:client
  - effort:M
assignees: []
milestone: phase-2
parent: TKT-01M29BKBM38VTYY4Y91018GTH6
origin: null
dependencies:
  - TKT-01M29CEE4KNQBFP49FVSCRNZA4
  - TKT-01M29CKRG9XYJS4HM1VY1QSJ1V
blocks_on: none
references:
  - ref: design:forms
    path: docs/05-identity-and-accounts.md
claim: null
archive: null
created_at: 2026-09-11T23:26:15Z
updated_at: 2026-09-11T23:26:15Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:terva/mieli
  name: Mieli
extensions: {}
---

## Description

The client rendering the registration form from its stored schema, plus the status page an applicant returns to.

This screen is a stranger's first impression of the community, seen while they are being judged. It should look like the rest of the site rather than like an admin tool, and it has to work at 390 px because most people will meet it on a phone.

## Acceptance criteria

- [ ] The form renders from the schema with no field types hard-coded in the client
- [ ] Server-side validation errors attach to the right fields
- [ ] A locked prefilled field is visibly locked and its value is not submitted as editable
- [ ] Both screens work at 390 px and 1440 px
