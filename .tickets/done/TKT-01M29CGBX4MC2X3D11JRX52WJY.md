---
schema: 3
id: TKT-01M29CGBX4MC2X3D11JRX52WJY
title: Add the desktop keyboard shortcuts
type: task
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area:client
  - effort:S
assignees: []
milestone: phase-1
parent: TKT-01M29BN367F3YQZESM63VN26GP
origin: null
dependencies:
  - TKT-01M29CEE6V358M5KCBJGHD58HM
blocks_on: none
references:
  - ref: design:canonical-view
    path: docs/06-thread-views.md
claim: null
archive: null
created_at: 2026-09-11T23:23:08Z
updated_at: 2026-09-28T05:53:43Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase1-run-438e26c1
  name: ""
extensions: {}
---

## Description

`j` and `k` to move between posts, `g` then a number to jump to an ordinal, `u` to jump to first unread, and `n` for the next unread thread in the forum.

Shortcuts are absent on the phone layout by design rather than by omission. Nothing here may be the only way to reach a feature, since a keyboard-only path would be a desktop-only feature and docs/06 forbids those.

## Acceptance criteria

- [x] Every documented shortcut works and none fires while focus is in the composer
- [x] Each shortcut has an equivalent visible control
- [x] A discoverable help overlay lists them
- [x] Shortcuts are inert on the phone layout

## Implementation plan

Revised at claim (2026-09-28), from the code on main at 05a6245.

### Approach
1. `web/client/src/keyboard/shortcuts.ts` holds the rules as plain functions (which key does what, the guard, the post walk, the next-unread-thread search) and `useThreadShortcuts`, which ThreadBody mounts. It installs one `keydown` listener on `document`. The listener does nothing when `isPhone.value`, when the event is already handled, when Ctrl, Meta or Alt is held, or when the target is an input, textarea, select or `[contenteditable]`, or sits inside `.composer` or any `[role=dialog]`.
2. `j` and `k` walk the canonical posts, `.posts > [id^=post-]` (articles and tombstones). The expanded replies' copies carry `data-reply` and no id, so the walk skips them. The current post is the focused one, else the post at the reading line (the posts' `scroll-margin-top`). The target is focused (posts take `tabindex="-1"`) and scrolled to. Past the last post `j` opens the next page and focuses its first post; before the first post `k` opens the previous page and focuses its last. A module-level pending target carries the focus across the page load, because ThreadBody remounts per page.
3. `g`, then digits, then Enter (or a 1.5 s pause) goes to that ordinal. A post on this page is scrolled to and focused; any other goes to its permalink. A `role=status` prompt shows "Go to #…" while typing. Escape cancels. Any other key cancels too. An ordinal outside 1..post_count gets a message instead of a request.
4. `u` goes to the first unread post: `#post-{last_read+1}` when it is on the page, else its permalink. With no read position it goes to the thread's own address, where the server lands the reader. With nothing unread it says so.
5. `n` opens the next unread thread in the forum. It reads `GET /api/v1/forums/{slug}/threads`, following `next_cursor` for at most 5 pages. It picks the first row after the current thread with `unread > 0`. If none is after it, it wraps to the first unread row before it. It opens that row where its unread badge goes (`unreadHref`). With none it says so.
6. `?` opens `keyboard/HelpOverlay.tsx`, a modal dialog listing every shortcut and its visible control. Escape or Close ends it, and focus goes back to what opened it.
7. Visible equivalents (AC2), all in the thread page:
   - `j`/`k`: each post's `#N` link and the pager.
   - `g`: a "Go to post" form in a new thread foot below the bottom pager.
   - `u`: a "First unread" thread action, in the desktop toolbar and the phone's more menu (threadActions). It is offered to a signed-in reader with unread posts.
   - `n`: a "Next unread thread" button in the thread foot, for a signed-in reader.
   - `?`: a "Keyboard shortcuts" button in the thread foot, on desktop only, since on the phone there are none to list.
   The thread foot is on both layouts, so nothing is desktop-only.

