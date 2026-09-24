---
schema: 3
id: TKT-01M29CGBX4MC2X3D11JRX52WJY
title: Add the desktop keyboard shortcuts
type: task
status: draft
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
updated_at: 2026-09-24T05:21:04Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude/phase1-groom
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

Pre-groom plan (2026-09-24). Revise at claim, after reading the code as it stands then.

### Approach
1. `keyboard/shortcuts.ts` installs one `keydown` listener on `document` while ThreadPage (and ThreadList, for `n`) is mounted. It returns early when `isPhone.value`, when any modifier key is held, or when `event.target` is an input, textarea, select or `[contenteditable]`, or sits inside `.composer` or an open dialog.
2. `j` and `k` move a `focusedOrdinal` among the `article[id^=post-]` on the page, scroll it into view, and focus it (`tabindex="-1"`). At the page edge they follow the pager to the next or previous page.
3. `g` then digits then Enter (or a 1.5 s pause) jumps to `/t/{id}/{slug}/{n}` through the permalink route. A small "Go to #…" prompt shows the digits being typed.
4. `u` jumps to the first unread: `#post-{last_read_ordinal+1}` if it is on the page, otherwise the permalink.
5. `n` goes to the next unread thread in the forum: fetch (or reuse cached) `GET /forums/{slug}/threads`, pick the first row after the current thread with `unread > 0`, and open it at its first unread. "Next" is not defined in docs/06; list order is assumed.
6. `?` opens `HelpOverlay.tsx` (a dialog listing the shortcuts). A visible "Keyboard shortcuts" link in the thread toolbar or footer makes it discoverable.
7. Visible equivalents for AC2: j and k correspond to the posts themselves and the pager; g to the permalink "link" action plus a "Go to post #" input in the pager; u to a "Jump to first unread" button in the thread header; n to a "Next unread thread" link at the thread bottom.

### Files
`web/client/src/keyboard/{shortcuts.ts,HelpOverlay.tsx}`, `src/screens/ThreadPage.tsx` (controls), `src/components/Pager.tsx` (go-to input).

### Tests
- AC1: Vitest dispatches each key on the document and asserts the effect; dispatching the same keys with the target inside the composer textarea asserts no effect. Playwright runs j, k, g 4 0 Enter, and u once at 1440 px.
- AC2: a Vitest table maps each shortcut to a selector for its visible control and asserts each control renders.
- AC3: Playwright presses `?` and asserts the dialog lists all six entries, and that the visible link opens it too.
- AC4: Playwright at 390 px presses j, u and ? and asserts no scroll, focus change or overlay.

### Design
docs/06 "Paged, not infinite" (keyboard list) and "Mobile" (Targets and gestures: shortcuts simply absent); docs/07 "Accessibility" (keyboard-first on desktop); the mockup's search box shows a `/` key hint, which suggests `/` focuses search. The ticket does not list `/`, so ask before adding it.

### Interactions
Depends on CEE6V and on CEE6D data for `n`. It uses `isPhone` from CEE4K. CGBXK checks that focus stays visible. The composer (CEE79) must carry the `.composer` class the guard relies on.
Open in docs: the ordering "next unread thread" uses, and whether `/` belongs to the set.

### Settled in grooming (2026-09-24)

- 'Next unread thread' follows the forum's thread-list order.
