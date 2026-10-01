---
schema: 3
id: TKT-01M3TV4HBFR3Y9DJR88AJRFTM0
title: Move focus and key listeners into layout effects in menus and popovers
type: bug
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area:client
  - effort:S
assignees: []
milestone: phase-3
parent: null
origin: null
dependencies: []
blocks_on: none
references:
  - ref: pr:292
    path: null
claim: null
archive: null
created_at: 2026-10-01T04:21:36Z
updated_at: 2026-10-01T11:15:27Z
created_by:
  id: agent:claude-code/phase2-run-ts0zv9
  name: ""
updated_by:
  id: agent:claude-code/phase3-tv4hbf
  name: ""
extensions: {}
---

## Description

Found while grooming TKT-01M3TS0ZV9 (Take Escape in the flag, moderate and forum-picker dialogs before focus) on 2026-10-01, against main at 61201be.

Four more components move focus, or add their Escape listener, in a `useEffect`. Preact runs a `useEffect` after the next frame, and it runs the cleanup of the previous one at that point too. So the same race that TKT-01M3TMRCMJ fixed in `HelpOverlay` applies here, from both ends:

- `web/client/src/shell/MoreMenu.tsx`, the phone top bar's overflow. Focus moves to the first item, and the document `keydown` and `pointerdown` listeners are added, in a `useEffect` on `[open]`. An Escape pressed straight after opening the menu is not caught. After an item is chosen, the listeners stay until the next frame. An Escape in that window moves focus to the More button. The listener does not check `defaultPrevented`, so it does this even when a panel the item opened has already taken the Escape. From the code (not run): an Escape pressed straight after choosing Lock closes the panel, and this listener then puts focus on the More button rather than the thread title.
- `web/client/src/components/thread/PostMore.tsx`, a post's More list on a phone. It has the same shape as MoreMenu. Its stale listener does check `defaultPrevented`. A throwaway Vitest probe on 2026-10-01 showed that this listener took an Escape pressed straight after choosing Flag: focus went to the More button and the flag dialog stayed open. TKT-01M3TS0ZV9 makes the flag dialog take its keys in the capture phase, so that dialog no longer loses the key. The list itself is unchanged.
- `web/client/src/components/thread/PostPopover.tsx`. Focus moves into the popover, and its Escape listener goes on `window`, in a `useEffect`.
- `DeleteConfirm` in `web/client/src/components/thread/OwnControls.tsx`. Focus moves to Cancel in a `useEffect`. The comment says this is "so a second Enter does not delete". An Enter pressed before the frame still lands on the control that opened the confirmation.

`ComposerSheet` was checked too. It has no race, because `ComposerBody` focuses the textarea in a layout effect. Its Escape and Tab are still caught only on the sheet, so after a click on the quoted post's text, Escape does nothing. Whether the sheet should take Escape wherever focus is, as HelpOverlay does, is a separate question.

### What to do

For each component, move focus and listener setup into a `useLayoutEffect`. Have MoreMenu's Escape check `defaultPrevented`. Where a component is a modal dialog, use `useModal` from `components/focus.ts`, which TKT-01M3TS0ZV9 adds. For each component, add a Vitest case that opens it outside `act` and presses the key one microtask later. Each case must fail before the change.

## Acceptance criteria

- [x] MoreMenu, PostMore, PostPopover, DeleteConfirm and PostEditor's status block move focus and add their key listeners in a useLayoutEffect, and remove them in the commit that closes them
- [x] MoreMenu's and PostPopover's Escape ignore a key already taken (defaultPrevented) and mark one they take
- [x] Each component has a Vitest case that opens it outside act and presses the key one microtask later, and the PR names the run in which each case failed against main
- [x] On a phone an Escape straight after choosing Lock leaves focus on the thread's heading (e2e/thread-moderation.spec.ts), and both e2e projects pass with their axe scans
- [x] The dist from go generate ./internal/web/spa is committed, npm run budget keeps the reader within 70000 B gzip, and the PR states the reader's growth

## Implementation plan

Groomed 2026-10-01 for the Phase 3 run (docs/14), against main at a599b48. Revised at claim on 2026-10-01, against main at ccb234d: the Settled and Approach sections are unchanged, and the Tests section changes two case names, for the reasons given there.

### Settled in grooming

