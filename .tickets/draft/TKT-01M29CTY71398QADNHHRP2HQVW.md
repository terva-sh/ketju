---
schema: 3
id: TKT-01M29CTY71398QADNHHRP2HQVW
title: Build the lens header and switcher in the client
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:client
  - area:views
  - effort:M
assignees: []
milestone: phase-3
parent: TKT-01M29BN36D34YVKVRPNQD3PHDV
origin: null
dependencies:
  - TKT-01M29CRJ1PY355KX3HX7A5N785
  - TKT-01M29CEE6V358M5KCBJGHD58HM
blocks_on: none
references:
  - ref: design:lenses
    path: docs/06-thread-views.md
claim: null
archive: null
created_at: 2026-09-11T23:28:54Z
updated_at: 2026-09-11T23:28:54Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:terva/mieli
  name: Mieli
extensions: {}
---

## Description

The header every lens shows: what is being filtered, how many posts are hidden, and a one-click return to the full thread at the same ordinal.

Returning at the same ordinal is the part that stops people getting lost. A reader who filters to one author, finds the post they wanted, and clicks back should be looking at that post in the full thread rather than at page 1.

## Acceptance criteria

- [ ] The header shows the active lens, the shown count, and the hidden count
- [ ] Returning to the full thread lands on the same ordinal the reader was viewing
- [ ] Switching lenses updates the URL so the view is shareable
- [ ] The header is legible at 390 px and reachable from the phone lens sheet
