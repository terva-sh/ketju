---
schema: 3
id: TKT-01M29CGBVNWMMSRTXA4N42XBBS
title: Build the mobile composer sheet and sticky reply bar
type: task
status: done
status_reason: null
priority: urgent
due_on: null
labels:
  - area:client
  - area:mobile
  - effort:M
assignees: []
milestone: phase-1
parent: TKT-01M29BN367F3YQZESM63VN26GP
origin: null
dependencies:
  - TKT-01M29CEE79NN4Q0RXXEYVJHYYE
  - TKT-01M29BW9P4PXMFKF4VAP8K9QPD
blocks_on: none
references:
  - ref: design:mobile-composing
    path: docs/06-thread-views.md
  - ref: pr:80
    path: null
claim: null
archive: null
created_at: 2026-09-11T23:23:08Z
updated_at: 2026-09-25T01:19:37Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase1-run-415af6aa
  name: ""
extensions: {}
---

## Description

On a thread page the tab bar is replaced by a sticky reply bar showing the reply-to chip and the first line of any draft. Tapping it opens the composer as a full-screen sheet with the same toolbar, preview tab, and server-rendered preview. Reply and quote from a post prefill that sheet.

docs/09 names the specific failure to avoid: a composer fighting the on-screen keyboard. The toolbar pins above the keyboard, and the sheet can show the quoted post above the text area, which is the argument for a sheet over an inline composer.

The post list needs bottom padding equal to the bar height, or the reply bar covers the last post, which is the one a reader most wants to see while replying.

## Acceptance criteria

- [x] The post list has bottom padding equal to the reply bar height so the last post is never covered
- [x] Reply and quote from a post open the sheet prefilled
- [x] A draft in progress shows in the reply bar and survives closing the sheet
- [x] The toolbar stays above the on-screen keyboard in Playwright's emulated phone viewport, positioned from the visualViewport API; real iOS and Android verification is in TKT-01M38WTKSQ (Verify the Phase 1 client on real iOS and Android devices)

## Implementation plan

Revised at claim (2026-09-25), after reading the client as it stands.

### Approach
1. **State.** `state/composer.ts`: each Composer gains `open: Signal<boolean>`, which says whether the phone sheet is open. `quotePost`, `replyToPost` and `replyToThread` set it when `isPhone`, so every summons opens the sheet on a phone, including the thread's Reply action in the more menu. On desktop they behave exactly as before.
2. **Shell.** `ShellContext` gains `reply?: string`, the thread whose composer the reply bar opens. ThreadScreen sets it when the reader may reply and the route is not history. `Shell` renders `ReplyBar` in place of `TabBar` when it is set, and adds the `has-replybar` class. A thread the reader cannot reply to keeps the tab bar.
3. **`composer/ReplyBar.tsx`.** Following the mockup's `mobile_thread` bar, it shows the reply-to chip (`#N`) and a pill button carrying the draft's first line (ellipsised) or a placeholder. The pill opens the sheet. The mockup's pen "Post reply" button is left out until posting exists (draft TKT-01M3ART992), because a post button that can only fail is worse than none (the Composer's own rule). The bar is `position: fixed` at the bottom, with height `--replybar-h` plus the safe-area inset (base.css already pads `.replybar`).
4. **Padding.** `.shell.phone.has-replybar .thread-page` gets `padding-bottom: calc(var(--replybar-h) + env(safe-area-inset-bottom, 0px))`, so the last post and the pager clear the bar (AC1).
5. **`composer/ComposerSheet.tsx`.** A full-screen fixed layer, `role=dialog` with `aria-modal`, labelled "Reply":
   - a head with a Close button;
   - the post being answered, shown above the composer when it is on this page;
   - the same `Composer` in `sheet` mode.
   It traps focus, Escape closes it, and closing returns focus to what opened it. The page behind it does not scroll while it is open. Closing only sets `open=false`, so the source and chip stay and the bar shows them (AC3).
6. **Keyboard.** A `visualViewport` resize and scroll listener sizes the sheet to the visual viewport (`top = offsetTop`, `height = vv.height`). In sheet mode the composer is a column with the textarea growing and the formatting toolbar last, so the toolbar sits on the keyboard's top edge (AC4). Without `visualViewport` the sheet is `100dvh`.
7. **Composer `sheet` mode.** On mount it focuses the textarea at the caret, because a sheet opens because someone asked to write. The inline Composer is not rendered on the phone.
8. **Quote and reply on a phone** open the sheet prefilled, using the CEE79 logic unchanged (AC2).

