---
schema: 3
id: TKT-01M29CP2P1R6TTJHY28FP7SWPG
title: Build the admin form editor
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
  - TKT-01M29CKRDQHB528ZW1FFBN4SA1
  - TKT-01M29CEE4KNQBFP49FVSCRNZA4
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

A small admin UI for editing the registration form, writing a new version on each save while the old version stays for applications already submitted against it.

A preview of the applicant's view belongs here. An admin writing a screening question should see what a stranger sees before a stranger does.

## Acceptance criteria

- [ ] Saving creates a new form version and sets it active, leaving prior versions intact
- [ ] The editor previews the applicant view of the draft form
- [ ] A form that fails schema validation cannot be saved
- [ ] Old applications continue to render against the version they were answered on
