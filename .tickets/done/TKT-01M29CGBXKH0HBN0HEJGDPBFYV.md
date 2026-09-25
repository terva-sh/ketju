---
schema: 3
id: TKT-01M29CGBXKH0HBN0HEJGDPBFYV
title: Run the accessibility pass on the client
type: task
status: done
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
  - ref: pr:89
    path: null
claim: null
archive: null
created_at: 2026-09-11T23:23:08Z
updated_at: 2026-09-25T05:06:02Z
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

- [x] Every standalone control on the phone layout is at least 44 px on its shorter side, checked automatically
- [x] Focus moves to the main heading on route change and is never lost to a removed element
- [x] Animations respect prefers-reduced-motion
- [x] No feature requires a swipe
- [x] An automated accessibility scan passes on the forum index, thread list, and thread page

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

**agent:claude-code/phase1-run-415af6aa** at 2026-09-25T05:05:22Z

Built. Where each criterion landed, and what moved off the plan.

### AC5, the scan
`e2e/a11y.spec.ts` runs `@axe-core/playwright` 4.13.0, a new exact-pinned dev dependency, with the tags `wcag2a`, `wcag2aa`, `wcag21a` and `wcag21aa`.
- **Coverage:** the forum index, the general thread list and the seed thread; signed in; light and dark; both project widths. It also scans the index with the forum picker open, and the phone thread page with the composer sheet open.
- **Result:** zero violations, and no rule is disabled.
- **What it found, and the fixes:**
  - Contrast: the pinned row's meta text, `--muted` on `--surface2`, is under 4.5:1 in light. It now uses `--ink2`.
  - Contrast: the phone pager's inactive "‹ Prev" / "Next ›" placeholder. That is an inactive component, which WCAG 1.4.3 exempts; it is now marked `aria-disabled="true"` beside its `aria-hidden`, which is what axe reads.

### AC1, targets
`e2e/targets.spec.ts` runs on the phone project only. It covers the forum index (plain, and with the picker open), the general and chatter lists, and the seed thread (plain, and with the sheet open).
- **How it measures.** It presses where each target should be: it scrolls each control to the middle and requires it to be what `elementFromPoint` hits at its centre and 21 px either side, across and down. That counts a padded hit area and fails one that is clipped or overlapped. Only a dialog's controls are measured while it is open, and prose in `.post-body` is skipped.
- **What it first reported:** 24 offenders on the index, 9 on the list, and 128 on the thread page.
- **The fixes.** A `.hit` class in `base.css`: under the phone query, a transparent `::after` extends an element's target to at least 44×44 about its centre without moving layout. It is applied to:
  - forum names, last-post titles and "Start one" on the index;
  - thread titles in the rows;
  - both unread badges;
  - post ordinals, the reply-to indicator, the four footer actions and the thread's forum link;
  - the popover's "Go to".
- **Clipping.** A link with its own ellipsis or clamp now puts it on an inner span, because `overflow: hidden` would clip the `::after`.
- **Real 44 px boxes** for the phone wordmark and the list's "New thread".
- **Footer spacing.** The phone footer gap is 24 px, so the actions' targets do not overlap. At 16 px they did, and the check caught it.
- **A judgement call.** The badges are standalone controls, but they keep their pill size and reach 44 through `.hit`, as the inline links do. Growing the pill itself would change the mockup's row.
- **Checks on the check.** Switching `.hit` off made it name 150+ controls. Reverting each other fix made it fail.

### AC2, focus
`components/focus.ts` gains three things:
- `focusMainHeading`: the main h1, given `tabindex=-1` and focused with `preventScroll`.
- `routeFocuser`: the Router's `onRouteChange`.
  - It skips moves within one thread (pages, permalinks, a posted reply) so a composer keeps focus.
  - It starts from the loaded path, because preact-iso never calls it for the first render, so skipping a first call would have swallowed the first real navigation.
- `guardFocus`: a MutationObserver that notices focus has fallen to `body` with the last focused element gone. After a frame and a task, so a component can return focus first, it focuses the nearest former ancestor that still takes focus, or else the heading.

Also:
- **Skip link.** `Shell` adds "Skip to content" first; it focuses the heading without adding a hash. The shell's content is `#main`.
- **The popover** now returns focus to its opener.
- **Openers.** The sheet and the popover accept an opener only if it is a real control (`isControl`). Safari does not focus a clicked button, and the heading a route change focused would otherwise have been taken as the opener. A Vitest caught this.
- **Paging moves focus to the heading.** When a list pages by keyboard, the pager goes while the next page loads. That is the guard at work, and the test asserts it.

### AC3, motion
`base.css` has a `prefers-reduced-motion: reduce` block: animations and transitions at 0.01 ms, one iteration, and `scroll-behavior: auto`. Nothing animates today.
- `e2e/focus.spec.ts` sets transitions, animations and smooth scrolling through the CSSOM (the CSP refuses injected stylesheets) and asserts that every computed duration is 0.01 ms and that scrolling is `auto`.
- Switching the block off fails it.
- `src/a11y.test.ts` checks the block's declarations, and that nothing asks for `behavior: 'smooth'`.

### AC4, swipes
None exist. `src/a11y.test.ts` scans every non-test source for touch, pointer-move and swipe handlers, and fails on any not listed with its button equivalent (the list is empty). ComposerSheet says so in a comment. I did not walk the phone layout by hand; that is left to the review and to a person.

### A fix to TKT-01M29CEE6D's scroll memory
Looping the whole suite exposed a flake on "back from a thread", about 1 run in 10.
- **Cause:** a list restored each key only once. Going back and then forward before the page in between had loaded left the page returned to unrestored, at 0.
- **Fix:** it now restores on each showing of a key.
- **Per-entry mode:** `history.scrollRestoration` is kept per history entry (HTML spec), so it is now set to manual for every entry a list shows, not only the first.
- **Verification:** a new Vitest covers the case, and 12 consecutive full runs were green. The mutant I had called equivalent in CEE6D's notes (`restored.current === key` → `false`) was not quite equivalent; this is the case it missed.

### Verification
- tsc is clean.
- vitest: 761 pass.
- Playwright: 33 pass and 5 skipped. The skips are width-specific: targets on desktop, the skip link on the phone, the sheet on desktop.
- 12 full runs in a row were green.
- About 40 hand mutants, with these survivors, all judged equivalent:
  - the guard's `pending` de-duplication;
  - rAF alone against rAF plus a task in the guard, a timing margin the harness cannot order;
  - the popover's `isControl` against `inside` alone;
  - the restore's `pending` check.

## Summary

The client passes axe's WCAG 2.1 A and AA scan on the forum index, the thread list and the thread page, at both widths and in both themes, including with the picker and the sheet open. Two contrast fixes. On the phone, every control's target is at least 44 px, checked by pressing where it should be; a .hit class pads the targets of inline links, badges and post actions. Focus moves to each new screen's heading, and a guard keeps it from being lost with a removed element. There is a skip link, the popover returns focus, and reduced motion stops every transition and animation. A source scan fails any swipe handler that has no button. It also fixes the thread list's restore after a quick back then forward.