### Tests
- **Vitest:**
  - Shell parts on a thread page: reply bar or tab bar, by can_reply.
  - The bar shows the chip and the first line.
  - Reply, quote and the more menu's Reply open the sheet with the chip or quote, with the textarea focused.
  - Close and Escape keep the draft, and the bar shows it.
  - The focus trap wraps.
  - A mocked `visualViewport` resize resizes the sheet.
  - Desktop is unchanged.
- **Local Playwright (playwright-core, 390×844 emulated):**
  - After scrolling to the bottom, the last post's bottom is at or above the bar's top (AC1).
  - With the sheet open and a synthetic `visualViewport` height reduction, the toolbar's box stays inside the visual viewport (AC4).
  - I record the result in a note. CGBY1 carries the committed Playwright suite.

### Not in scope
- Browser back closing the sheet. It needs a history entry the router shares; left for CGBXK or later.
- Server drafts (CA9CE). The draft lasts the session, as the inline composer's does.

## Notes

**agent:claude/phase1-groom** at 2026-09-24T05:06:25Z

Acceptance criteria rewritten during Phase 1 grooming (2026-09-24), agreed with the user as part of preparing the autonomous run. Rewritten criteria move to the end of the list, so their numbers changed. The real-device keyboard check moved to TKT-01M38WTKSQ. What stays here is what an agent can verify: the emulated phone viewport and positioning driven by visualViewport.

**agent:claude-code/phase1-run-415af6aa** at 2026-09-25T01:19:19Z

Built as planned. Evidence per criterion, and the decisions made:

**Verification**
- **Vitest** (`src/composer/ComposerSheet.test.tsx`, 16 tests, 612 total):
  - the bar replaces the tab bar only where can_reply is set and not on history;
  - the desktop is unchanged;
  - the sheet opens from the bar, from a post's Reply (chip plus the answered post), from a post's Quote (quotation, caret after it) and from the more menu's Reply, with focus in the textarea each time;
  - closing keeps the draft and chip, the bar shows the first non-blank line, and reopening restores them;
  - Escape closes, but not when the mention list already handled it;
  - Tab wraps both ways;
  - a fake `visualViewport` resize moves `--vv-top` and `--vv-height`, and closing stops following.
- **19 hand mutants**, all killed.
- **Local Playwright** (playwright-core, headless Chromium, 390×844, isMobile, touch, light and dark, against vite with the API routed):
  - AC1: the thread page's padding-bottom is 64px, equal to the bar's height. At the bottom of the page the last post ends at 779.5 and the bar starts at 780. The tab bar is absent.
  - AC2 and AC3: tapping Reply on #11 opens the sheet with chip #11. After typing and then Close, the bar shows "One thing to add before we lock this in." and #11.
  - AC4: an init script wraps `visualViewport` so the script can shrink it the way a keyboard does. With 340px taken the sheet is 0–504 and the toolbar 451–504. With the viewport also scrolled 40px (as iOS does) the sheet is 40–584 and the toolbar 531–584. In every case the toolbar's bottom equals the visual viewport's bottom. No console errors.
  - The script is `~/.cache/ketju-gotmp/pw/mobile-sheet.mjs`, outside the repository. CGBY1 should carry an equivalent check into the committed suite. Real devices are TKT-01M38WTKSQ.

**Decisions**
- **No post button.** The mockup's pen "Post reply" button is left out, because posting from the client is draft TKT-01M3ART992 and a button that can only fail is worse than none. The pill opens the sheet instead.
- **Fixed bar plus padding, not sticky.** The criterion asks for padding equal to the bar, and a fixed bar with that padding holds when the page is shorter than the screen as well.
- **Sheet placement via custom properties.** The sheet is placed through `--vv-top` and `--vv-height`, set through the CSSOM, because the CSP forbids style attributes (breakpoints.test.ts enforces this).
- **Focus return.** The opener is read at render, because the composer's layout effect moves focus first. Safari does not focus a clicked button, so when the opener is the body, focus returns to the bar.
- **Not done.** Browser back does not close the sheet, because it would need a history entry shared with the router. That is noted for CGBXK.

## Summary

On a phone, a thread the reader may reply to has a reply bar (ReplyBar) in place of the tab bar. It shows the reply chip and the draft's first line, and the thread page is padded by its height. Tapping it, or a post's Reply or Quote, or the more menu's Reply, opens ComposerSheet: a full-screen modal dialog holding the same Composer, with the post being answered above it. The sheet traps focus, closes on Escape, and returns focus when it closes. It is sized to the visual viewport with the toolbar last, so the toolbar sits on the keyboard. Closing keeps the draft. Verified with Vitest, 19 mutants, and a local Playwright run at 390x844.