- **The rule** (`components/focus.ts`, TKT-01M3TMRCMJ, TKT-01M3TS0ZV9 and #262). Focus moves and key listeners go in a `useLayoutEffect`, so they happen in the same task as the press that drew the component, and their cleanup runs in the commit that removes it. A modal dialog uses `useModal`, which listens on the document in the capture phase. A handler that takes Escape checks `defaultPrevented` and calls `preventDefault`, so a dialog that took the key first keeps it.
- **Scope: the reader-bundle components this ticket names, plus one more of the same shape found in grooming:**
  - `shell/MoreMenu.tsx`
  - `components/thread/PostMore.tsx`
  - `components/thread/PostPopover.tsx`
  - `DeleteConfirm` in `components/thread/OwnControls.tsx`
  - in the same file, `PostEditor`'s focus on its status block while the source loads (`useEffect` on `[loaded]`), which has `DeleteConfirm`'s shape
- **Not modal.** `MoreMenu` and `PostMore` are a menu and a disclosure, and `PostPopover` is a non-modal `role="dialog"` with no focus trap. None of them takes `useModal`. Each keeps its own listener, moved into a layout effect.
- **`useOwn`'s refocus effect is left alone.** It runs after the editor or confirmation has closed, and it puts focus back on the post's control. A key pressed before that frame lands on the body, where nothing acts on it. This is recorded so the next reader does not re-check it.
- **The admin and apply chunks have the same pattern**, in `ConfirmButton`, `SuspensionPanel`, `NewAgentForm`, `AgentPanel`, `UsersScreen`, `ApplicationDetail`, `FormEditor` and `FormView`. They are filed as TKT-01M3V7CFJ5 (Move focus into layout effects in the admin and apply screens), a draft with no milestone, because nothing in Phase 3 needs them.

### Approach

1. **`MoreMenu`.** Move the `[open]` effect to `useLayoutEffect`. The Escape handler returns early on `e.defaultPrevented`, and calls `e.preventDefault()` on a key it takes.
2. **`PostMore`.** Move the `[open]` effect to `useLayoutEffect`. Its Escape already checks `defaultPrevented`.
3. **`PostPopover`.**
   - Move the mount effect to `useLayoutEffect`. That covers focusing the box, the `keydown` listener, and returning focus on close.
   - The listener moves from `window` to `document` and ignores a key already taken (`defaultPrevented`). It calls `preventDefault` when it closes.
   - The opener is still read before focus moves.
4. **`DeleteConfirm`.** `useLayoutEffect(() => cancel.current?.focus(), [])`.
5. **`PostEditor`.** The status-block focus becomes a `useLayoutEffect` on `[loaded]`.
6. **Build.** Run `go generate ./internal/web/spa` and `npm run budget`.

**Reader bundle.** This is in the reader, with about ±20 B gzip.

### Tests

The Vitest cases mount the component through the press that opens it outside `act`, then dispatch the key one microtask later, as `shortcuts.test.tsx` (TKT-01M3TMRCMJ) and `FlagDialog.test.tsx` (TKT-01M3TS0ZV9) do. Run each case against main first and record that it fails there. The PR names the failing run.

- `MoreMenu`, in a new `shell/MoreMenu.test.tsx`:
  - "Escape one microtask after opening closes the menu and focuses More".
  - "after choosing an item, its listeners are gone: a later Escape leaves focus where the item put it". Revised at claim: the groomed name said "an Escape another handler took". A taken Escape is the third case's, so this one sends an Escape nobody takes, which fails on main only because the stale listener is still there.
  - "Escape already taken (defaultPrevented) does not move focus".
- `PostMore`, in a new `components/thread/PostMore.test.tsx`:
  - "Escape one microtask after opening closes the list".
  - "after choosing an item, its listener is gone: a later Escape leaves focus where the item put it". Revised at claim: the groomed case was "Escape one microtask after choosing Flag leaves the flag dialog open and focus inside it". Since TKT-01M3TS0ZV9 the flag dialog takes Escape in the capture phase and closes, and `FlagDialog.test.tsx` ("closes the dialog opened from a phone's More list") already pins that and passes on main, so it cannot fail first. The new case chooses an item whose target does not take Escape, which is where PostMore's stale listener still acted on main.
- `PostPopover`, in a new `components/thread/PostPopover.test.tsx`:
  - "focus is in the popover one microtask after it opens, and Escape then closes it".
  - "a key a dialog already took does not close the popover".
- `DeleteConfirm`, in `PostControls.test.tsx`: "Enter one microtask after delete opens the confirmation lands on Cancel and deletes nothing".
- `PostEditor`, in `PostControls.test.tsx`: "while the source loads, a key one microtask after opening lands in the status block".
- **e2e.** In `e2e/thread-moderation.spec.ts`, the step "Escape pressed straight after opening closes the panel" now also asserts that focus is on the thread's heading rather than on More. It asserts this in both projects, since the panel returns focus to the heading on desktop too. The step runs at 390 px and 1440 px, and the spec's axe tests pass. Without CI load the race window closes before Playwright's next key arrives, so this step guards the outcome and the Vitest cases prove the race.

### Out of scope

- The admin and apply chunks' focus effects: TKT-01M3V7CFJ5 (Move focus into layout effects in the admin and apply screens), filed in grooming with no milestone.
- Whether the composer sheet should take Escape wherever focus is, which the description raises. It is a separate question, with no Phase 3 deliverable behind it.

## Notes

**agent:claude-code/phase3-tv4hbf** at 2026-10-01T11:11:54Z

The new Vitest cases, run against main's components before any change (2026-10-01, branch run/tv4hbf-layout-focus at main ccb234d plus the claim commit, `npx vitest run src/shell/MoreMenu.test.tsx src/components/thread/PostMore.test.tsx src/components/thread/PostPopover.test.tsx src/components/thread/PostControls.test.tsx`). Result: 9 failed, 28 passed. Every new case failed, at the assertion the race predicts:

- MoreMenu, "Escape one microtask after opening closes the menu and focuses More": focus was still on More, not on the first item, because the focus move waited for the frame.
- MoreMenu, "after choosing an item, its listeners are gone: a later Escape leaves focus where the item put it": focus went to More. The closed menu's listener was still on the document and took the key.
- MoreMenu, "Escape already taken (defaultPrevented) does not move focus": the menu closed. Its listener ignored defaultPrevented.
- PostMore, "Escape one microtask after opening closes the list": the list stayed open, because no listener was attached yet.
- PostMore, "after choosing an item, its listener is gone: a later Escape leaves focus where the item put it": focus went to the More button, from the closed list's stale listener.
- PostPopover, "focus is in the popover one microtask after it opens, and Escape then closes it": focus was still on the opener.
- PostPopover, "a key a dialog already took does not close the popover": the popover closed. The window listener ignored defaultPrevented.
- DeleteConfirm, "Enter one microtask after delete opens the confirmation lands on Cancel and deletes nothing": focus was on the body. The delete control had gone and Cancel was not focused yet.
- PostEditor, "while the source loads, a key one microtask after opening lands in the status block": focus was outside the editor block.

After the change, all nine pass, and the whole client suite passes with 2060 tests in 72 files.

e2e: the new assertion in e2e/thread-moderation.spec.ts ("a moderator locks and unlocks a thread": after Escape, the h1 has focus) passed against main's dist too, in an unloaded local run on port 8793. Playwright's round trips let a frame pass between choosing Lock and pressing Escape, so the race window had already closed. The step guards the outcome on a loaded runner. The Vitest cases above prove the race. With the new dist, the full suite passed locally: 193 passed, and 11 were skipped by the specs' own width gates. That includes the lock step and the moderation axe scans at 390 px and 1440 px.

Reader bundle (npm run budget): 60465 B on main, 60464 B with this change, so -1 B against the 70000 B budget.

## Summary

MoreMenu, PostMore, PostPopover, DeleteConfirm and PostEditor's status block now move focus, and add their key listeners, in a useLayoutEffect. That happens in the same task as the press that drew them, and the cleanup runs in the commit that closes them. MoreMenu's Escape now ignores a key already taken (defaultPrevented) and marks one it takes. PostPopover's Escape moved from window to document and does the same. PostMore already did. None of the four takes useModal, as grooming settled.

Tests: nine Vitest cases open each component outside act and press the key one microtask later. They are in shell/MoreMenu.test.tsx, components/thread/PostMore.test.tsx, components/thread/PostPopover.test.tsx, and the "a key pressed straight after opening" block in PostControls.test.tsx. All nine failed against main's components first (see the note of 2026-10-01). Two case names changed at claim, and the revised plan gives the reasons: the groomed Flag case already passes on main since TKT-01M3TS0ZV9. e2e/thread-moderation.spec.ts now asserts that the heading has focus after an Escape straight after choosing Lock, in both projects.

Reader bundle: 60465 B on main, 60464 B after the change (-1 B of 70000 B).

Not done here: the admin and apply chunks, which are TKT-01M3V7CFJ5 (Move focus into layout effects in the admin and apply screens), and the composer sheet's Escape question, as the plan says.
