---
schema: 3
id: TKT-01M29CP2KEEB60VEVBPY9H2GBD
title: Implement the declarative form schema and server-side validation
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
parent: TKT-01M29BKBM38VTYY4Y91018GTH6
origin: null
dependencies:
  - TKT-01M29CKRDQHB528ZW1FFBN4SA1
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

The field list from docs/05 as data: `text`, `textarea`, `email`, `checkbox`, `select`, and `markdown_notice`, with `required`, `max`, `from_claim`, and `locked`. Validation runs server-side from the stored schema.

The form is data rather than code because three renderers need it: the Preact client, the future `html/template` client, and the validator. Hand-written per-field validation would have to be written three times and would drift twice.

`locked` with `from_claim` means a field is prefilled from the IdP and cannot be edited, so the server must reject a submitted value for it rather than trusting the form to have disabled the input.

## Acceptance criteria

- [ ] Every field type validates server-side from the schema with no per-field hand-written code
- [ ] A locked field ignores any submitted value and takes the claim value
- [ ] A required field missing from a submission returns a field-scoped error the client can attach to the input
- [ ] An unknown field type in a stored schema fails loudly at load rather than rendering nothing
