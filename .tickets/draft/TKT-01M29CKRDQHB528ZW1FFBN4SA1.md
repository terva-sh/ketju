---
schema: 3
id: TKT-01M29CKRDQHB528ZW1FFBN4SA1
title: Add the applications and forms schema
type: task
status: draft
status_reason: null
priority: high
due_on: null
labels:
  - area:db
  - area:identity
  - effort:S
assignees: []
milestone: phase-2
parent: TKT-01M29BKBKV6HH8W0DN6QXQNZ0R
origin: null
dependencies:
  - TKT-01M29BW9Q17KEBMHJ91G6QHW36
blocks_on: none
references:
  - ref: design:data-model-applications
    path: docs/04-data-model.md
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

`applications` with a nullable identity_id, the form version it was answered against, the answers as JSON, and its decision fields. `forms` and `form_versions` holding the declarative schema.

Storing `form_version` on the application is what keeps an old application readable after an admin edits the form. Without it, a form edit silently rewrites the meaning of every answer already submitted.

## Acceptance criteria

- [ ] An application records the exact form version it answered
- [ ] Editing a form creates a new version and leaves old versions intact
- [ ] Application status is constrained to pending, approved, denied, and withdrawn
- [ ] An identity can be marked blocked while keeping user_id NULL
