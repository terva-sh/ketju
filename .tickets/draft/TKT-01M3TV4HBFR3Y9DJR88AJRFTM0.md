---
schema: 3
id: TKT-01M3TV4HBFR3Y9DJR88AJRFTM0
title: Move focus and key listeners into layout effects in menus and popovers
type: bug
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:client
  - effort:S
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-01T04:21:36Z
updated_at: 2026-10-01T04:21:36Z
created_by:
  id: agent:claude-code/phase2-run-ts0zv9
  name: ""
updated_by:
  id: agent:claude-code/phase2-run-ts0zv9
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
