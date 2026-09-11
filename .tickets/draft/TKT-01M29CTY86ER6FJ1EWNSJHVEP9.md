---
schema: 3
id: TKT-01M29CTY86ER6FJ1EWNSJHVEP9
title: Build the notifications UI and alerts tab
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
  - TKT-01M29CRJ2TJG29ZBV037PYC943
  - TKT-01M29CEE4KNQBFP49FVSCRNZA4
blocks_on: none
references:
  - ref: design:mobile-nav
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

The notification list with its API, a badge in both navigation layouts, and the Alerts tab on phones.

Each notification links to the exact ordinal that caused it, which is the only thing anybody wants from it. Marking read happens on view for the ones displayed, with an explicit mark-all.

In-app only in v1, with no email and no push, so this list is the entire notification story and it needs to be good enough to bring someone back.

## Acceptance criteria

- [ ] Each notification links to the post that caused it, landing on the right page and anchor
- [ ] Badge counts match the list and clear correctly on read
- [ ] The Alerts tab works at 390 px with the same content as desktop
- [ ] An empty state explains what would appear there
