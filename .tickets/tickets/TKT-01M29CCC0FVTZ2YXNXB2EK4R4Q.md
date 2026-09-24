---
schema: 3
id: TKT-01M29CCC0FVTZ2YXNXB2EK4R4Q
title: Build the CSS system, themes, and breakpoints
type: task
status: in-progress
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
claim:
  actor: agent:claude-code/phase1-run-415af6aa
  branch: run/ccc0f-css-system
  worktree: /home/sothr/.t3/worktrees/ketju/t3code-415af6aa
  commit: f2deccb2eb8d9d7eba546197062d4876940bd287
  session: null
  claimed_at: 2026-09-24T14:32:31Z
  expires_at: null
archive: null
created_at: 2026-09-11T23:20:57Z
updated_at: 2026-09-24T14:33:48Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase1-run-415af6aa
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

Claim-time plan (2026-09-24). It revises the pre-groom plan after reading the client at f2deccb (the thread page is merged).

### Settled in grooming (2026-09-24), carried forward
- Fonts are self-hosted subset WOFF2 files, embedded and served same-origin (docs/07, decided 2026-09-24). Replace the mockups' Google Fonts links in anything the client ships.

### Approach
1. **`src/styles/tokens.css`.**
   - The colour tokens are generate.py's `LIGHT` and `DARK` dicts, one custom property each, with underscores as hyphens: `--bg`, `--surface`, `--surface2`, `--ink`, `--ink2`, `--muted`, `--rule`, `--rule2`, `--accent`, `--accent-ink`, `--accent-soft`, `--unread`, `--unread-soft`, `--code-bg`, `--shadow`, `--thumb`.
   - Alongside them: the font stacks, the type scale the mockups use, the radius, and the 44 px minimum target.
   - Light is the default. Dark applies under `@media (prefers-color-scheme: dark)` when `<html>` has no `data-theme`, and `data-theme="dark"` or `"light"` overrides the system either way.
   - A Vitest contract test reads generate.py and tokens.css as text and asserts every colour matches in both themes, so the two cannot drift.
2. **`src/styles/fonts.css`.** Latin subsets from @fontsource 5.3.0, copied into `src/assets/fonts/` with each family's OFL licence. Nothing is added to package.json.
   - IBM Plex Sans 400, 500, 600 and 400 italic.
   - IBM Plex Mono 400 and 500.
   - Newsreader 400, 500 and 600.

   Vite hashes and emits them under /assets/, served same-origin and immutable, which `default-src 'self'` allows. All use `font-display: swap`.
3. **`src/styles/base.css`.**
   - The body type (Plex Sans at 15 px and 1.55 line height), the colours, links, and `.tnum`.
   - The breakpoints, written literally because custom properties cannot appear in media queries, with one comment naming the values: phone up to 719.98 px, tablet 720 to 1100 px, desktop from 1100.02 px.
   - The safe-area rules: `.tabbar` and `.replybar` pad the bottom with `env(safe-area-inset-bottom)`, and `.topbar` pads left and right with the matching insets. `viewport-fit=cover` is already in index.html.
   - The shell (CEE4K) and the reply bar (CGBVN) use these classes when they land.
   - `app.css` is replaced by these files.
4. **`src/state/theme.ts`.** `themePref` is `system`, `light` or `dark`, persisted in localStorage.
   - `applyTheme` sets or removes `data-theme` on `<html>`.
   - main.tsx applies it before the first render, so an override does not flash the system theme.
   - Syncing the preference to server settings stays open (phase 4).
5. **`src/state/layout.ts`.** `phoneQuery` (the same 719.98 px) and an `isPhone` signal from matchMedia, for the navigation shell: the one component swap the criterion allows.
6. **`thread.css`** moves onto the tokens. Its local fallback aliases go, and the thread page follows the theme.

### Tests
- **AC1:** Vitest covers theme.ts (system by default, an override, persistence, and a corrupt stored value) and the token contract against generate.py. It is verified in a local headless Chromium: the computed `--bg` under `colorScheme: dark` is `#12161E`, and `data-theme=light` overrides it.
- **AC2:** a Vitest source check that the media queries use exactly the docs/06 values. It is verified in local Chromium at 719, 720, 1100 and 1101 px on the thread page: the URL is unchanged and only the CSS differs.
- **AC3:** the rules exist and a Vitest source check asserts them, but the elements they style (the tab bar and reply bar) are built by CEE4K and CGBVN, and nothing in the repo runs Playwright (CGBY1). This criterion is ticked only if a Playwright check is actually committed; otherwise it is left unticked with a note and carried by TKT-01M39WBN4Q.
- **AC4:** screenshots are TKT-01M39WBN4Q's, which depends on this ticket, CEE4K and CGBY1. It is left unticked here with a note.

## Notes

**agent:claude/phase1-groom** at 2026-09-24T05:06:25Z

Acceptance criteria rewritten during Phase 1 grooming (2026-09-24), agreed with the user as part of preparing the autonomous run. Rewritten criteria move to the end of the list, so their numbers changed. An agent cannot check against a device with a home indicator, so that check moved to TKT-01M38WTKSQ; the criterion here is what an emulated viewport can prove. 'Matches the mockups' now names its method: committed screenshots compared against docs/mockups/, with every divergence listed in the PR.
