---
schema: 3
id: TKT-01M3V6XTYGE9R6MRRH27HYSMHM
title: Add handle change and self-deactivation to the settings screen
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:client
  - effort:S
assignees: []
milestone: phase-4
parent: null
origin: null
dependencies:
  - TKT-01M29CXK4E23MC6DWK4VBX6DXS
  - TKT-01M29CCC0X7SY4SHJ46FV40GNW
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-01T07:47:39Z
updated_at: 2026-10-02T15:42:10Z
created_by:
  id: agent:claude-code/phase3-groom-pages
  name: ""
updated_by:
  id: agent:claude-code/phase4-run-orchestrator
  name: ""
extensions: {}
---

## Description

`PUT /settings/handle` (TKT-01M3NRP3W9) and `POST /settings/deactivate` (TKT-01M29CKRFS) are built, but no screen calls them. A member cannot change their own handle or deactivate their own account from the client.

Found while grooming TKT-01M29CXK4E (Build the user settings screens). That ticket keeps to the settings docs/09 lists, so these two controls are left to this ticket.

The handle control shows the 30-day cooldown from `next_change_at` or `available_at`, and the field errors `invalid_handle`, `reserved`, `taken` and `same`. Deactivation asks for confirmation, says how to come back (sign in and press Reactivate, docs/05 "Account lifecycle"), then loads `/`.
