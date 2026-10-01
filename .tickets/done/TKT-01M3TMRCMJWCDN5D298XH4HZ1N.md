---
schema: 3
id: TKT-01M3TMRCMJWCDN5D298XH4HZ1N
title: Close the shortcuts overlay on an Escape pressed before it takes focus
type: bug
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area:client
  - area:test
  - effort:S
assignees: []
milestone: phase-2
parent: null
origin: null
dependencies: []
blocks_on: none
references:
  - ref: ticket:TKT-01M3SZ3GCE
    path: null
  - ref: pr:257
    path: null
  - ref: pr:259
    path: null
claim: null
archive: null
created_at: 2026-10-01T02:30:06Z
updated_at: 2026-10-01T04:02:04Z
created_by:
  id: agent:claude-code/phase2-run-sz3gce
  name: ""
updated_by:
  id: agent:claude-code/phase2-run-tmrcmj
  name: ""
extensions: {}
---

## Description

Found by the TKT-01M3SZ3GCE lane on 2026-10-01, while reproducing that ticket's flake under load. `web/client/e2e/shortcuts.spec.ts:73`, on the desktop project, failed in 2 of 21 full-suite runs on main (tree f3ba70d). The runs were in the CI image (golang:1.27-alpine with Alpine's Chromium 152.0.7977.82), in three containers limited to 2 CPUs each, running at the same time under 24 busy host loops. Each run took 8 to 9 minutes, as CI's runs do when other jobs share the runner. The failing runs are labelled par-c2 run 2 and par-c2 run 4.

```
Error: expect(locator).toHaveCount(expected) failed
Locator:  getByRole('dialog', { name: 'Keyboard shortcuts' })
Expected: 0
Received: 1
Timeout:  5000ms
```

The step is `page.keyboard.press('Escape')`, followed by `expect(dialog).toHaveCount(0)`.

### The likely cause (not yet proven)

`HelpOverlay` (web/client/src/keyboard/HelpOverlay.tsx) closes on Escape through `onKeyDown` on the dialog element. So it catches Escape only when focus is inside the dialog. A `useEffect` moves focus to Close, and Preact runs that effect after the next frame. The spec checks that the dialog is visible and that its list is right, then presses Escape at once. If the effect has not run yet under load, focus is still on the post, the key never reaches the dialog, and the dialog stays open. A person who presses `?` and then Escape quickly would see the same thing.

### What to do

1. Confirm the cause from a trace: focus at the moment Escape is pressed.
2. Fix it in the app, not the spec. For example, move focus in `useLayoutEffect`, or handle Escape for the open overlay at the document level.
3. Add a vitest test that presses Escape before the effect would have run.
4. Show 20 runs under load without the failure. Do not add a retry, a longer timeout or a skip.

## Acceptance criteria

- [x] A Vitest case in web/client/src/keyboard/shortcuts.test.tsx dispatches ? then Escape on a focused post with no frame between them (outside act, one microtask apart) and asserts the overlay closed and focus returned to the post; a ticket note records it failing on unchanged code and passing after the fix
- [x] HelpOverlay moves focus to Close in a useLayoutEffect, and takes Escape and Tab through a document keydown listener added and removed by that effect; the dialog's own onKeyDown Escape handler is gone
- [x] A Vitest case shows that with the overlay open and focus on document.body, Escape closes it and Tab moves focus to Close; that j moves no post while it is open; and that after it closes j works again and no overlay listener remains
- [x] e2e/shortcuts.spec.ts keeps every existing assertion, adds ? then Escape pressed back to back with expect(dialog).toHaveCount(0), and adds exactly one axe scan of the open overlay, run as alice from her stored session; no retry, longer timeout or skip is added
- [x] e2e/shortcuts.spec.ts passed 20 of 20 desktop runs on port 18911 under CPU contention, with the command, load and results in a ticket note
- [x] internal/web/spa/dist is regenerated with go generate ./internal/web/spa, and the PR states the reader bundle's gzip size before and after (under 70000 B)

