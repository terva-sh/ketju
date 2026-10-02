---
schema: 3
id: TKT-01M3V9DWTNNAMF2WA9CWY0AJGQ
title: Let a member turn off the single-key shortcuts (WCAG 2.1.4)
type: task
status: draft
status_reason: null
priority: low
due_on: null
labels:
  - area:client
  - effort:S
assignees: []
milestone: phase-4
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-01T08:31:22Z
updated_at: 2026-10-02T15:42:10Z
created_by:
  id: agent:claude-code/phase3-groom-polish
  name: ""
updated_by:
  id: agent:claude-code/phase4-run-orchestrator
  name: ""
extensions: {}
---

## Description

Found while grooming TKT-01M3S3ZZSZ (Close the small visual gaps between the client and the mockups) on 2026-10-01, against main at a599b48. The mockups draw a `/` key hint beside the desktop search field. That ticket leaves `/` out until this one lands, a run decision in docs/06 "Paged, not infinite".

Every desktop shortcut in docs/06 is a single character with no modifier: `j`, `k`, `g`, `u`, `n` and `?`. WCAG 2.1 success criterion 2.1.4, Character Key Shortcuts (level A), asks for one of three things for such shortcuts:
- a way to turn them off;
- a way to remap them so they include a modifier;
- shortcuts that act only while their component has focus.

`keyboard/shortcuts.ts` ignores keys typed in a text field and is absent on phones. Neither meets 2.1.4, and nothing records why the client does not. axe does not test 2.1.4, so the accessibility pass (TKT-01M29CGBXK) passed without it.

### What to do
Offer a setting that turns the single-key shortcuts off, the simplest of the three. It belongs with the user settings screens (TKT-01M29CXK4E), and docs/06 should say so. Once the setting exists, the `/` shortcut (TKT-01M3V7GE59) can be added with it. Alternatively, record in docs/06 or docs/07 why the client does not meet 2.1.4.

## Acceptance criteria

- [ ] A member can turn the single-key shortcuts off in their settings, and with them off no single key acts; or docs/06 records why the client does not meet WCAG 2.1.4
