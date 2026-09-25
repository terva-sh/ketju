---
schema: 3
id: TKT-01M29CGBXKH0HBN0HEJGDPBFYV
title: Run the accessibility pass on the client
type: task
status: in-progress
status_reason: null
priority: high
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
  - TKT-01M29CEE4KNQBFP49FVSCRNZA4
  - TKT-01M29CEE6V358M5KCBJGHD58HM
blocks_on: none
references:
  - ref: design:mobile-targets
    path: docs/06-thread-views.md
claim:
  actor: agent:claude-code/phase1-run-415af6aa
  branch: run/cgbxk-a11y
  worktree: /home/sothr/.t3/worktrees/ketju/t3code-415af6aa
  commit: 601c687cc9fdfc03133b7f8eefe53d770ea884d3
  session: null
  claimed_at: 2026-09-25T04:28:58Z
  expires_at: null
archive: null
created_at: 2026-09-11T23:23:08Z
updated_at: 2026-09-25T04:30:46Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase1-run-415af6aa
  name: ""
extensions: {}
---

## Description

Semantic landmarks, focus management on page change, `prefers-reduced-motion`, and 44 px minimum touch targets on the phone layout.

The 44 px rule has a carve-out that needs implementing correctly: inline links inside text, such as author names, ordinals, and tag chips, keep their visual size and get an enlarged hit area through padding. Everything standalone gets the full target.

No action is swipe-only. A swipe may be a shortcut, never the only route to a feature.

## Acceptance criteria

- [ ] Every standalone control on the phone layout is at least 44 px on its shorter side, checked automatically
- [ ] Focus moves to the main heading on route change and is never lost to a removed element
- [ ] Animations respect prefers-reduced-motion
- [ ] No feature requires a swipe
- [ ] An automated accessibility scan passes on the forum index, thread list, and thread page

## Implementation plan

Revised at claim (2026-09-25), after reading the client as it stands. The forum index, thread list and thread page are merged (TKT-01M29CEE5Y, TKT-01M29CEE6D, TKT-01M29CEE4K), so every screen AC5 names exists.

### What is already there
- Landmarks: the shell has a `<header>` bar and a `<nav aria-label="Sections">` (top nav or tab bar). Each screen renders one `<main>` with one `<h1>`, and posts are `<article>`s.
- Focus return: the more menu, the composer sheet (with `trapTab`) and the forum picker already return focus.
- No transitions, animations, smooth scrolling or swipe handlers exist.

### Approach
1. **Skip link.** `Shell` renders "Skip to content" first, visually hidden until focused. It is `href="#main"`, which preact-iso ignores for `#` links, and its click moves focus to the main heading, so the address does not gain a hash.
2. **Focus on route change (AC2).** `components/focus.ts` (beside `trapTab`) gains `focusMainHeading()`: the first `main h1`, given `tabindex=-1` and focused with `preventScroll`, so a thread's arrival scroll and the list's restored position are not undone; `main` if there is no h1.
   - `app.tsx` passes `onRouteChange` to the `Router`. It fires after the new route has rendered, only when the path changes. The first render (a fresh load) is skipped.
   - A navigation within one thread (`/t/{id}/…` to `/t/{id}/…`, such as a permalink after posting or a page change) is skipped too, because the thread page places the reader itself, and focus on a composer should not be pulled away by the reply it just posted.
3. **Focus never lost (AC2).** Removing a focused element does not reliably fire blur in Chromium, so an app-level guard tracks the last focused element and its ancestors on `focusin`.
   - A `MutationObserver` on the body notices when focus has fallen to `body` and that element is disconnected.
   - After a frame, so a component's own effect can return focus first, it focuses the nearest still-connected focusable ancestor, or else the main heading.
   - This covers the paging link that disappears on the list's last page and a tombstone replacing a post.
   - `PostPopover` also returns focus to the link that opened it on close. Today it drops focus with the box.
4. **Motion (AC3).** `styles/base.css` gains a `prefers-reduced-motion: reduce` block: animations and transitions at 0.01 ms, and `scroll-behavior: auto`. Nothing animates today, so the rule governs what is added later. The one JS scroll API in use, `scrollIntoView({block})`, is already instant.
5. **Targets (AC1).** Fix whatever `e2e/targets.spec.ts` finds at 390 px. Standalone controls get `min-height`/`min-width: 44px` under the phone query. Inline links in text (author names, ordinals, the reply-to indicator) get a `.hit` class that pads the hit area with a matching negative margin, so the visual size is unchanged (docs/06 "Targets and gestures").
6. **Swipes (AC4).** None exist. A Vitest scans `src/**/*.ts(x)` for `touchstart`, `touchmove`, `touchend`, `pointermove` and swipe gesture handlers, and fails on any not in an allowlist that names its button equivalent. A comment by the sheet says so.
7. **Scan (AC5).** `@axe-core/playwright` is added as a dev dependency. `e2e/a11y.spec.ts` scans the forum index, the thread list and the thread page:
   - signed in, with the composer present;
   - in light and dark, at both project widths;
   - with the tags `wcag2a`, `wcag2aa`, `wcag21a` and `wcag21aa` (WCAG 2.1 AA, as the pre-groom plan assumed; docs leaves it open);
   - requiring zero violations.
   Fix what it finds, or disable a rule only with a written reason in the spec.

### Tests
- AC1: `e2e/targets.spec.ts` at 390 px, phone project only, on the forum index, the thread list, and the thread page with the sheet open.
  - It enumerates visible `a, button, [role=button], input, select, textarea, summary, [tabindex="0"]`, skipping anything inside `.post-body`.
  - It measures `.hit` elements by their padded box, and asserts `min(width, height) >= 44`, naming each offender.
- AC2:
  - Playwright clicks index, then list, then thread, and asserts `activeElement` is that screen's h1 each time.
  - It pages the chatter list to its last page by keyboard, where "Older threads" goes, and asserts focus is not on `body`.
  - It opens and closes a post popover, and asserts focus is back on its link.
  - Vitest covers `focusMainHeading`, the same-thread skip, the first-render skip, and the guard.
- AC3: Playwright with `reducedMotion: 'reduce'` asserts the computed `transition-duration` and `animation-duration` on the sheet and on a sample of controls are at most 0.01 s. Vitest checks that base.css carries the block.
- AC4: the Vitest source scan.
- AC5: `e2e/a11y.spec.ts`.
- Hand mutants on focus.ts.

### Files
`src/components/focus.ts`, `src/app.tsx`, `src/shell/Shell.tsx`, `src/shell/shell.css`, `src/styles/base.css`, `src/components/thread/PostPopover.tsx`, and the CSS the scans point at. `e2e/a11y.spec.ts`, `e2e/targets.spec.ts`, `e2e/focus.spec.ts`; `package.json` and the lock file.

## Notes

**agent:claude-code/phase1-run-415af6aa** at 2026-09-25T02:19:54Z

2026-09-25: AC5 scans the forum index and thread list, which do not exist yet. They need TKT-01M29CA9A5, TKT-01M29CA9AG, TKT-01M29CEE5Y and TKT-01M29CEE6D, none of which is a recorded dependency. The maintainer chose to build those four before this ticket.
