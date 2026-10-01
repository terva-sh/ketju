---
schema: 3
id: TKT-01M3TS0ZV9V7WRQQ4QM5RZC3D1
title: Take Escape in the flag, moderate and forum-picker dialogs before focus
type: bug
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area:client
  - effort:S
assignees: []
milestone: phase-2
parent: null
origin: null
dependencies: []
blocks_on: none
references:
  - ref: pr:260
    path: null
  - ref: pr:262
    path: null
claim: null
archive: null
created_at: 2026-10-01T03:44:42Z
updated_at: 2026-10-01T04:46:53Z
created_by:
  id: agent:claude-code/phase2-run-tmrcmj
  name: ""
updated_by:
  id: agent:claude-code/phase2-run-ts0zv9
  name: ""
extensions: {}
---

## Description

Found while grooming TKT-01M3TMRCMJ (Close the shortcuts overlay on an Escape pressed before it takes focus) on 2026-10-01, against main at c3de983.

Three modal dialogs have the race that ticket fixes in `HelpOverlay`:

- `web/client/src/components/thread/FlagDialog.tsx`
- `web/client/src/components/thread/ModerateDialog.tsx`
- `web/client/src/screens/ForumPicker.tsx`

Each moves focus into itself in a `useEffect`, which Preact runs after the next frame (`afterNextFrame`: a `requestAnimationFrame` or a 35 ms timeout). Each catches Escape and Tab only in the dialog's `onKeyDown`. A key pressed between the render and that frame is dispatched on whatever opened the dialog, so the dialog never sees it. ForumPicker's own comment says focus goes inside "so Escape and Tab always reach the dialog's handler" (review on PR #86), but that holds only once a frame has passed. Under load, a frame can take hundreds of milliseconds.

### What to do

Apply what TKT-01M3TMRCMJ lands for HelpOverlay to each dialog: focus moved in a `useLayoutEffect`, and Escape and Tab taken on the document while the dialog is mounted. With three callers, extract a shared hook into `components/focus.ts` if the three fit one. For each dialog, add a Vitest case that opens the dialog outside `act` and presses Escape before a frame passes. It must fail before the change.

ComposerSheet already uses `useLayoutEffect` and is not affected in the same way. Check it anyway.

## Acceptance criteria

- [x] components/focus.ts exports useModal(root, onClose, enter), which runs enter (moving focus) in a useLayoutEffect and adds a capture-phase keydown listener on document for Escape (close unless defaultPrevented) and Tab (trapTab), removed by that effect's cleanup; HelpOverlay, FlagDialog and ForumPicker use it, and none keeps a keydown handler of its own
- [x] ModerateDialog moves focus to its first control in a useLayoutEffect, starts the move forum load in a separate useEffect, and stays inline and non-modal: Escape is still taken by the section's onKeyDown and Tab is not trapped
- [x] FlagDialog.test.tsx has a case that opens the dialog from a button outside act and dispatches Escape on document.activeElement one microtask later, asserting the dialog closed and focus is on returnTo, and a case that does the same from PostMore's flag item, asserting closed with focus on the More button; a ticket note records both failing on unchanged code
- [x] ForumIndex.test.tsx has the same race case for the picker opened from the index's new-thread button, asserting it closed and focus returned to the button; a ticket note records it failing on unchanged code
- [x] ThreadScreen.test.tsx has the same race case for the lock panel opened from the toolbar, asserting the panel closed, nothing was sent, and focus is on the h1, plus a case that Escape with focus on a post leaves the panel open; a ticket note records the race case failing on unchanged code
- [x] focus.test.ts tests useModal: with focus on the body Escape closes and Tab focuses the first control; a document keydown listener added before mount that preventDefaults Escape does not stop the close; after unmount Escape no longer calls onClose
- [x] e2e/flag.spec.ts, e2e/forum-index.spec.ts and e2e/thread-moderation.spec.ts each add an open-then-Escape step pressed back to back that expects the dialog gone (and, for flag and picker, the opener focused); every existing assertion is kept, and no retry, longer timeout, skip, focus wait or axe scan is added
- [x] Those three specs passed 20 of 20 repeats each on KETJU_E2E_PORT=18911 under CPU contention, with the command, load and counts in a ticket note, which also records the new steps run against the old dialogs
- [x] internal/web/spa/dist is regenerated with go generate ./internal/web/spa, and the PR states the reader bundle's gzip size before and after (under 70000 B)

## Implementation plan

Groomed 2026-10-01 against main at 61201be. That is the merge of TKT-01M3TMRCMJ's fix (PR #259).

### Settled in grooming

- **Fix the product, not the specs.** TKT-01M3TMRCMJ settled this for the same race, citing its own description and the run prompt. Its rule holds here too: no e2e spec gets a retry, a longer timeout, a skip or a wait for focus. Every existing assertion stays. The run prompt for this ticket (2026-10-01) repeats this: "Do not weaken any existing assertion".
- **Follow HelpOverlay's fix.** This ticket's description (filed 2026-10-01 by the TKT-01M3TMRCMJ lane) asks for the same fix: focus moves in a `useLayoutEffect`, and Escape and Tab are taken on the document while the dialog is mounted. With three callers, the shared part goes in a hook in `components/focus.ts`. The run prompt (2026-10-01) asks for the hook when there are three or more users of the pattern.
- **Every dialog gets a failing test first.** The description and the run prompt (2026-10-01) ask for a Vitest case per dialog. The case opens the dialog outside `act` and presses Escape before a frame passes. It must fail on the old code. Each dialog with an e2e spec also gains an "open, then Escape" step, pressed back to back.
- **The race is confirmed in all three, not inferred.** A throwaway Vitest probe ran during grooming against 61201be and was not committed. In each case it focused an opener button, clicked it outside `act`, and waited one microtask:
  - FlagDialog, ForumPicker and ModerateDialog (lock) were each in the DOM. `document.activeElement` was still the opener, and an Escape dispatched there left the dialog open. 60 ms later, focus was inside the still-open dialog.
  - On a phone, a post's More list opened FlagDialog. Choosing Flag left focus on the flag item. The Escape was then taken by `PostMore`'s document listener, which Preact removes only after the next frame. That listener moved focus to the More button and left the dialog open.
  - The probe was run again with FlagDialog patched to focus in a `useLayoutEffect` and take keys on the document. With a bubble-phase document listener, the phone case still failed, because PostMore's stale listener runs first and calls `preventDefault`. With a capture-phase listener, the dialog closed and focus went to the More button, its `returnTo`.
- **ModerateDialog stays inline and non-modal.** Its comment says: "It is inline, not modal: it keeps the thread head ... in view, and needs no focus trap" (TKT-01M3QGH66W). This ticket does not change that. ModerateDialog gets the layout-effect focus move, which closes the race. Its Escape stays on the panel, and Tab is not trapped. The modal hook would trap Tab, and it would close the panel on an Escape pressed anywhere on the page, including in the composer. That contradicts the panel's design, which belongs to the moderation ticket, not to this bug.
- **ComposerSheet is not affected.** It was checked, as the description asked. `ComposerBody` focuses the textarea in a layout effect when the sheet opens (`pending` seeded from `focus`), so no key can land outside the sheet first. It is not changed here. Its other difference from HelpOverlay is filed in TKT-01M3TV4HBF: Escape does nothing after a click on the quoted post's text.

### Approach

1. **Commit the failing tests first** (see Tests), run them on the unchanged dialogs, and record each failure in a ticket note with the assertion it fails on.
2. **Add `useModal` to `web/client/src/components/focus.ts`.**
   ```ts
   export function useModal(
     root: { current: HTMLElement | null },
     onClose: () => void,
     enter: (root: HTMLElement | null) => void | (() => void),
   ): void
   ```
   - `onClose` is kept in a ref that is updated on each render, so a prop that changes identity is always current.
   - One `useLayoutEffect` with no dependencies runs `enter(root.current)`, which moves focus and may return a cleanup that puts focus back. It then adds a `keydown` listener on `document` in the **capture** phase:
     - Escape that is not `defaultPrevented`: call `preventDefault()`, then `onClose()`.
     - Tab: call `trapTab(e, root.current)`.
   - The effect's cleanup removes the listener and then runs `enter`'s cleanup.
   - Capture, not bubble, so that a menu's listener left over from the frame before (PostMore, MoreMenu) cannot take the key first. The probe above showed that this matters. The update to the header comment of `focus.ts` names the hook and why it uses a layout effect and the capture phase.
3. **HelpOverlay** (`keyboard/HelpOverlay.tsx`) uses `useModal(root, close, enter)`. `enter` records the opener, focuses the first button, and returns the existing focus-back cleanup. Its own listener goes. Its behaviour is the same, except that it now listens in the capture phase. The overlay is modal, so it should take the key before anything behind it.
4. **FlagDialog** (`components/thread/FlagDialog.tsx`) uses `useModal(root, onClose, enter)`. `enter` focuses the first `input, button` and returns the existing `returnTo`-or-post cleanup. The dialog's `onKeyDown` and its attribute go. The `[received]` effect, which moves focus to Close after the thanks, stays a plain `useEffect`: it follows a network answer, not a key.
5. **ForumPicker** (`screens/ForumPicker.tsx`) uses `useModal(root, onClose, enter)`. `enter` focuses the first forum link, or Close when there is none, and returns nothing: the caller, `ForumIndex`'s `onClose`, still puts focus back on the opener. The `onKeyDown` and its attribute go. The comment that cites "review on PR #86" is reworded, because it now holds from the first commit, not after a frame.
6. **ModerateDialog** (`components/thread/ModerateDialog.tsx`) splits its mount effect in two:
   - a `useLayoutEffect` that focuses the first `textarea, input, select, button` and keeps the existing cleanup, which returns focus to `returnTo()` when focus was inside the panel or nowhere;
   - a `useEffect` that starts the forum load for `move`. A fetch does not need to run before paint.
   `onKeyDown` stays on the section. The `[offered]` effect stays as it is.
7. **e2e steps, back to back, with nothing between the open and the Escape:**
   - `e2e/flag.spec.ts`: before the existing open, open the dialog the same way (desktop: the post's flag action; phone: More, then flag). Press Escape, then expect `getByRole('dialog', { name: 'Flag post #3' })` to have count 0 and `opener` to be focused.
   - `e2e/forum-index.spec.ts`: after the existing Escape and `toHaveCount(0)`, open the picker again the same way (FAB on a phone, `.new-thread` on desktop). Press Escape, then expect count 0 and the opener focused.
   - `e2e/thread-moderation.spec.ts`, "a moderator locks and unlocks a thread": before the existing `action(page, isMobile, 'lock')`, run `action(...,'lock')`, press Escape, and expect `[data-moderate]` to have count 0. The step does not assert focus. On a phone, MoreMenu's stale listener may move focus to the More button in that window (TKT-01M3TV4HBF), and the unit test pins focus on desktop.
   No axe scan is added, so each test keeps at most one. The specs keep their stored sessions (alice, alice through `newContext`, mona).
8. **Rebuild and measure.** Run `go generate ./internal/web/spa` and commit `internal/web/spa/dist`. Report the reader bundle's gzip size before and after; it was 60440 B of 70000 B at grooming.
9. **Prove it under load.** Run the three specs 20 times each (`--repeat-each 20`) on `KETJU_E2E_PORT=18911`, under the load TKT-01M3TMRCMJ used: 24 busy loops on the 12-core host. Record the command, the load and the counts in a ticket note. Also run each new e2e step against the old dialogs built into the dist, and note what it showed.

### Tests

- `components/thread/FlagDialog.test.tsx`:
  - "Escape pressed before the dialog has taken focus still closes it". A harness button opens the dialog. Click it outside `act`, await one microtask, then dispatch Escape on `document.activeElement`. Assert that the dialog is gone and focus is on `returnTo`.
  - "a phone's More list does not take the Escape". `PostMore` with `onFlag` opens the dialog. Choose flag outside `act`, wait one microtask, then press Escape. Assert that the dialog is gone and focus is on the More button. This fails on the old code, and it would also fail with a bubble-phase listener.
- `screens/ForumIndex.test.tsx`, under "the forum picker": the same race case, opened through the index's new-thread button as the existing picker tests mount it. Assert that the picker is gone and focus is back on the button.
- `screens/ThreadScreen.test.tsx`, beside "Escape and Cancel close the panel and send nothing": the race case for the lock panel, opened from the toolbar action outside `act`. Assert that the panel is gone, nothing was sent, and focus is on the `h1`. A second case pins the non-modal decision: with the panel open and focus on a post, Escape leaves the panel open.
- `components/focus.test.ts`, for `useModal` (rendered with `h`, since the file is `.ts`):
  - With focus on the body, Escape closes and Tab moves focus to the first control.
  - A document `keydown` listener added before the dialog mounts, which calls `preventDefault` on Escape, does not stop the dialog from closing.
  - After unmount, Escape no longer calls `onClose`.
- Existing cases that must pass unchanged: HelpOverlay's in `keyboard/shortcuts.test.tsx`, including the microtask case from #259; the picker's "closes on Escape, and returns focus", "takes focus with nothing to offer" and "keeps Tab inside"; and every FlagDialog and moderate panel case.
- Mutations to run and record in the commit body:
  - `useLayoutEffect` changed back to `useEffect` in `useModal`: the three race cases fail.
  - The listener moved to the bubble phase: the PostMore case and the stale-listener case fail.
  - The listener never removed: the unmount case fails.
  - ModerateDialog's focus moved back to `useEffect`: its race case fails.
- Run `npx tsc --noEmit -p .` in `web/client`, because Vitest does not typecheck test files.

### Alternatives that lost

- **Bubble-phase document listener, as HelpOverlay has now.** On a phone, PostMore's document listener is registered first, and Preact removes it only after the next frame. It takes the Escape, calls `preventDefault`, and moves focus out of the dialog (probe above). Capture puts the open modal first, which is what `aria-modal` means.
- **Keep each dialog's `onKeyDown` and change only the effect.** A layout effect alone closes the race when the opener is a plain button. Escape would still do nothing when focus is on the body, after a click on the dialog's text. TKT-01M3TMRCMJ rejected that for HelpOverlay as failing "wherever focus is".
- **Fix PostMore and MoreMenu here instead of using capture.** That would correct the stale listeners at their source. But it would grow this ticket to six components, against the run prompt's instruction not to grow scope. The menus are filed as TKT-01M3TV4HBF, and capture makes the dialogs correct whatever the menus do.
- **Make ModerateDialog a user of `useModal`.** See "ModerateDialog stays inline and non-modal" above.
- **One hook with a `modal` flag that covers ModerateDialog too.** It would have one caller for the non-modal branch, and ModerateDialog's change is two lines.

### Out of scope

- MoreMenu, PostMore, PostPopover and OwnControls' `DeleteConfirm`, which set up focus or listeners in a `useEffect`, and ComposerSheet's Escape from the body: TKT-01M3TV4HBF (Move focus and key listeners into layout effects in menus and popovers), filed with no milestone.
- Stacked modals: two `aria-modal` dialogs open at once. No path opens one over another today.

## Notes

**agent:claude-code/phase2-run-ts0zv9** at 2026-10-01T04:22:35Z

Grooming, 2026-10-01, against main at 61201be. A throwaway Vitest probe confirmed the race in all three dialogs: one microtask after the opener's click each is in the DOM with focus still on the opener, and an Escape there is dropped. It also found that on a phone PostMore's document listener, which Preact removes only after the next frame, takes an Escape pressed straight after choosing Flag; a bubble-phase document listener in the dialog does not fix that and a capture-phase one does. So the plan adds useModal in components/focus.ts (layout effect, capture-phase document listener) for HelpOverlay, FlagDialog and ForumPicker, and gives ModerateDialog, which is inline and non-modal by design, only the layout-effect focus move. Filed TKT-01M3TV4HBF (Move focus and key listeners into layout effects in menus and popovers) with no milestone for MoreMenu, PostMore, PostPopover, DeleteConfirm and ComposerSheet's Escape from the body. No open decision.

**agent:claude-code/phase2-run-ts0zv9** at 2026-10-01T04:46:14Z

Implementation evidence, 2026-10-01. The plan groomed in PR #260 held at claim. Main at b3cf1f4 differs from the groomed 61201be only in `.tickets/`, so the plan is not revised. The new tests were written before the fix and run on the unchanged dialogs. They are committed together with the fix, because `focus.test.ts` imports `useModal` and a tests-only commit would not build.

### The race tests fail before the fix

`npx vitest run` on FlagDialog.test.tsx, ForumIndex.test.tsx and ThreadScreen.test.tsx, against main's dialogs: 4 failed and 87 passed.

- FlagDialog "closes the dialog" failed at `FlagDialog.test.tsx:257`: `expected <div class="flag-dialog" …> to be null`.
- FlagDialog "closes the dialog opened from a phone's More list" failed at `:289`, with the same assertion.
- ForumIndex "closes on an Escape pressed straight after opening" failed at `ForumIndex.test.tsx:210`: `expected <div class="forum-picker" …> to be null`.
- ThreadScreen "an Escape pressed straight after opening closes the panel" failed at `ThreadScreen.test.tsx:640`: `expected <section …> to be null`.
- ThreadScreen "an Escape outside the panel leaves it open" passes on both the old and the new code. It pins the non-modal decision.

With the fix, the whole client suite passes: 2040 tests in 68 files. `npx tsc --noEmit -p .` is clean. gofmt, `go build`, `go vet` and `go test -race ./...` pass.

### Mutations, each caught

- `useModal`'s `useLayoutEffect` changed to `useEffect`: 4 cases fail (flag, phone flag, picker, and HelpOverlay's #259 race case).
- The listener moved to the bubble phase: 2 cases fail ("hears Escape before a listener added earlier", and the phone flag case).
- The listener never removed: 10 cases fail.
- ModerateDialog's focus moved back to `useEffect`: its race case fails.

### e2e: the new steps catch the old dialogs

Main's five source files were put back, built with `npx vite build`, and run with the new specs. The command was `-g "flags another|new-thread button|locks and unlocks" --repeat-each 10`, on port 18911, with no load. 39 of 60 failed, every one on a new step's `toHaveCount(0)` (received 1):

- `thread-moderation.spec.ts:60`: 19 of 20
- `forum-index.spec.ts:99`: 18 of 20
- `flag.spec.ts:38`: 2 of 20

### 20 runs under CPU contention, with the fix

From `web/client`: `KETJU_E2E_PORT=18911 KETJU_E2E_DIR=<scratch>/e2e-load/run npx playwright test e2e/flag.spec.ts e2e/forum-index.spec.ts e2e/thread-moderation.spec.ts --repeat-each 20`.

Load: 24 busy `while :; do :; done` loops on a 12-core host, started before the run and stopped after it. The 1-minute load average was 29.7 at the end.

Result: 404 passed, 0 failed, 0 flaky, in 10.6 minutes. That is 20 runs of each of the 10 tests in each of the phone and desktop projects, and the 4 setup sign-ins. No retry, timeout change, skip or focus wait was added.

### Bundle

The reader bundle went from 60440 B to 60465 B gzip (+25 B), of 70000 B.

## Summary

The race is fixed in all three dialogs. FlagDialog, ForumPicker and ModerateDialog moved focus in a useEffect, after the next frame, and caught Escape only on the dialog. So an Escape pressed straight after opening went to the opener and was lost. On a phone, PostMore's stale document listener also took the key and moved focus out of the flag dialog.

- components/focus.ts now has useModal. It moves focus in a layout effect, and takes Escape and Tab on the document in the capture phase while the dialog is mounted. HelpOverlay, FlagDialog and ForumPicker use it.
- ModerateDialog, which is inline and non-modal, moves focus in a layout effect and keeps its panel-level Escape.
- Four Vitest race cases fail on the old code and pass now. useModal has its own tests.
- Three e2e specs gained back-to-back open-then-Escape steps. Against the old dialogs, those steps failed in 39 of 60 runs. With the fix, 20 loaded repeats passed: 404 passed, 0 failed.
- The reader bundle went from 60440 B to 60465 B.
- The menus, popover and delete confirmation that share the pattern are TKT-01M3TV4HBF, a draft with no milestone.
