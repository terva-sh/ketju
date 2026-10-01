---
schema: 3
id: TKT-01M3V7GE5986PGNHTJJBZT1Z0H
title: Add the / shortcut that opens search on desktop
type: task
status: draft
status_reason: null
priority: low
due_on: null
labels:
  - area:client
  - area:search
  - effort:S
assignees: []
milestone: null
parent: TKT-01M29BN367F3YQZESM63VN26GP
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-01T07:57:49Z
updated_at: 2026-10-01T07:57:49Z
created_by:
  id: agent:claude-code/phase3-groom-search
  name: ""
updated_by:
  id: agent:claude-code/phase3-groom-search
  name: ""
extensions: {}
---

## Description

The desktop mockup's search control shows a `/` key hint (`docs/mockups/FrontDark.dc.html`). The built top bar links to `/search` with no key. docs/06's shortcut list (`j`, `k`, `g`, `u`, `n`, `?`) does not include `/`, and `keyboard/shortcuts.ts` installs the shortcuts on the thread page only.

Filed in Phase 3 search grooming (TKT-01M3V3BVPY) and left out of TKT-01M3V70X0F (Build the search results screen). Before building it:
- decide whether `/` belongs in docs/06's list;
- decide whether a shortcut outside the thread page fits docs/06, given "each shortcut has a visible control" (the top bar's search link would be that control);
- check that `ignored()` keeps it out of the composer and inputs, and that the help overlay lists it.

Not a docs/09 Phase 3 deliverable, so no milestone.