## Implementation plan

Groomed 2026-10-01 against main at c3de983.

### Settled in grooming

- Fix the product, not the spec. The ticket's description (TKT-01M3SZ3GCE lane, 2026-10-01) rules out a retry, a longer timeout or a skip in `e2e/shortcuts.spec.ts`, and so does the run's prompt for this ticket. The spec's existing assertions stay as they are.
- The behaviour to build: an Escape pressed while the shortcuts overlay is open closes it, wherever focus is. This is the run prompt's wording for this ticket (2026-10-01). The overlay's own comment (HelpOverlay.tsx: "Escape, Close or a click outside ends it") and TKT-01M29CGBX4's plan step 6 say the same. docs/06 "Paged, not infinite" lists the keys and says nothing about how the overlay closes, so no design document changes.
- The race is confirmed and named, and is not just suspected. A throwaway Vitest probe was run during grooming against main at c3de983 and not committed. It mounted the thread page, focused a post, and dispatched `?` on it outside `act`. After one microtask the dialog was in the DOM, `helpOpen` was true, and `document.activeElement` was still the post. An Escape dispatched on the post then left the dialog open. 60 ms later the focus effect had run, and focus was on Close with the dialog still open. That is the failure #1592's sibling runs showed in e2e. The mechanism:
  - `?` sets `helpOpen` in `useThreadShortcuts`'s document listener (keyboard/shortcuts.ts). Preact renders `<HelpOverlay />` on the following microtask, within the same task.
  - `HelpOverlay` moves focus to Close in a `useEffect`. Preact runs that after the next frame: `afterNextFrame`, which waits for `requestAnimationFrame` or a 35 ms timeout, whichever comes first. A loaded runner delays both.
  - Escape is caught only by the dialog's `onKeyDown`, so it must be dispatched inside the dialog. Before the effect runs, its target is the post. The thread listener returns early because `helpOpen` is true, so nothing handles the key.
  - The same race lets a Tab pressed in that window move focus out of the page's posts rather than into the dialog.

### Approach

1. **Commit the failing test first.** Add a Vitest case to `web/client/src/keyboard/shortcuts.test.tsx`, beside "? opens the list of shortcuts, and Escape closes it and gives focus back". It reproduces the window the grooming probe found:
   - Mount the thread with `renderAt` and focus `#post-2`.
   - Dispatch `?` on the post with a plain `dispatchEvent`, outside `act`, so Preact's effects are not flushed.
   - Await one microtask, so the overlay has rendered and no frame has passed.
   - Assert the dialog is present. Then dispatch Escape on `document.activeElement`.
   - Assert the dialog is gone and focus is back on `#post-2`.
   Run it on unchanged code and record in a ticket note that it fails, with the assertion it fails on.
2. **Move focus in the commit that draws the overlay.** In `web/client/src/keyboard/HelpOverlay.tsx`, change the focus effect from `useEffect` to `useLayoutEffect`. Layout effects run synchronously when Preact commits the render that inserts the dialog. That render is in the same task as the `?` keydown, so no later key can arrive before focus is on Close. The opener is still `document.activeElement` at that point, because nothing has moved focus yet. The cleanup, which returns focus to the opener, then also runs synchronously on unmount. `ComposerSheet.tsx` already uses `useLayoutEffect` for its open and close for the same reason, so this follows an existing pattern.
3. **Take the overlay's keys on the document while it is mounted.** The same layout effect adds a `keydown` listener on `document` and removes it in its cleanup. It replaces the dialog's `onKeyDown`, which is removed:
   - Escape, not `defaultPrevented`: `preventDefault()` and close. This holds wherever focus is: on the post that opened it, or on the body after a click on the dialog's text.
   - Tab: `trapTab(e, root.current)`. `trapTab` already handles focus outside the root (`!root.contains(at)`) by moving to the first or last control, so a Tab from outside goes into the dialog, as `aria-modal` promises.
   The thread's own listener in `useThreadShortcuts` is registered earlier and returns at once while `helpOpen` is true, so the two never both act on one key. The order is pinned by a test (Tests).
