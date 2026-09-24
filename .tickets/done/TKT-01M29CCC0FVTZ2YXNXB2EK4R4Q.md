---
schema: 3
id: TKT-01M29CCC0FVTZ2YXNXB2EK4R4Q
title: Build the CSS system, themes, and breakpoints
type: task
status: done
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
  - ref: ticket:TKT-01M39WBN4QAMNRBXCGD85CDJF0
    path: null
  - ref: pr:65
    path: null
claim: null
archive: null
created_at: 2026-09-11T23:20:57Z
updated_at: 2026-09-24T14:52:31Z
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

- [x] Light and dark themes are driven by custom properties and follow prefers-color-scheme with a user override
- [x] The three breakpoints match docs/06 and layout changes are CSS rather than component swaps, except the navigation shell
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

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T14:41:14Z

### What was built
**`src/styles/tokens.css`.**
- The colours are generate.py's LIGHT and DARK dicts, one custom property per key.
- Light is the default. Dark applies under `prefers-color-scheme: dark` when `<html>` has no data-theme, and `data-theme` light or dark is the reader's override either way.
- There are eight avatar colours for each theme. The first four are the mockups' own, which are lighter in dark mode, with initials in the surface colour.
- The file also holds the font stacks, the text size and leading, the radius, and the 44 px `--target`.

**`src/styles/fonts.css`.** Nine Latin WOFF2 files, about 195 kB, from @fontsource 5.3.0, with each family's OFL licence beside them:
- IBM Plex Sans 400, 500, 600 and 400 italic;
- IBM Plex Mono 400 and 500;
- Newsreader 400, 500 and 600.

Nothing was added to package.json. Vite hashes the files into /assets/.

**`src/styles/base.css`.**
- The body type and colours.
- The breakpoint forms, written out once in a comment.
- The safe-area rules for `.tabbar`, `.replybar` and `.topbar`.
- `app.css` is gone.

**`src/state/theme.ts`.** `themePref` (system, light or dark) in localStorage. A stored value that is unknown or unreadable counts as system. `applyTheme` runs in main.tsx before the first render.

**`src/state/layout.ts`.** `phoneQuery` and an `isPhone` signal for the navigation shell.

**thread.css** now uses only tokens: its fallback aliases and literal colours are gone.

**spa.go** serves `.woff2` as `font/woff2`. Go's built-in MIME table lacks it, and without the host's mime.types the fonts would go out as octet-stream. `TestTheFontsAreServed` checks that all nine fonts are referenced from /assets/, typed, immutable and same-origin. The mutant that drops the header survives on this host, whose /etc/mime.types maps woff2. The comment in spa.go says so.

**vite.config.ts.**
- `test.css: true`. Without it Vitest hands every stylesheet over empty, even as `?raw`, and my first breakpoint scan passed vacuously. The tests now also assert that the sheets were actually read.
- `server.fs.allow` adds `docs/mockups`, so the token test can read generate.py.

### Verification
- **Vitest.**
  - The token contract: every LIGHT and DARK value equals its token in `:root`, in the system-dark rule, and in `[data-theme=dark]`, and the two dark rules match.
  - A breakpoint scan over every stylesheet: only the three docs/06 forms appear, and no source sets a style attribute, which the CSP forbids.
  - The safe-area rules and `viewport-fit=cover`.
  - theme.ts: the default, stored values, a corrupt value, storage that throws, apply, persist, and forget.
  - The suite has 473 tests.
- **Mutation.** 13 mutants on the CSS system, all killed. The woff2 header mutant is explained above.
- **Local headless Chromium, against the dev server with a stubbed API; nothing committed.**
  - `--bg` is #F6F7FA on a light system and #12161E on a dark one.
  - `data-theme=light` under a dark system gives #F6F7FA.
  - A stored dark choice under a light system gives data-theme=dark and #12161E before the first paint.
  - The Plex Sans (regular and italic), Plex Mono and Newsreader faces load from same-origin files.
  - At 719 px the pager shows its phone row and avatars are 30 px; at 720, 1100 and 1101 px it shows the wide row and avatars are 36 px. The URL did not change at any width.
  - The thread page renders correctly in dark.

### Unticked
- **AC3: the committed Playwright check at 390 px.** The rules exist and are tested at the source, but the bars they apply to arrive with CEE4K and CGBVN, and Playwright arrives with CGBY1.
- **AC4: screenshots against the mockups.** For the same reason.

Both are carried by draft TKT-01M39WBN4Q (Commit thread page screenshots at 390 and 1440 px against the mockups), now widened to cover them and linked here.

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T14:52:31Z

Answered review 423 (medium, "apply a stored theme before the stylesheet can paint").

The module script that ran applyTheme is deferred, so a reader whose stored choice differs from the system's could see the other theme's background first. The fix is src/theme-early.js: a classic, blocking script that reads the same key and values as state/theme.ts and sets data-theme on <html>. A small Vite plugin (vite.config.ts earlyTheme) emits it as a hashed asset and puts it first in <head>. In development it is served straight from src.

Alternatives considered:
- An inline nonce'd script in index.html. The CSP would allow it, but TestTheShellHasNoInlineScriptOrStyle (internal/web) holds the shell to no inline body at all, per the earlier ticket's AC. Rejected rather than weakening that test.
- Hand-placing the script in index.html. Vite does not bundle a classic script and warns, and the path would not be hashed, while /assets is served immutable. Rejected.

Tests: src/styles/theme-early.test.ts runs the script against theme.ts's key and values (stored light and dark apply; system, unknown and empty values leave <html> alone; storage that throws is survived). It also checks that the built shell carries it as a classic script ahead of the module and the stylesheet. Mutants killed: the dark branch dropped, the key renamed, the try/catch removed (3 of 3).

Browser check (Chromium, vite dev): with a stored dark choice on a light system, and the reverse, data-theme was set while document.body did not exist yet, so before any paint. With nothing stored the system theme applies. go test ./internal/web/... passes, including the nonce and no-inline shell tests.

## Summary

The CSS system landed. The design tokens are generate.py's light and dark colours exactly, held together by a contract test. The system theme is followed and a reader's override persists on this device. The three docs/06 breakpoints are enforced by a scan of every stylesheet. The mockups' three font families are self-hosted as Latin WOFF2 and served as font/woff2. Safe-area rules are in place for the bars that CEE4K and CGBVN will add. The thread page now uses only tokens and follows the theme. AC1 and AC2 are met. The Playwright check for AC3 and the screenshots for AC4 are carried by TKT-01M39WBN4Q.
