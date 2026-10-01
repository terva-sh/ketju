---
schema: 3
id: TKT-01M3S3ZZSZQHQN41DQKMM5F8D2
title: Close the small visual gaps between the client and the mockups
type: task
status: in-progress
status_reason: null
priority: low
due_on: null
labels:
  - area:client
assignees: []
milestone: phase-3
parent: TKT-01M29BN367F3YQZESM63VN26GP
origin: null
dependencies:
  - TKT-01M29CGBZDVGWNDTKHJFHHSGY5
blocks_on: none
references: []
claim:
  actor: agent:claude-code/phase3-visual-client
  branch: run/s3zzsz-visual-gaps
  worktree: /home/sothr/workspace/git.local.sothr.com/terva-sh/ketju/.claude/worktrees/agent-aa69ff635c22d83cb
  commit: 0009f715bf9eea8fb42021a4354562511cf33b81
  session: null
  claimed_at: 2026-10-01T22:52:33Z
  expires_at: null
archive: null
created_at: 2026-09-30T12:17:55Z
updated_at: 2026-10-01T23:10:15Z
created_by:
  id: agent:claude-code/phase2-run-39wbn4q
  name: ""
updated_by:
  id: agent:claude-code/phase3-visual-client
  name: ""
extensions: {}
---

## Description

Found comparing the committed screenshots in `web/client/e2e/screenshots/` with `docs/mockups/` (TKT-01M39WBN4Q). Each is small, none is decided anywhere, and none has an owner. The IDs are the divergence list's in the note of 2026-09-30 on TKT-01M39WBN4Q.

