---
schema: 3
id: TKT-01M29CCC0FVTZ2YXNXB2EK4R4Q
title: Build the CSS system, themes, and breakpoints
type: task
status: ready
status_reason: null
priority: urgent
due_on: null
labels:
  - area:client
  - effort:M
assignees: []
milestone: phase-1
parent: TKT-01M29BN367F3YQZESM63VN26GP
origin: null
dependencies:
  - TKT-01M29BW9NWKJGM3ZQ2DJH8WPWH
blocks_on: none
references:
  - ref: design:mobile
    path: docs/06-thread-views.md
  - ref: design:mockups
    path: docs/mockups/generate.py
claim: null
archive: null
created_at: 2026-09-11T23:20:57Z
updated_at: 2026-09-24T05:21:20Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude/phase1-groom
  name: ""
extensions: {}
---

## Description

Custom properties for color, spacing, and type, a light and a dark theme, and the three breakpoints from docs/06: phone below 720 px, tablet 720 to 1100 px, desktop above 1100 px.

The mockups in `docs/mockups/` are the reference, including their type choices, and `generate.py` regenerates them. Match the mockups rather than approximating them, because they are the only place the intended result exists.

Safe-area handling belongs here: `viewport-fit=cover` with `env(safe-area-inset-*)` padding on the tab bar and reply bar, or the bottom bar sits under the home indicator on an iPhone.

## Acceptance criteria

- [ ] Light and dark themes are driven by custom properties and follow prefers-color-scheme with a user override
- [ ] The three breakpoints match docs/06 and layout changes are CSS rather than component swaps, except the navigation shell
- [ ] Safe-area insets are applied to the bottom bars through env(safe-area-inset-*) with viewport-fit=cover, and a Playwright check at 390 px confirms the padding rules apply; real-device verification is in TKT-01M38WTKSQ (Verify the Phase 1 client on real iOS and Android devices)
- [ ] Playwright screenshots at 390 px and 1440 px are committed beside the tests and compared against the matching file in docs/mockups/; every visible divergence is listed in the pull request description

## Implementation plan

Pre-groom plan (2026-09-24). Revise at claim, after reading the code as it stands then.

### Approach
1. `src/styles/tokens.css`: spacing, radius, type scale and font stacks as custom properties, taken from `docs/mockups/generate.py`. The color tokens are the `LIGHT` and `DARK` dicts there (bg, surface, surface2, ink, ink2, muted, rule, rule2, accent, accent_ink, accent_soft, unread, unread_soft, code_bg, shadow, thumb), named `--bg`, `--surface`, and so on.
2. `themes.css`: light by default; `@media (prefers-color-scheme: dark)` applies the dark values when `:root:not([data-theme])`; `[data-theme="light"]` and `[data-theme="dark"]` override. `state/theme.ts` holds `themePref` in localStorage and sets `data-theme` on `<html>`. The server-side settings endpoints are phase-4, so the override lives only on the device for now.
3. Breakpoints from docs/06: phone `max-width: 719.98px`, tablet 720 to 1100 px, desktop `min-width: 1100.02px`. CSS custom properties cannot be used inside media queries, so write the values literally and put a comment in one place. `state/layout.ts` uses the same 720 value for the shell's `isPhone`.
4. Self-host Newsreader, IBM Plex Sans and IBM Plex Mono (all OFL) as woff2 in `src/assets/fonts`, subset to Latin with `font-display: swap`. The mockups' Google Fonts link is blocked by the CSP.
5. Safe-area insets: `padding-bottom: env(safe-area-inset-bottom)` on `.tabbar` and `.replybar`, and left and right insets on the phone top bar. `viewport-fit=cover` is in the index.html from CCC02.
6. No `style="..."` strings in markup: the CSP has no `'unsafe-inline'` for style attributes. Preact's object styles go through the CSSOM and are allowed, but prefer classes.

### Files
`web/client/src/styles/*.css`, `src/assets/fonts/*`, `src/state/theme.ts`, `src/state/layout.ts`; imported from `main.tsx`.

### Tests
- AC1: Vitest for `theme.ts` (system, then override, then persisted). A Playwright test with `colorScheme: 'dark'` checks the computed `--bg` equals `#12161E`, and that `data-theme="light"` overrides it.
- AC2: a Playwright test resizes across 719, 720, 1100 and 1101 px on one route. It asserts the URL is unchanged, and that only the nav component differs in the DOM while layout is driven by computed styles.
- AC3, reworded per grooming: at 390 px emulated, assert that the tab bar's and reply bar's computed `padding-bottom` resolves from `env(safe-area-inset-bottom)` (a CSS-source grep plus a computed-style check), and that the viewport meta contains `viewport-fit=cover`. The real-device check belongs to TKT-01M38WTK.
- AC4, reworded: commit Playwright screenshots at 390 and 1440 px, light and dark, under `web/client/e2e/screenshots/`. Compare them by eye against the eight `docs/mockups/*.dc.html` files and list any divergence in the PR.

### Design
docs/06 "Mobile" (breakpoints, 44 px, safe area); docs/07 "Responsive"; `docs/mockups/generate.py` (tokens, `nav`, `mtabbar`, `mobile_front`, `mobile_thread`); `canvas.json` for the artboard sizes.

### Interactions
Depends on CCC02 and on BW9NW's decision (now recorded in docs/10 Q21). Every screen ticket uses these tokens. CGBXK checks targets and motion. The font weight counts toward the CGBYH budget if fonts are in scope there.
Open in docs: whether the theme override should later sync to server settings.

### Settled in grooming (2026-09-24)

- Fonts are self-hosted subset WOFF2 files, embedded and served same-origin (docs/07, decided 2026-09-24). Replace the mockups' Google Fonts links in anything the client ships.

## Notes

**agent:claude/phase1-groom** at 2026-09-24T05:06:25Z

Acceptance criteria rewritten during Phase 1 grooming (2026-09-24), agreed with the user as part of preparing the autonomous run. Rewritten criteria move to the end of the list, so their numbers changed. An agent cannot check against a device with a home indicator, so that check moved to TKT-01M38WTKSQ; the criterion here is what an emulated viewport can prove. 'Matches the mockups' now names its method: committed screenshots compared against docs/mockups/, with every divergence listed in the PR.
