---
schema: 3
id: TKT-01M29CEE4KNQBFP49FVSCRNZA4
title: Build the app shell with both navigation layouts
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
  - TKT-01M29CCC0FVTZ2YXNXB2EK4R4Q
  - TKT-01M29CCC02K8F14P0SKAYFMSE6
  - TKT-01M29BW9P4PXMFKF4VAP8K9QPD
blocks_on: none
references:
  - ref: design:mobile-nav
    path: docs/06-thread-views.md
  - ref: pr:70
    path: null
claim: null
archive: null
created_at: 2026-09-11T23:22:04Z
updated_at: 2026-09-24T19:41:02Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase1-run-415af6aa
  name: ""
extensions: {}
---

## Description

The navigation shell: a top bar on desktop and a five-item bottom tab bar on phones, carrying Forums, Unread, New, Alerts, and Me with the same badge counts at both sizes.

This is the one place where layout is a component choice rather than CSS, because a tab bar and a top bar are different markup. Everything else reflows. Keeping that list at one item is what stops the phone layout from forking into a second product.

Phone screens also get a 52 px top bar with a back control, a truncating context title, search, and a "more" menu holding the thread actions that live in the desktop toolbar.

## Acceptance criteria

- [x] The shell swaps between top bar and tab bar at the 720 px breakpoint with no route change
- [x] The phone top bar truncates a long forum name rather than wrapping or pushing controls off screen
- [x] Every desktop toolbar action is reachable from the phone more menu
- [x] Badges show only counts the API provides and match between layouts; the Unread, New, and Alerts tabs route to placeholder screens until their Phase 3 screens land (docs/06)

## Implementation plan

Revised at claim (2026-09-24), after reading the client as it stands.

### Approach
1. `shell/Shell.tsx` wraps the router in `app.tsx`. It renders `TopBar` when `!isPhone.value`, and `PhoneTopBar` plus `TabBar` when it is true. The screen sits between them in one element that stays at both widths, so crossing 720 px re-renders only the bars. This is the only component-level layout switch.
2. `shell/nav.ts` holds the destinations both layouts draw from.
   - Tabs: Forums `/`, Unread `/unread`, New `/new`, Alerts `/notifications`, Me (`/u/{handle}`, or `/settings` when signed out).
   - Top bar: the mockup's links (Forums, Unread, New, Participated, Watching), then search, the Alerts bell, the moderation queue (moderator/admin role only, as the mockup's inbox icon), and the reader.
3. Badges come from one `badges` computed in `state/viewer.ts`: unread = `Me.unread.threads`, alerts = `Me.notifications`.
   - New has none, because /me has no count for it.
   - A badge shows only above zero, and caps at 99+.
4. `state/shell.ts` has `shellContext` and `useShellContext`. A screen publishes a title, a back target and its actions.
   - `PhoneTopBar` draws them in 52 px: back, a one-line title (`min-width: 0`, ellipsis, nowrap), search, and `MoreMenu`.
   - Without a context it draws the wordmark.
   - `Placeholder` sets its title.
   - `ThreadScreen` sets the forum name, back to the forum, and `threadActions`.
5. `shell/threadActions.ts` is the one list of thread actions. On desktop, `Toolbar` renders it under the thread title; on phone, `MoreMenu` renders it. Phase 1 has only "Copy link". Watch (CA9CE) and the moderator actions add themselves with their tickets, and /me has no permission to gate the moderator ones on yet.
6. `/notifications` is a route with a placeholder screen ('Alerts'), and joins docs/07's URL scheme and routes.test.tsx.
7. The tab bar stays on the thread page until CGBVN's reply bar exists to replace it.

### Files
`web/client/src/shell/*`, `src/components/Icon.tsx`, `src/state/{shell,viewer}.ts`, `src/app.tsx`, `src/routes.tsx`, `src/routes.test.tsx`, `src/screens/{Placeholder,ThreadScreen}.tsx`, `docs/07-api-and-clients.md`, and the rebuilt `internal/web/spa/dist`.

### Tests
`src/shell/Shell.test.tsx` covers:
- The swap keeps the address and the screen element.
- Badges are equal in both layouts for me.member, cap at 99+, and are absent at zero and for me.anon.
- Tab hrefs.
- The moderator queue.
- aria-current.
- Phone titles and back.
- The truncation rule.
- Toolbar actions equal more-menu actions.
- The menu runs, closes, and returns focus, including on Escape and on an outside click.
- The context is not taken down by a superseded screen.

It also checks the width in Chromium (local, not committed). The Playwright versions of AC1–3 belong to the gate (CGBY1).

### Design
docs/06 "Mobile" (Navigation); docs/07 "Responsive"; docs/10 Q25; `generate.py` `nav`, `mtabbar`, `mtopbar`, `mbtn`; Main.dc.html and MobileFrontLight.dc.html.