4. **Strengthen the e2e step without weakening it.** In `web/client/e2e/shortcuts.spec.ts`, the test "j and k move between posts, g goes to one, ? lists them" keeps every current assertion. After the button-and-Close sequence, it adds:
   - `?` then Escape pressed back to back, with no wait between them, then `expect(dialog).toHaveCount(0)`. This is the reader's quick `?` then Escape, and on unchanged code it fails whenever the frame has not come.
   - One axe scan of the open overlay, using the existing `wcag2a/aa, wcag21a/aa` tags. This is the test's only scan; no spec scans the overlay today. The test already runs as alice from her stored session (`asAlice` and `stateFile`), and stays that way.
5. **Rebuild and measure.** Run `go generate ./internal/web/spa` and commit `internal/web/spa/dist`. Report the reader bundle's gzip size before and after (60438 B of 70000 B at grooming). Expect a change of a few bytes.
6. **Prove it under load.** Run `e2e/shortcuts.spec.ts` 20 times on the desktop project with `KETJU_E2E_PORT=18911`, under CPU contention: busy loops on every core, with the TKT-01M3SZ3GCE note's setup as the model. Record the command, the load and the pass count in a ticket note. No retry, no longer timeout, no skip.

### Tests

- New Vitest case: "Escape pressed before the overlay has taken focus still closes it" (Approach 1). It must fail on main and pass after the fix, and the ticket note records both runs.
- New Vitest case: "Escape closes the overlay from outside it". Open the overlay through `act`, then move focus to `document.body`, as a click on the dialog's text leaves it. Escape on the body closes the overlay, and Tab on the body moves focus to Close. This case pins "wherever focus is", which Approach 2 alone does not give.
- In the same case: with the overlay open and focus on the body, `j` moves no post's focus, because the thread's listener stands aside while `helpOpen` is true. After Escape, `j` works again, and the overlay's listener is gone, so a second Escape changes nothing. This guards the listener order and the cleanup in Approach 3.
- The existing case "? opens the list of shortcuts, and Escape closes it and gives focus back", and "Keyboard shortcuts opens the list", pass unchanged.
- e2e: the added steps in `shortcuts.spec.ts` (Approach 4), with 20 loaded runs (Approach 6).
- `npx tsc --noEmit -p .` in `web/client`, because Vitest does not typecheck test files.

### Alternatives that lost

- **Change the spec to wait for focus on Close before pressing Escape.** This hides a real bug a quick reader hits. The ticket and the run prompt both forbid it.
- **`useLayoutEffect` alone (Approach 2 without 3).** It closes this race. But Escape then still depends on focus being inside the dialog, and a click on the dialog's text puts focus on the body, where Escape does nothing. That fails "wherever focus is".
- **Handle Escape for the overlay in `useThreadShortcuts`'s listener.** That listener is installed before the overlay exists, so it has no race. But it would put the overlay's dismissal in the thread hook, and the overlay also opens from its button, so ownership would sit in the wrong module. The overlay owns its keys.
- **A shared `useModalKeys` hook in `components/focus.ts` now.** FlagDialog, ModerateDialog and ForumPicker share the race (below). Extracting the hook is that follow-up ticket's to do, once it has three callers to fit. This ticket changes one dialog.

### Out of scope

