---
schema: 3
id: TKT-01M3V7CFJ5RYC1RCZRNYD8RSQS
title: Move focus into layout effects in the admin and apply screens
type: bug
status: in-progress
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
references:
  - ref: pr:360
    path: null
claim:
  actor: agent:claude-code/phase4-run-7CFJ5
  branch: run/7cfj5-layout-focus
  worktree: /home/sothr/workspace/git.local.sothr.com/terva-sh/ketju/.claude/worktrees/agent-adf21bb4c458f1a36
  commit: 74105a52c1f7a62b9832ad83ab1134b8615b1e57
  session: null
  claimed_at: 2026-10-02T23:47:17Z
  expires_at: null
archive: null
created_at: 2026-10-01T07:55:39Z
updated_at: 2026-10-03T00:13:30Z
created_by:
  id: agent:claude-code/phase3-groom-polish
  name: ""
updated_by:
  id: agent:claude-code/phase4-run-7CFJ5
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
- [ ] The PR lists each of the eight components with its Vitest case and whether the case failed on main, and a ticket note says why each component left on useEffect is safe
- [ ] The admin-agents, admin-suspension, admin-applications, admin-forms, admin-rename and apply e2e specs pass at 390 px and 1440 px, with one axe scan per test
- [ ] The dist from go generate ./internal/web/spa is committed, npm run budget keeps the reader within 120000 B gzip, and the PR states the change to the counted chunks

## Implementation plan

Groomed 2026-10-02 for the Phase 4 run (docs/15), against main at ed82f15.

### Settled in grooming