- T1: moved in grooming on 2026-10-01 to TKT-01M3S3ZF6J (Show who started a thread, when, and its post count under the title), which redraws the same thread head.
- T4: the pager fills the current page with the accent colour, and its "posts 51–60 of 60" span is in the sans face. The mockup fills it with ink (#1B2130 light, near-white dark), draws wider 48 px page buttons, and sets the span in mono (`web/client/src/components/thread/thread.css`).
- T10: the thread column is 880 px wide (`thread.css`, `max-width`). The mockup's is 900 px.
- P1: the phone top bar's title is bold ink. The mockup's is muted and regular (`shell/shell.css`, `.topbar.phone .title`).
- P4: the phone pager's steps are text ("‹ Prev", "Next ›"). The mockup's are square icon-only buttons. The page picker itself is TKT-01M29CGBZ.
- M1: the phone front page centres the wordmark. The mockup left-aligns it (`shell.css` centres every `.title`).
- D2: the desktop top bar shows the handle as text. The mockup shows an avatar circle beside the name.
- D1: the desktop top bar's search is a compact button. The mockup's is a 240 px field, "Search threads and posts", with a "/" key hint. The search screen is TKT-01M29CTY8T.
- F9: on the phone front page the lock icon follows the forum's name. The mockup puts it at the card's right edge.

### What to do
Match each to the mockup, or record in docs/06 or docs/07 why the client differs. Refresh the affected screenshots with `KETJU_SCREENSHOTS=1 npx playwright test e2e/thread.spec.ts e2e/forum-index.spec.ts`.

## Acceptance criteria

- [x] Every axe scan in a11y.spec.ts, forum-index.spec.ts and thread.spec.ts passes at 390 px and 1440 px in both themes, and forum-index-*, thread-* and thread-list-*.png are refreshed
- [ ] The dist from go generate ./internal/web/spa is committed, npm run budget keeps the reader within 70000 B gzip, and the PR states the reader's growth
- [x] D1: the desktop top bar keeps its link to /search, as docs/07 Search decided, with no / shortcut and no key hint, and docs/06 Paged, not infinite records why it differs from the mockup (decided by the run 2026-10-01)
- [x] Each of T4, T10, P1, P4, M1, D2 and F9 matches its mockup at its width. Each is checked in e2e where it can be measured: the current page's fill, the 900 px column, the phone title's colour, the 44 px icon steps, the wordmark's alignment and the lock's position. The rest are checked in the refreshed screenshots

## Implementation plan

Groomed 2026-10-01 for the Phase 3 run (docs/14), against main at a599b48. Revised against main at 3890b38, after the search lane's plan merged.

### Settled in grooming

- **T1 left this ticket.** The breadcrumb, "Forums › General", moved to TKT-01M3S3ZF6J (Show who started a thread, when, and its post count under the title), which redraws the same `thread-head`. Eight items remain: T4, T10, P1, P4, M1, D2, D1 and F9.
- **T4: only the fill and the face differ.** The description says the mockup's page buttons are 48 px wide. `docs/mockups/generate.py` (`thread().pager`) draws them at `min-width: 32px` with `padding: 0 8px`, as `thread.css` `.pager .page` already does. What differs is the current page's fill, ink rather than accent, with `bg` text, and the "posts 51–75 of 212" span, mono rather than sans.
- **D1: the top bar keeps its search link, and there is no code change.** Decided by the run (2026-10-01, pending maintainer review), and recorded in docs/06 "Paged, not infinite".
  - docs/07 "Search", from the search lane (TKT-01M3V70X0F), had already chosen the top bar's link to `/search` over a field in the bar. This ticket follows that choice, so the two run decisions do not disagree about one control.
  - The mockups' `/` key hint is left out. Every shortcut is a single key with no off switch, which WCAG 2.1.4 asks for (TKT-01M3V9DWTN). `/` itself is TKT-01M3V7GE59's.
  - So D1 is closed by the docs/06 line that says why the client differs from its mockup.
  - This revises the D1 of review 1710 and earlier, which had drawn a 240 px field. The dependency on TKT-01M29CTY8T, which D1 alone needed, is removed.
- **P4 waits for the page picker.** TKT-01M29CGBZD rebuilds the phone pager's middle control into the picker sheet's opener, so this ticket waits for it. Then P4 makes the previous and next steps 44 px icon-only squares, with the `back` icon mirrored for next. Their `aria-label`s stay "Previous page" and "Next page".
- **No docs/06 rule conflicts with the other items**, so each follows its mockup. M1, P1 and F9 come from `mobile_front()` and `mtopbar()`, and D2, T4 and T10 from `nav()` and `thread()`.
- **Layout switches in CSS, and one set of components renders at every size** (docs/06 "Breakpoints").

### Approach

1. **T4** (`thread.css`). `.pager .current` uses `background: var(--ink)`, `border-color: var(--ink)` and `color: var(--bg)`. `.pager .span` uses `font-family: var(--font-mono)`. Check the contrast in both themes. The axe scan covers it.
2. **T10** (`thread.css`). Size `.thread-page` so the post column measures 900 px at 1440 px. Measure the `.posts` box rather than trust `max-width`, since the page has 16 px of padding.
3. **P1** (`shell.css`). `.topbar.phone .title` becomes muted, regular weight, at 13 px. The wordmark (`.title.wordmark`) keeps its serif face.
4. **M1** (`shell.css`). The phone wordmark sits left, with about 10 px of padding, as `mobile_front()` draws it. A screen's context title stays centred.
5. **P4** (`Pager.tsx`, `pager-narrow`). Use `Icon name="back"` in each step, with next mirrored by CSS, inside a 44 px square box with a border. The disabled placeholder keeps its size.
6. **D2** (`TopBar.tsx`). A signed-in reader's handle is preceded by a 28 px `Avatar` (`components/thread/Avatar.tsx`) of `viewer.user`, with `alt=""` or `aria-hidden` as `Avatar` already does. "Sign in" is unchanged.
7. **D1.** No code. The docs/06 line is the record.
8. **F9** (`forums.css`, phone block). The lock icon sits at the card's right edge, before the unread badge, as `mobile_front().row` draws it.
9. **Screenshots and build.** Refresh `forum-index-*`, `thread-*` and `thread-list-*.png` at both widths, since the top bar is in all three. Run `go generate ./internal/web/spa` and `npm run budget`.

**Reader bundle.** All of this is in the reader. Estimated at about +180 B gzip: about 150 B of CSS and about 30 B for the avatar. Measure it.

### Tests

- `Shell.test.tsx`:
  - "the desktop bar shows the reader's avatar beside the handle".
  - "an anonymous reader sees Sign in and no avatar".
- A new Pager case: "the phone steps are icon buttons named Previous page and Next page".
- `e2e/thread.spec.ts` at 1440 px:
  - The current page's computed background equals `--ink`.
  - The font family of `.pager .span` is the mono token.
  - The `.posts` box is 900 px wide.
- `e2e/thread.spec.ts` at 390 px: the top bar title's colour is `--muted`, and the steps are 44 by 44 px with no text.
- `e2e/forum-index.spec.ts` at 390 px:
  - The wordmark's box starts within 24 px of the bar's left edge.
  - In the garden row, which is members-only and so carries the lock, the lock's box is right of the name's and next to the right edge.
- Every axe scan in `a11y.spec.ts`, `forum-index.spec.ts` and `thread.spec.ts` passes at both widths and in both themes.

### Out of scope

- The page picker sheet: TKT-01M29CGBZD, a dependency.
- The breadcrumb, T1: TKT-01M3S3ZF6J.
- Relative times anywhere: the maintainer decided on absolute times on 2026-10-01 (docs/06).
- A field in the top bar: docs/07 "Search" (TKT-01M3V70X0F) chose the link.
- The `/` shortcut: TKT-01M3V7GE59. It waits for the off switch for single-key shortcuts (WCAG 2.1.4), TKT-01M3V9DWTN, filed in grooming with no milestone.

## Notes

**agent:claude-code/phase3-groom-polish** at 2026-10-01T07:56:06Z

run decision: pending maintainer review. D1: on desktop, / focuses the top bar's new 240 px search field, a real form to /search?q=. Recorded in docs/06 'Paged, not infinite' with the alternatives. Grooming also changed this ticket's shape. T1 moved to TKT-01M3S3ZF6J. T4's 48 px width is not a divergence, because docs/mockups/generate.py draws the page buttons at a 32 px minimum, as the client does. The ticket now depends on TKT-01M29CTY8T (D1) and TKT-01M29CGBZD (P4).

**agent:claude-code/phase3-groom-polish** at 2026-10-01T08:43:26Z

Terva review 1710 on PR #269 (head 151a234, base a599b48), medium, 'Do not add another unmodifiable single-key shortcut'. Fixed by dropping the shortcut. D1 is now the 240 px field alone, with no / shortcut and no key hint, until TKT-01M3V9DWTN gives single-key shortcuts an off switch (WCAG 2.1.4). docs/06, the plan and the criteria say so. This supersedes this ticket's earlier run-decision note on /.

**agent:claude-code/phase3-groom-polish** at 2026-10-01T10:23:03Z

run decision: pending maintainer review. This supersedes the earlier D1 notes. While carrying this grooming onto main at 3890b38, a contradiction showed up. The search lane's docs/07 'Search' (TKT-01M3V70X0F, merged first) chose the top bar's link to /search over a field in the bar. This ticket's D1 had drawn the mockups' 240 px field. Neither decision is the maintainer's, so the safer reading is the one already on main: D1 keeps the link, has no code, and the / key hint stays out. / belongs to TKT-01M3V7GE59 and waits on TKT-01M3V9DWTN's off switch. docs/06 'Paged, not infinite' says why the client differs from its mockup. The dependency on TKT-01M29CTY8T, which only D1 needed, is removed.

**agent:claude-code/phase3-visual-client** at 2026-10-01T22:52:37Z

Acceptance criterion 2's 70000 B figure is superseded: the maintainer raised the reader budget to 120000 B gzip on 2026-10-01 (#331). This ticket is judged against 120000 B; the criterion text is left as filed. The reader is 73762 B on main at 0009f715. D1 needs no code: docs/06 'Paged, not infinite' already carries the D1 decision on main at 0009f715.

**agent:claude-code/phase3-visual-client** at 2026-10-01T23:10:15Z

Prepared on run/s3zzsz-visual-gaps from main at 0009f715, before the dist is regenerated (docs/14 "Client lanes may prepare").

Each item was re-checked against main at 0009f715 before it changed:
- T4 (thread.css): `.pager .current` is ink-filled with `bg` text at weight 600, as `thread().pager` draws it; `.pager .span` is mono.
- T10 (thread.css): `.thread-page` max-width went from 880 px to 900 px. The padding stays outside the content box, so `.posts` measures 900 px at 1440 px.
- P1 (shell.css): `.topbar.phone .title` is muted, weight 400, 13 px. The wordmark rule sets ink back.
- M1 (PhoneTopBar.tsx, shell.css): with no screen context the bar no longer renders `tb-spacer` before the wordmark. The wordmark is left-aligned with 10 px of padding and is only as wide as the name, so the empty middle of the bar is no longer a link home. A titled screen keeps its spacer and its centred title.
- P4 (Pager.tsx, thread.css): after #340 the phone steps are 44 px squares holding `Icon name="back"`, with next mirrored by `scaleX(-1)`. The inactive placeholder holds an icon too, so the pager does not shift. The aria-labels are unchanged.
- D2 (TopBar.tsx, shell.css): `Avatar` of `viewer.user` at 28 px, then the handle in `span.handle`, which now carries the 16ch ellipsis that `.who` used to carry.
- F9 (forums.css, phone block): `.forum-lock { margin-left: auto }` puts the lock at the right end of the name column, before the unread badge column.
- D1: no code. docs/06 "Paged, not infinite" on main already records why the bar keeps the /search link.

Tests: tsc; vitest 2445 passed (new: avatar beside handle, anonymous sees no avatar, the index bar opens with the wordmark, icon-only phone steps); `go test -race ./...` passes; the full Playwright suite at 390 and 1440 px passes (240 passed, 18 skipped), with the new e2e measures in thread.spec.ts and forum-index.spec.ts.

The screenshots were compared with the mockups by eye. thread-desktop-{light,dark}: page 3 is ink-filled (near-white in dark), the span is mono, the column is 900 px, and the 28 px avatar circle sits beside "alice". thread-phone-dark: "General" is muted and regular, and the steps are icon squares with the disabled next mirrored. forum-index-phone-light: the wordmark is at the left, and the garden's lock sits at the right before "1 new". thread-list-phone: the title is muted.

Reader bundle: 73762 B before and 73871 B after (+109 B), within the 120000 B budget.

No new run decisions.
