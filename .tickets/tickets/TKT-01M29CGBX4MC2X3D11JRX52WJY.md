---
schema: 3
id: TKT-01M29CGBX4MC2X3D11JRX52WJY
title: Add the desktop keyboard shortcuts
type: task
status: in-progress
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
claim:
  actor: agent:claude-code/phase1-run-438e26c1
  branch: run/cgbx4-keyboard-shortcuts
  worktree: /home/sothr/workspace/git.local.sothr.com/terva-sh/ketju/.claude/worktrees/agent-ad2d70c0d1d9be0ad
  commit: 05a624537bdb1cc5863c39485ef9dbf45eda748f
  session: null
  claimed_at: 2026-09-28T05:43:30Z
  expires_at: null
archive: null
created_at: 2026-09-11T23:23:08Z
updated_at: 2026-09-28T05:45:24Z
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

- [ ] Every documented shortcut works and none fires while focus is in the composer
- [ ] Each shortcut has an equivalent visible control
- [ ] A discoverable help overlay lists them
- [ ] Shortcuts are inert on the phone layout

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
