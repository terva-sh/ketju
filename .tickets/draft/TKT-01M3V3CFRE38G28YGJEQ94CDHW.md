---
schema: 3
id: TKT-01M3V3CFRE38G28YGJEQ94CDHW
title: Check Phase 3's exit criterion with named end-to-end tests
type: task
status: draft
status_reason: null
priority: high
due_on: null
labels:
  - area:test
  - effort:M
assignees: []
milestone: phase-3
parent: TKT-01M29BN36D34YVKVRPNQD3PHDV
origin: null
dependencies:
  - TKT-01M29CTY4JB7MMAWHVDT4BTCZM
  - TKT-01M29CTY7K2YTY0V54YKJTKX1W
  - TKT-01M29CTY86ER6FJ1EWNSJHVEP9
  - TKT-01M29CTY8TB2CHCBQH3ABHCG3Q
  - TKT-01M29CTYA0E5Y06WWYY2ETBF8V
  - TKT-01M29CYS9W8GAWNQVHFH7HZ7AR
  - TKT-01M29CGBZDVGWNDTKHJFHHSGY5
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-01T06:45:45Z
updated_at: 2026-10-01T06:45:53Z
created_by:
  id: agent:claude-code/phase3-run-orchestrator
  name: ""
updated_by:
  id: agent:claude-code/phase3-run-orchestrator
  name: ""
extensions: {}
---

## Description

docs/09 gives Phase 3's exit criterion as "daily-driver quality for an active community, on a phone as much as on a desktop". Phase 2's exit was checked by named tests (TKT-01M3NTSCMN, CI step "Phase 2 exit tests, by name"). This ticket does the same for Phase 3 once its features land.

Grooming names the tests. At least, a member at 390 px and at 1440 px:
- reads by the unread lens and jumps to the first unread;
- finds a post by search, with a snippet;
- sees a new reply arrive live through the pill;
- gets a notification for a mention and a watched thread;
- attaches an image and sees its thumbnail;
- moves between pages with the phone page picker.

Each runs with axe, and none needs an `/auth/*` call beyond `paceAuth`.
