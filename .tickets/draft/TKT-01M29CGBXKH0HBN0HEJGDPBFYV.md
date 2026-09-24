---
schema: 3
id: TKT-01M29CGBXKH0HBN0HEJGDPBFYV
title: Run the accessibility pass on the client
type: task
status: draft
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

Pre-groom plan (2026-09-24). Revise at claim, after reading the code as it stands then.

### Approach
1. Landmarks: the Shell renders `<header>` (top bar), `<nav aria-label="Primary">` (top nav or tab bar), `<main id="main">`, and a skip link to `#main`. Each screen has exactly one `<h1>` (forum name, thread title, "Forums"). Posts are `<article aria-labelledby>`.
2. Focus: `a11y/focus.ts` exports `focusMainHeading()`, called from the router's route-change hook after render. It sets `tabindex="-1"` and focuses the `h1`. Closing a dialog (popover, sheet, help overlay, more menu) returns focus to the opener. When a focused element is removed (collapse, tombstone replace), focus moves to the nearest stable ancestor control rather than to `body`.
3. Motion: `@media (prefers-reduced-motion: reduce)` in `base.css` sets transitions and animations to 0.01 ms and `scroll-behavior: auto`. JS `scrollIntoView` calls pass `behavior` from a `prefersReducedMotion()` helper.
4. Targets: standalone controls get `min-width` and `min-height: 44px` under the phone media query. Inline links (author names, ordinals, tag chips) get a `.hit` class that enlarges the hit area with padding and a matching negative margin, so the visual size is unchanged (docs/06 carve-out).
5. Swipes: no swipe handlers exist in Phase 1. If any are added, each needs a button equivalent; say so in a code comment near the Sheet.
6. Fix what the scans find across CEE4K, CEE5Y, CEE6D and CEE6V, in this ticket's PR or as notes on those tickets.

### Files
`web/client/src/a11y/focus.ts`, `src/shell/Shell.tsx`, the screens' headings, `src/styles/base.css`, `src/styles/layout.css`; `e2e/a11y.spec.ts`, `e2e/targets.spec.ts`.

### Tests
- AC1: `e2e/targets.spec.ts` at 390 px on the forum index, thread list and thread page (with the sheet open) enumerates `a, button, [role=button], input, select, summary, [tabindex="0"]`, excludes elements inside `.post-body` prose, measures `.hit` elements by their padded box, and asserts `min(width, height) >= 44`.
- AC2: Playwright navigates index, then list, then thread, and asserts `document.activeElement` is the `h1` each time. It collapses an expander and closes the popover, asserting `activeElement` is never `body`.
- AC3: Playwright with `reducedMotion: 'reduce'` asserts computed `transition-duration` on the sheet and pill is at most 0.01 s.
- AC4: a grep-style Vitest asserts no `touchstart`, `touchmove` or `pointermove` swipe handlers exist without an allowlisted button equivalent. The review also walks the phone layout by hand.
- AC5: `@axe-core/playwright` (a dev dependency) on the three screens at both widths and in both themes, with zero violations at the `wcag2a` and `wcag2aa` tags. Fix or document any rule disabled.

### Design
docs/07 "Accessibility"; docs/06 "Mobile" (Targets and gestures); the mockups' `aria-label`s (`mbtn`, the pager, the post reply button).

### Interactions
Depends on CEE4K and CEE6V. It also touches CEE5Y, CEE6D, CGBVN (sheet focus trap), CGBW6 (`aria-expanded`) and CGBX4 (focus visibility). CGBY1 runs these specs in the same Playwright job.
Open in docs: which axe rule set counts as "passes"; WCAG 2.1 AA is assumed.