### Files
- `web/client/src/keyboard/{shortcuts.ts,HelpOverlay.tsx,ThreadFoot.tsx,keyboard.css}`
- `src/screens/ThreadScreen.tsx` (mount the hook and the foot; pass unread state to threadActions)
- `src/shell/threadActions.ts` (First unread)
- `src/components/thread/{Post,Tombstone}.tsx` (`tabindex="-1"`)
- `thread.css` (the focused post's ring)
- `docs/06-thread-views.md` (records the order `n` uses and its wrap)
- the regenerated `internal/web/spa/dist`

### Tests
- AC1: Vitest dispatches each key on the document against a stubbed thread and asserts the effect. The same keys from the composer textarea, and from inside a dialog, change nothing. The rules (guard, walk, next-unread search with wrap) get table tests. Playwright at 1440 px runs `j`, `k`, `g 4 0 Enter`, `u` and `n` against the e2e seed, as carol.
- AC2: a Vitest table maps each shortcut to its visible control and asserts it renders and does the same thing.
- AC3: Playwright presses `?` and asserts the dialog lists every shortcut, and that the visible button opens it too.
- AC4: Playwright at 390 px presses `j`, `u`, `g 4 Enter` and `?`, and asserts no scroll, no focus change and no overlay. Vitest does the same with `isPhone` set.
- The existing a11y and 44 px target specs cover the thread foot at both widths.

### Design
- docs/06 "Paged, not infinite" (the keyboard list) and "Mobile" (shortcuts absent).
- docs/07 "Accessibility".
- `/` is not in the ticket's set and is not added.

### Settled in grooming (2026-09-24)

- 'Next unread thread' follows the forum's thread-list order.

## Notes

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T05:53:28Z

Gaps settled during the build (docs/12 "Gaps the design leaves open"):

- **Wrap for `n`.** Grooming settled that "next" follows the forum's thread-list order. Under latest-activity order, threads with new posts rise above the one being read. So when no unread thread is below the current one, `n` wraps to the first unread thread above it. This is recorded in docs/06 "Paged, not infinite" in the same PR.
- **How far `n` reads.** It reads at most 5 pages of the list, following `next_cursor`. The bound has no product meaning. It stops early once it holds an unread thread below the current one.
- **Where `n` lands.** It opens the thread where the list's unread badge goes (`unreadHref`): the permalink after the read position, or with no position, the thread's own address.
- **`u` with no read position** goes to the thread's own address, where the server lands the reader. It does not assume post 1, for the same reason `unreadHref` does not.
- **`/` was not added.** It is not in the ticket's set.
- **Where the controls live.**
  - The visible controls for `g`, `n` and `?` are in a new thread foot below the bottom pager (`keyboard/ThreadFoot.tsx`).
  - "Keyboard shortcuts" is not drawn on the phone, because there are no shortcuts there to list.
  - "First unread" (`u`) is a thread action, so it is in the desktop toolbar and the phone's more menu. It is offered to a signed-in reader with unread posts.
- **Posts take `tabindex="-1"`,** so `j`/`k`/`g`/`u` can focus them. A focused post gets a ring under `:focus-visible` only.

Tests: 41 Vitest cases in `src/keyboard/shortcuts.test.tsx`, and `e2e/shortcuts.spec.ts` at both widths. The e2e run used port 8794: 45 passed, 9 skipped by project. The spec reads alice's position in "Minutes from the September meeting" (#30) without marking it read. When read tracking lands, that spec and thread-list.spec.ts both need that position to hold.

## Summary

The thread page takes j, k, g then a number then Enter, u, n and ? on the desktop layout (web/client/src/keyboard/). None fires in a field, the composer or a dialog, with Ctrl, Meta or Alt held, or on the phone layout. Each shortcut has a visible control on both layouts: Go to post, Next unread thread and Keyboard shortcuts (desktop only) in a new thread foot, First unread in the thread's actions, and the posts and pager for j and k. n follows the forum's list order and wraps to the first unread thread above; docs/06 records this. Vitest covers the rules and each key. e2e/shortcuts.spec.ts covers 1440 px and 390 px.
