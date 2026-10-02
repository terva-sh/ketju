---
schema: 3
id: TKT-01M3V7CFJ5RYC1RCZRNYD8RSQS
title: Move focus into layout effects in the admin and apply screens
type: bug
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
created_at: 2026-10-01T07:55:39Z
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

Found while grooming TKT-01M3TV4HBF (Move focus and key listeners into layout effects in menus and popovers) on 2026-10-01, against main at a599b48.

TKT-01M3TV4HBF moves focus and key listeners into layout effects in the reader bundle's menus and popovers. Outside the reader, several components in the lazy admin and apply chunks still move focus in a plain `useEffect`. Preact runs that effect after the next frame, so a key pressed before it lands on the control that opened them (TKT-01M3TMRCMJ, TKT-01M3TS0ZV9):
- `screens/admin/common/ConfirmButton.tsx` (Cancel on asking, the action on closing)
- `screens/admin/SuspensionPanel.tsx`
- `screens/admin/NewAgentForm.tsx`
- `screens/admin/AgentPanel.tsx`
- `screens/admin/UsersScreen.tsx`
- `screens/admin/applications/ApplicationDetail.tsx`
- `screens/admin/forms/FormEditor.tsx`
- `forms/FormView.tsx`

None of these is known to have lost a key. They have the same shape as the components that did.

### What to do
For each one, check whether a key pressed before the frame can act on the wrong control. Where it can, move the focus into a `useLayoutEffect` and add a Vitest case that opens the component outside `act` and presses the key one microtask later. Each case must fail before the change.

## Acceptance criteria

- [ ] Each listed component is checked, and every one where a key pressed before the frame acts on the wrong control moves its focus into a useLayoutEffect, with a Vitest case that fails before the change
