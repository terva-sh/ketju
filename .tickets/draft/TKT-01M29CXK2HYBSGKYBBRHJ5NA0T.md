---
schema: 3
id: TKT-01M29CXK2HYBSGKYBBRHJ5NA0T
title: Build the admin site settings screen
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:moderation
  - area:client
  - effort:M
assignees: []
milestone: phase-2
parent: TKT-01M29BN36JH4M1W5819D06PGPP
origin: null
dependencies:
  - TKT-01M29BYYJ0WJ3C7RRZQMDRP5ZB
  - TKT-01M29CEE4KNQBFP49FVSCRNZA4
blocks_on: none
references:
  - ref: design:config
    path: docs/02-architecture.md
claim: null
archive: null
created_at: 2026-09-11T23:30:21Z
updated_at: 2026-09-11T23:30:21Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:terva/mieli
  name: Mieli
extensions: {}
---

## Description

The runtime settings from the `settings` table: site name, registration mode, edit and delete windows, the flag auto-hide threshold, `public_edit_history`, default page size, and slow-mode defaults.

The screen also has to explain the split. An admin looking for the OIDC client secret here should be told it lives in config.toml and why, rather than concluding the feature is missing.

## Acceptance criteria

- [ ] Every documented runtime setting is editable and takes effect without a restart
- [ ] Each change records who made it and when
- [ ] Settings that live in config.toml are named on the screen with the reason they are not editable here
- [ ] An invalid value is refused with a message naming the acceptable range
