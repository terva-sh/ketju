---
schema: 3
id: TKT-01M29CGBVNWMMSRTXA4N42XBBS
title: Build the mobile composer sheet and sticky reply bar
type: task
status: draft
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

On a thread page the tab bar is replaced by a sticky reply bar showing the reply-to chip and the first line of any draft. Tapping it opens the composer as a full-screen sheet with the same toolbar, preview tab, and server-rendered preview. Reply and quote from a post prefill that sheet.

docs/09 names the specific failure to avoid: a composer fighting the on-screen keyboard. The toolbar pins above the keyboard, and the sheet can show the quoted post above the text area, which is the argument for a sheet over an inline composer.

The post list needs bottom padding equal to the bar height, or the reply bar covers the last post, which is the one a reader most wants to see while replying.

## Acceptance criteria

- [ ] The post list has bottom padding equal to the reply bar height so the last post is never covered
- [ ] Reply and quote from a post open the sheet prefilled
- [ ] A draft in progress shows in the reply bar and survives closing the sheet
- [ ] The toolbar stays above the on-screen keyboard in Playwright's emulated phone viewport, positioned from the visualViewport API; real iOS and Android verification is in TKT-01M38WTKSQ (Verify the Phase 1 client on real iOS and Android devices)

## Implementation plan

Pre-groom plan (2026-09-24). Revise at claim, after reading the code as it stands then.

### Approach
1. `composer/ReplyBar.tsx`: on the thread page, when `isPhone`, the Shell hides `TabBar` and renders `ReplyBar`. It shows the reply-to chip and the first line of `composer.source` (ellipsised), or a placeholder, plus a post button, matching `mobile_thread` in `generate.py`. Heights come from a `--replybar-h` custom property, and the bar includes `env(safe-area-inset-bottom)`.
2. `.post-list` gets `padding-bottom: calc(var(--replybar-h) + env(safe-area-inset-bottom))` in the phone media query, so both values come from one token.
3. `composer/ComposerSheet.tsx` (on the shared `components/Sheet.tsx`) is a full-screen fixed layer holding the same `Composer` in sheet mode. When `replyTo` is set, it shows the quoted or replied post above the textarea. It is a dialog with `aria-modal` and a focus trap, and Escape or back closes it.
4. Keyboard: the sheet uses `height: 100dvh`, plus a `visualViewport` resize listener that sets `--kb-inset`, so the toolbar pins above the on-screen keyboard. The toolbar is `position: sticky; bottom: var(--kb-inset)`.
5. Post actions: on the phone, reply and quote set `composer.replyTo` or insert the quote (the CEE79 logic), then set `sheetOpen = true`.
6. Draft persistence: closing the sheet leaves `composer.source` untouched; the ReplyBar reads it and the server draft (CA9CE) keeps it.

### Files
`web/client/src/composer/{ReplyBar,ComposerSheet}.tsx`, `src/components/Sheet.tsx`, `src/shell/Shell.tsx` (hide the TabBar on the thread route), `src/styles/components.css`.

### Tests
- AC1, reworded per grooming: Playwright at 390 px emulated opens the sheet, focuses the textarea, and fires a synthetic `visualViewport` resize, checking that the toolbar's bounding box stays inside the reduced visual viewport. A CSS-source check confirms `env(safe-area-inset-bottom)`. The real iOS and Android keyboard check moves to TKT-01M38WTK; leave AC1 pointing there.
- AC2: Playwright at 390 px scrolls to the bottom and asserts the last `.post`'s bottom edge is at or above the ReplyBar's top edge.
- AC3: Playwright taps reply on post N and asserts the sheet is open with chip `#N`. Quote with a selection asserts the textarea starts with `> [quote=#N]`.
- AC4: type text, close the sheet, and assert the ReplyBar shows the first line. Reload and assert the draft returns from the server.

### Design
docs/06 "Mobile" (Composing, Read tracking note on bottom padding); docs/09 risk table ("Mobile composer fights the on-screen keyboard"); docs/10 Q26; MobileThreadLight.dc.html and MobileThreadDark.dc.html; `generate.py` `mobile_thread`.

### Interactions
Depends on CEE79 (Composer, `composer` signal) and CEE4K (Shell). It uses CA9CE drafts. CGBXK checks the dialog focus trap. CGBY1 covers the reply flow at 390 px through this sheet.
Open in docs: the mockup shows no sheet or preview tab at phone size, so the sheet layout has no reference.

## Notes

**agent:claude/phase1-groom** at 2026-09-24T05:06:25Z

Acceptance criteria rewritten during Phase 1 grooming (2026-09-24), agreed with the user as part of preparing the autonomous run. Rewritten criteria move to the end of the list, so their numbers changed. The real-device keyboard check moved to TKT-01M38WTKSQ. What stays here is what an agent can verify: the emulated phone viewport and positioning driven by visualViewport.
