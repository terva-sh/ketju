---
schema: 3
id: TKT-01M29CXK4E23MC6DWK4VBX6DXS
title: Build the user settings screens
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:client
  - effort:M
assignees: []
milestone: phase-4
parent: TKT-01M29BN367F3YQZESM63VN26GP
origin: null
dependencies:
  - TKT-01M29CCC0X7SY4SHJ46FV40GNW
  - TKT-01M29CEE4KNQBFP49FVSCRNZA4
blocks_on: none
references:
  - ref: design:url-scheme
    path: docs/07-api-and-clients.md
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

`/settings`, `/settings/sessions`, and `/settings/identities`: timezone, page size, theme, the session list with sign out here and everywhere, and linked identities with add and remove.

The session list is the screen that makes server-side sessions worth their cost. Seeing every signed-in device and killing one is the feature people reach for when a laptop goes missing.

## Acceptance criteria

- [ ] Timezone, page size, and theme apply immediately and persist across devices
- [ ] The session list shows device, IP, and last use, with sign out here and everywhere
- [ ] Identities can be linked and unlinked, with the last one refused
- [ ] All three screens work at 390 px
