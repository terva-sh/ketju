---
schema: 3
id: TKT-01M29CEE4KNQBFP49FVSCRNZA4
title: Build the app shell with both navigation layouts
type: task
status: ready
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
claim: null
archive: null
created_at: 2026-09-11T23:22:04Z
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

The navigation shell: a top bar on desktop and a five-item bottom tab bar on phones, carrying Forums, Unread, New, Alerts, and Me with the same badge counts at both sizes.

This is the one place where layout is a component choice rather than CSS, because a tab bar and a top bar are different markup. Everything else reflows. Keeping that list at one item is what stops the phone layout from forking into a second product.

Phone screens also get a 52 px top bar with a back control, a truncating context title, search, and a "more" menu holding the thread actions that live in the desktop toolbar.

## Acceptance criteria

- [ ] The shell swaps between top bar and tab bar at the 720 px breakpoint with no route change
- [ ] The phone top bar truncates a long forum name rather than wrapping or pushing controls off screen
- [ ] Every desktop toolbar action is reachable from the phone more menu
- [ ] Badges show only counts the API provides and match between layouts; the Unread, New, and Alerts tabs route to placeholder screens until their Phase 3 screens land (docs/06)

## Implementation plan

Pre-groom plan (2026-09-24). Revise at claim, after reading the code as it stands then.

### Approach
1. `shell/Shell.tsx` renders `<TopBar/>` when `!isPhone.value` and `<PhoneTopBar/>` plus `<TabBar/>` when it is true. Both sit around one `<main>` that holds the router outlet. That swap is the only component-level layout switch (docs/07 "Responsive").
2. `TopBar.tsx` follows the mockup `nav()`: wordmark, Forums, Unread, New, Participated, Watching, search, moderator queue, and user. Reconcile the items with the five tabs: Alerts has no desktop entry in the mockup.
3. `TabBar.tsx` follows the mockup `mtabbar()`: Forums, Unread, New, Alerts, Me, each at least 44 px, with the safe-area inset from CCC0F. It is hidden on the thread page, where CGBVN's ReplyBar takes its place.
4. Badges come from the single `badges` computed signal in `state/viewer.ts` (unread = `Me.unread.threads`, alerts = `Me.notifications`), so both layouts read the same number. `Me` has no New count; see interfaces item 5.
5. `PhoneTopBar.tsx`: 52 px tall, with back, a context title using `min-width: 0` and `text-overflow: ellipsis` on one line, search, and the `MoreMenu`.
6. `shell/threadActions.ts` is one registry of thread actions (watch, share link, and moderator actions when `viewer.can_moderate`). The desktop toolbar and the phone MoreMenu both render from it, which is AC4 by construction.
7. Unread, New and Alerts link to `/unread`, `/new` and the notifications route, whose screens are phase-3 (CTY7K, CTY86). In Phase 1 they render `Placeholder`.

### Files
`web/client/src/shell/*`, `src/state/viewer.ts`, `src/state/layout.ts`, `src/styles/layout.css`.

### Tests
- AC1: Playwright loads `/f/general` at 1440 px, resizes to 390 px, and asserts the tab bar is present, the top bar is absent, and `page.url()` is unchanged. Then the reverse.
- AC2: a Vitest test renders TopBar and TabBar with one `viewer` fixture (from `internal/dto/testdata/golden/me.member.json`) and asserts equal badge text. Playwright repeats the check at both widths.
- AC3: Playwright at 390 px with a 120-character forum name asserts the title element's `scrollWidth > clientWidth` on one line, and that the search and more buttons stay inside the viewport.
- AC4: a Vitest test asserts that every entry in `threadActions` appears in both the toolbar and the MoreMenu render.

### Design
docs/06 "Mobile" (Navigation); docs/07 "Responsive"; docs/10 Q25; `generate.py` `nav`, `mtabbar`, `mtopbar`, `mbtn`; Main.dc.html and MobileFrontLight.dc.html.

### Interactions
Depends on CCC02 and CCC0F. The badge source is CA99R (`/me`). Consumed by every screen, CGBVN (reply bar replaces the tab bar), CGBXK (landmarks, focus), CGBX4 (shortcuts inert when `isPhone`), and CTY86 (Alerts, later).
Open in docs: the desktop placement of Alerts, and where the New count comes from.

### Settled in grooming (2026-09-24)

- Five tabs ship now. Unread, New and Alerts route to placeholder screens until Phase 3 (docs/06, decided 2026-09-24). A badge shows only a count the API provides.

## Notes

**agent:claude/phase1-groom** at 2026-09-24T05:17:38Z

Criterion rewritten during Phase 1 grooming (2026-09-24). It moves to the end of the list. The user decided on 2026-09-24 that Phase 1 ships the final five-tab bar with placeholder screens. The API has no 'New' count yet, so the criterion no longer demands one.