- **The rule** (`components/focus.ts`, TKT-01M3TMRCMJ, TKT-01M3TS0ZV9, #262, and TKT-01M3TV4HBF, done). Focus moves and key listeners go in a `useLayoutEffect`. That way they happen in the same task as the render that needs them, and a cleanup runs in the commit that removes the component. Preact runs a plain `useEffect` after the next frame, so a key pressed before that frame lands on whatever had focus.
- **Each component is checked, not converted blind.** This is the criterion. A case is written first and run against main. A component is changed only where the case fails there. A component whose case cannot fail on main keeps its `useEffect`, and the PR and a ticket note say why.
- **Not modal.** None of these is a modal dialog, so none takes `useModal`. TKT-01M3TV4HBF settled the same for menus and popovers.
- **`useEffect`s that do not move focus are left alone**, for example `AgentPanel`'s `[a.burst, a.refill_seconds]` field sync and the abort-on-unmount cleanups.

### Approach

What grooming read in each component, on main at ed82f15. "Expected" is a reading to test, not a finding.

1. **`screens/admin/common/ConfirmButton.tsx`**, the `[asking]` effect.
   - Asking replaces the action button with the question, so focus falls to the body until Cancel takes it. A key on the body acts on nothing.
   - Expected not to fail. Closing focuses the action again only when focus is on the body. Write the case for both directions anyway: Enter one microtask after asking must not confirm.
2. **`screens/admin/SuspensionPanel.tsx`**, the `[open]` effect (reason input). The opener stays focused until the frame. Expected not to fail for Enter, which opens a panel already open. Type a character one microtask after opening: on main it is lost rather than typed into the reason, so test whether it reaches the input.
3. **`screens/admin/NewAgentForm.tsx`**, the `[open]` effect (first field). The same shape as `SuspensionPanel`. Test a typed character one microtask after opening.
4. **`screens/admin/AgentPanel.tsx`.**
   - **`TokenPanel`'s `[token]` effect**, which focuses and selects the token. Until the frame, focus may stay on the control that issued the token. If Enter there can ask for another token, it revokes the one just shown, because issuing revokes a live credential first (`internal/auth/agentcred`). Test it first: this is the case that matters most here.
   - The `heading.current?.focus()` calls run inside event handlers, synchronously, and need no change.
5. **`screens/admin/UsersScreen.tsx`.**
   - The `[selected]` effect returns focus to a closed detail's row.
   - The two effects in the detail focus the title: once on mount for a new agent, and once after a rename once the detail has loaded.
   - After a rename the Rename button is gone, so a key lands on the body. Test Enter on the row-return path, where the Close control may still hold focus.
6. **`screens/admin/applications/ApplicationDetail.tsx`**, the `[notice]` effect. After a decision, the decision's button may still be focused and enabled until the frame. Enter would send a second decision. Test it.
7. **`screens/admin/forms/FormEditor.tsx`.**
   - The unconditioned effect that applies `focusNext` and `focusLead` after a move, add or remove. Expected to fail: Enter on a "Move up" control before the frame moves the field a second step.
   - The `[notice]` and `[conflict]` effects.
   - The `[open?.n, !!open?.form]` heading effect in the version viewer.
8. **`forms/FormView.tsx`**, the `[refusals]` effect, which focuses the first refused field. Until the frame, focus stays on Submit, and Enter submits again. Expected to fail. This is the one component outside the admin chunk: the application screens' chunk, which the budget counts.
9. For each component whose case fails on main: change `useEffect` to `useLayoutEffect` with the same dependencies. Keep reading the opener before focus moves, where the effect does that.
10. **Build.** Run `go generate ./internal/web/spa` and `npm run budget`.

**Reader bundle.** The admin chunk is not counted (`scripts/bundle.mjs` `adminPrefix`). `forms/FormView.tsx` is in the application screens' lazy chunk, which is counted. The change there is a hook rename, about ±10 B gzip. Nothing in the reader's entry changes. Measure it and state it in the PR.

### Tests

Each case mounts the component through the press that opens it, outside `act`, then dispatches the key one microtask later, as `shortcuts.test.tsx`, `FlagDialog.test.tsx` and TKT-01M3TV4HBF's cases do. Each case is run against main first, and the PR names whether it failed there.

- `screens/admin/common/common.test.tsx` (`ConfirmButton`): "Enter one microtask after asking does not confirm", and "after Cancel, a key one microtask later lands on the action".
- `screens/admin/SuspensionControls.test.tsx` (`SuspensionPanel`): "a character typed one microtask after opening reaches the reason".
- `screens/admin/AgentControls.test.tsx` (`NewAgentForm`): "a character typed one microtask after opening reaches the handle".
- `screens/admin/AgentControls.test.tsx` (`AgentPanel`'s `TokenPanel`): "Enter one microtask after a token is issued does not issue another".
- `screens/admin/UsersScreen.test.tsx`: "a key one microtask after closing a detail lands on its row", and "after a rename, a key one microtask after the new detail loads lands on its title".
- `screens/admin/applications/ApplicationDetail.test.tsx`: "Enter one microtask after approving sends no second decision".
- `screens/admin/forms/FormEditor.test.tsx`: "Enter one microtask after Move up moves the field once", and "a notice or a conflict takes focus before the next key".
- `forms/FormView.test.tsx`: "Enter one microtask after a refused submit does not submit again, and focus is on the first refused field".
- **e2e.** The specs that cover these screens must still pass at 390 px and 1440 px, each with its one axe scan: `admin-agents`, `admin-suspension`, `admin-applications`, `admin-forms`, `admin-rename` and `apply`. No new e2e step is needed. Without CI load the race window closes before Playwright's next key arrives, so the Vitest cases are the proof, as in TKT-01M3TV4HBF.

### Out of scope

- The reader bundle's menus and popovers, done in TKT-01M3TV4HBF.
- `useOwn`'s refocus effect, which TKT-01M3TV4HBF settled is safe.

## Notes

**agent:claude-code/phase4-run-7CFJ5** at 2026-10-03T00:13:30Z

Each component was checked with a Vitest case written first and run against main's code, before its change. The cases use pressRaw (testing.tsx): a keydown dispatched outside act on whatever has focus, with the browser's default action emulated. Enter clicks a focused button or submits a focused field's form, and a character is typed into a focused text field. The key goes one microtask after the press, render or answer that moves focus.

What "failed on main" means here: at that moment focus was not where the component moves it. The key then either landed on nothing (lost) or acted on another control. Where the result was the wrong control, that is named below.

### Changed to useLayoutEffect (the case failed on main)

1. `ConfirmButton` (`common.test.tsx`).
   - "Enter one microtask after asking does not confirm, and lands on Cancel": on main focus was on the body, so the key was lost.
   - "after Cancel, a key one microtask later lands on the action": on main the key landed on the body and was lost.
2. `SuspensionPanel` (`SuspensionControls.test.tsx`).
   - "a character typed one microtask after opening reaches the reason": on main the character was lost.
   - "a key one microtask after Cancel lands on Suspend again": on main the key landed on the body.
3. `NewAgentForm` (`AgentControls.test.tsx`), "a character typed one microtask after opening reaches the handle": on main the character was lost.
4. `UsersScreen`: all three focus effects.
   - "a key one microtask after closing a detail lands on its row" (`UsersScreen.test.tsx`): on main the key landed on the body.
   - "after a rename, a key one microtask after the new detail loads lands on its title" (`AdminUserRename.test.tsx`): on main the key landed on the body.
   - "a key one microtask after creating a tokenless agent lands on its title" (`AgentControls.test.tsx`): on main the key landed on the body.
5. `ApplicationDetail` (`Decision.test.tsx`), "Enter one microtask after approving sends no second decision, and lands on the notice": on main the key landed on the body. No second decision could be sent, because the controls go with the decision.
6. `FormEditor`: the focusNext/focusLead effect, the notice, the conflict, and the version viewer's heading (`FormEditor.test.tsx`, "a key pressed before the next frame"). Each landed on a wrong control on main:
   - Enter on Add field added a second field.
   - The notice and the conflict: the key landed on Save as version.
   - The version heading: Enter on View version 1 asked for the version again.
7. `FormView` (`FormView.test.tsx`), "Enter one microtask after a refused submit does not submit again, and focus is on the first refused field": on main Enter landed on Submit and submitted again. FormView is in the apply chunk, which the budget counts.

### Left on useEffect, and why each is safe

- `AgentPanel`'s `TokenPanel` `[token]` effect. The case "Enter one microtask after a token is issued does not issue another" passes on main. The issue's answer sets the token, clears `pending` in `finally`, and applies the credential, all in the same task. The re-render that follows runs TokenPanel's pending effect before rendering it, which is Preact's rule for a component re-rendered with an effect still pending. So the token input has focus before a key can arrive.
- `FormEditor`'s Move up path shares the converted focusNext effect. Its own case ("one microtask after Move up lands on the moved field's Move up, which moved it once") passes on main in happy-dom, which keeps focus on a moved node. It stays as a guard.
- Effects that move no focus were left alone, as grooming settled:
  - `AgentPanel`'s mount handover and field sync;
  - `UsersScreen`'s live-flag cleanup and prefix sync;
  - `FormEditor`'s preview timer and abort cleanups;
  - `ApplicationDetail`'s abort cleanup.

### Mutation check

Reverting `FormEditor`'s version-heading effect alone to useEffect fails its case.

### e2e and bundle

The admin-agents, admin-suspension, admin-applications, admin-forms, admin-rename and apply specs pass at 390 and 1440 px (62 tests). Five serial tests in admin-agents and admin-forms had no axe scan of their own and now scan their state.

Reader bundle 79983 B to 79979 B gzip (−4 B; ApplyRoot −11 B).