- The same `useEffect` focus race in `components/thread/FlagDialog.tsx`, `components/thread/ModerateDialog.tsx` and `screens/ForumPicker.tsx`. Each moves focus in a `useEffect` and catches Escape only on the dialog. Filed as TKT-01M3TS0ZV9 (Take Escape in the flag, moderate and forum-picker dialogs before focus).
- TKT-01M3SZ3GCE (Stop admin-applications.spec.ts timing out under CI load) and TKT-01M3TNBKNQ (Print Playwright's error context into the CI log when a test fails). They are separate flakes and tooling.

## Notes

**agent:claude-code/phase2-run-tmrcmj** at 2026-10-01T03:45:00Z

Grooming, 2026-10-01, against main at c3de983. The race is confirmed in Vitest, not inferred: after ? the overlay renders on the next microtask with focus still on the post, an Escape dispatched there is dropped, and the focus effect later puts focus on Close with the dialog open (details in the plan's Settled in grooming). The plan fixes HelpOverlay with a layout effect and a document listener. Filed TKT-01M3TS0ZV9 (Take Escape in the flag, moderate and forum-picker dialogs before focus) for the same race in three other dialogs. No open decision.

**agent:claude-code/phase2-run-tmrcmj** at 2026-10-01T04:01:33Z

Implementation evidence, 2026-10-01. The plan groomed in PR #257 held at claim (main at af9cd54, which differs from the groomed c3de983 only in `.tickets/`), so it is not revised.

### Approach 1: the race test fails before the fix

The new tests were run against the old `HelpOverlay.tsx` (main's), with `npx vitest run src/keyboard/shortcuts.test.tsx`:

- "Escape pressed before the list has taken focus still closes it" failed at `shortcuts.test.tsx:433`: `expected <div class="help-overlay" …> to be null`. The dialog was still open after Escape.
- "Escape and Tab reach the list wherever focus is, and the page's keys wait for it" failed at `shortcuts.test.tsx:452`: `expected false to be true`. Tab from the body did not move focus to Close.

With the fix, all 50 tests in the file pass. The whole client suite passes, 2032 tests in 68 files, and `npx tsc --noEmit -p .` is clean.

### Mutations, each caught

- `useLayoutEffect` changed back to `useEffect`: the microtask case fails.
- The listener on the dialog root, not the document: the focus-anywhere case fails.
- The listener never removed: the second Escape is taken, and the focus-anywhere case fails.

### e2e: the new step catches the old code

The new `?` then Escape step in `e2e/shortcuts.spec.ts` was run on the old overlay, rebuilt into the dist: `--project desktop --repeat-each 10 -g "lists them"`, port 18911, no load. It failed 9 of 10 runs at `shortcuts.spec.ts:98` (`toHaveCount(0)`, received 1). It reproduces the bug in Chromium without any load.

### Approach 6: 20 runs under CPU contention, with the fix

Command, from `web/client`: `KETJU_E2E_PORT=18911 KETJU_E2E_DIR=<scratch>/e2e-load/run npx playwright test e2e/shortcuts.spec.ts --repeat-each 20`. Each repeat also runs the setup and phone projects.

Load: 24 busy `while :; do :; done` loops on a 12-core host, started before the run and stopped after it. The 1-minute load average was 33.4 at the end. The main test took 2.8 to 4.4 s per run, against 1.2 s unloaded.

Result: 84 passed, 0 failed, 0 flaky. That is all 20 runs of each of the three desktop tests, including "j and k move between posts, g goes to one, ? lists them" (line 34, formerly the failure at :73), 20 phone runs, and 4 setup runs. No retry, timeout change or skip was added.

One full-suite Playwright run on port 18911 passed 193, with 11 skipped.

## Summary

The race is confirmed and fixed. ? drew HelpOverlay on the next microtask, but focus moved to Close only after the next frame (useEffect), and Escape was caught only on the dialog. So an Escape in that window landed on the post and was dropped. HelpOverlay now moves focus in a useLayoutEffect, and takes Escape and Tab on the document while it is mounted, so Escape closes it wherever focus is. Two Vitest cases fail on the old code and pass now. The e2e spec gains a back-to-back ? and Escape step, which failed 9 of 10 on the old code, and one axe scan of the open list. 20 loaded repeats of e2e/shortcuts.spec.ts on port 18911 passed: 84 passed, 0 failed. The reader bundle is 60438 B to 60440 B. The same race in FlagDialog, ModerateDialog and ForumPicker is TKT-01M3TS0ZV9.