### Interactions
- Depends on CCC02 and CCC0F. The badge source is CA99R (`/me`).
- Consumed by every screen; CGBVN (the reply bar replaces the tab bar on the thread page); CGBXK (landmarks, focus); CGBX4 (shortcuts inert when `isPhone`); CTY86 (the Alerts screen replaces the /notifications placeholder).

### Settled in grooming (2026-09-24)

- Five tabs ship now. Unread, New and Alerts route to placeholder screens until Phase 3 (docs/06, decided 2026-09-24). A badge shows only a count the API provides.

### Decided at claim (2026-09-24)

- Alerts routes to /notifications, which is added to docs/07.
- On desktop, Alerts is a bell with its badge on the right of the top bar.

## Notes

**agent:claude/phase1-groom** at 2026-09-24T05:17:38Z

Criterion rewritten during Phase 1 grooming (2026-09-24). It moves to the end of the list. The user decided on 2026-09-24 that Phase 1 ships the final five-tab bar with placeholder screens. The API has no 'New' count yet, so the criterion no longer demands one.

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T19:03:34Z

Decisions the user made at claim (2026-09-24), because both change what a member sees (docs/12 'Gaps the design leaves open'):
1. The Alerts tab routes to /notifications, which mirrors the API's GET /notifications. It is added to docs/07's URL scheme in this PR, with a placeholder screen until the Phase 3 Alerts screen (TKT-01M29CTY86) lands. /alerts, and blocking for a design pass, were the alternatives offered.
2. On desktop, Alerts is a bell icon with the notifications badge on the right of the top bar, beside search and the user, where the mockup puts the moderator-queue icon. The nav links stay as the mockup has them. A nav item after Watching, and no desktop entry until Phase 3, were the alternatives offered.

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T19:17:12Z

Built and verified (2026-09-24).

- **Vitest.** 523 tests pass, 17 of them new in src/shell/Shell.test.tsx.
- **Mutants.** 37 mutants over shell/*, state/shell.ts, the badges computed, shell.css and the ThreadScreen/Placeholder wiring. All are killed. Three survived the first pass and each got a test:
  - `current()` matched a prefix rather than a segment.
  - An empty Toolbar rendered an empty role=toolbar.
  - An unmount cleared a newer screen's context.
- **Chromium (vite dev, playwright-core, local).**
  - At 390 px, a 97-character title renders one line (23 px tall), scrollWidth 677 > clientWidth 230, with search and More inside the viewport (right edges 334 and 382 of 390).
  - Five tabs are 78x56 each, and the top bar is 52 px plus its border.
  - Resizing 390→1440→390 swaps the bars with the URL unchanged.
  - With me.moderator, badge text is identical at 1440 and 390 ('3 unread threads', '9 notifications').
  - Screenshots are in ~/.cache/ketju-gotmp/shots/shell-*.png, not committed.

Choices made without a decision, none of them changing what a member can see or do beyond the mockup:
- The Me tab goes to /settings when signed out, and the top bar says "Sign in" there. Where signing in lives is still the settings screen's to decide.
- The moderation queue icon shows for role moderator or admin, from /me's user.role. It is only a link; the server gates /admin. It has no count, because /me has none.
- The tab bar stays on the thread page until CGBVN's reply bar exists to replace it.
- Copy link copies the thread's canonical URL (/t/{id}/{slug}), not the page or post, and fails silently where there is no clipboard. There is no toast system yet to confirm it.

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T19:41:02Z

Review 490 (head e5f6f92): three medium findings, all accepted and fixed.

1. **The desktop bar overflowed near 720 px.**
   - Measuring the worst case (moderator, 30-character handle, 99+ badges) in Chromium at 720 px showed more than the reviewer said: the nav had `min-width: 0` and shrank under its links, so the search box overlapped "Watching".
   - Fix:
     - The wordmark, links, search and icons are `flex: none`.
     - The tools group and the reader's handle take `min-width: 0`, and the handle is capped at 16ch with an ellipsis.
     - A tablet block (the allowed 720–1100 px form) closes the gaps and padding, and hides the search box's label to screen-reader-only text.
   - Measured at 720, 800, 1100 and 1101 px: no two top-bar links overlap, nothing passes the viewport, and only the handle is cut.
2. **A not-found thread kept its context.**
   - A not-found thread now publishes no shell context, so the phone bar shows the wordmark, no back and no menu.
   - Actions are offered only once the thread's header is known, from the answer or the cache, and are built from the header's id and slug rather than the address's.
3. **An outside press dropped focus.**
   - It now returns focus to the More button when focus was in the menu.
   - It leaves focus alone when the press is on something that already has it.

Tests added for each. 527 Vitest tests pass, and all 46 shell mutants are killed, including one per fix.

## Summary

The navigation shell landed: a desktop top bar and a phone top bar plus five-tab bar, swapped at 720 px by state/layout.ts with the screen and address untouched. Badges come from one computed over /me (Unread, Alerts), equal in both layouts. Thread actions are one registry rendered as the desktop toolbar and the phone more menu (Copy link for now). /notifications joins docs/07 with a placeholder until TKT-01M29CTY86.
