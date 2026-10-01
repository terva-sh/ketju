---
schema: 3
id: TKT-01M29CGBZDVGWNDTKHJFHHSGY5
title: Add the page picker and lens sheets on phones
type: task
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area:client
  - area:mobile
  - effort:M
assignees: []
milestone: phase-3
parent: TKT-01M29BN367F3YQZESM63VN26GP
origin: null
dependencies:
  - TKT-01M29CEE6V358M5KCBJGHD58HM
  - TKT-01M29CTY71398QADNHHRP2HQVW
  - TKT-01M29CTY4JB7MMAWHVDT4BTCZM
  - TKT-01M29CTY6EE6T8BWR1DGSG3WGV
  - TKT-01M29CRJ1PY355KX3HX7A5N785
blocks_on: none
references:
  - ref: design:mobile-pages
    path: docs/06-thread-views.md
  - ref: pr:340
    path: null
claim: null
archive: null
created_at: 2026-09-11T23:23:08Z
updated_at: 2026-10-01T22:30:51Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase3-cgbzd
  name: ""
extensions: {}
---

## Description

A "Page 3 of 9" control with previous and next, where tapping the label opens a picker sheet with page numbers plus jump-to-unread and jump-to-date. Lenses open from a sheet as well.

The sheet is how a phone gets the lens switcher that sits in the desktop thread header, and it is what keeps "every lens is reachable at both sizes" true rather than aspirational.

## Acceptance criteria

- [x] A Lens button in the phone thread header opens a sheet listing exactly the lenses the desktop switcher offers this viewer (the moderator lens only to moderators)
- [x] Every choice is a link that updates the URL: a page keeps the lens, a lens follows the switcher's rule
- [x] Both sheets are useModal dialogs: focus moves in, Tab stays inside, Escape, the close button and the backdrop close them and focus returns to the opener
- [x] Sheet controls are at least 44 px and a bottom sheet pads by the safe-area inset (e2e, the targets.spec.ts pattern)
- [x] Tested at 390 and 1440 px with one axe scan per test; at 1440 px no sheet trigger is drawn
- [x] The committed dist is regenerated (go generate ./internal/web/spa) and npm run budget stays within 70000 B, with the before and after figures in the PR
- [x] On a phone the pager label is a button that opens a sheet reaching every page (a grid of all pages up to 60, past that a bounded grid plus a Go to page field), first unread when the reader has unread posts, and jump to date through the since lens

## Implementation plan

### Settled in grooming

- docs/06 "Mobile", "Pages": "Page 3 of 9" sits between previous and next, and tapping the label opens a picker sheet with page numbers, jump to unread and jump to date. Lenses open from a sheet as well. The phone mockups (`docs/mockups/MobileThread{Light,Dark}.dc.html`) draw the label as a 44 px button, and a "Lens" button beside "Only {author}" and "Watching" in the thread header.
- The sheets' contents and layout are decided by the run in docs/06 "Pages" (2026-10-01, pending maintainer review):
  - pages appear as numbered links in a grid, with the current one marked and scrolled into view. Up to 60 pages the grid holds every page. Past 60 it holds the first, the last, and the 20 on either side of the current one, and a "Go to page" number field (1 to `total_pages`) reaches the rest, so the sheet draws at most about 43 links however long the thread is (Terva review 1691);
  - "First unread" is offered on the thread page's existing terms;
  - "Jump to date" is a date field that opens the `since` lens;
  - the lens sheet lists exactly the lenses the desktop switcher offers this viewer.
- This ticket plans no lens. The lens framework and its URL grammar are TKT-01M29CRJ1P's. The switcher, its list of lenses per viewer, and returning to the full thread at the same ordinal are TKT-01M29CTY71's. The unread lens and "jump to first unread" are TKT-01M29CTY4J's. The `since` lens and its date grammar are TKT-01M29CTY6E's. The sheets draw what those export, and this ticket depends on all four.
- Every choice is a link (`pageHref` in `components/thread/Pager.tsx`, and the switcher's lens hrefs), so it updates the URL and is shareable. A page link keeps the lens; a lens link follows the switcher's rule for the page.
- Sheets are modal dialogs through `useModal` (`components/focus.ts`, #262), like `ForumPicker`. Focus goes to the current page's link or the first lens, Tab stays inside, and Escape or the close button ends the sheet and returns focus to the button that opened it.
- On the phone (below 720 px), every control in a sheet is at least 44 px (docs/06 "Targets and gestures"), and a sheet docked to the bottom pads by `env(safe-area-inset-bottom)` as the tab bar does (`styles/base.css`). Nothing is swipe-only: a sheet closes with its close button, Escape or a tap on the backdrop.
- At 720 px and above, the desktop pager row and the header switcher are used, and neither sheet is drawn. The sheets are the phone's arrangement of the same features (docs/06 "Mobile").
- Bundle: both sheets are part of the thread page, so they go in the reader bundle, not a lazy chunk. A chunk would be fetched on the first tap of the label and would still count (`scripts/bundle.mjs` counts every non-admin chunk). Estimate: 1.0 to 1.5 KB gzip for JS and CSS for both sheets, reusing `ForumPicker`'s backdrop and sheet styles where they fit.

### Approach

1. **`components/thread/PageSheet.tsx`.**
   - `Pager`'s `.pager-narrow` label becomes a `<button aria-haspopup="dialog" aria-expanded>` reading "Page N of M". Previous and next stay links.
   - The sheet is a `role="dialog"` with the heading "Go to page" and three parts:
     1. "First unread", when `ThreadScreen` passes its existing `firstUnread` handler, or the unread lens's jump once TKT-01M29CTY4J lands;
     2. a grid of `<a href={pageHref(thread, n, lens)}>`, with `aria-current="page"` on the current one. Up to 60 pages it holds 1 to `total_pages`. Past 60 it holds `sheetPages(current, total)`: 1, `total`, and `current - 20` to `current + 20` clamped, with a gap marker between runs. Above it, a "Go to page" form with `<input type="number" min=1 max=total>` routes to `pageHref` for the number entered;
     3. a `<form>` with `<input type="date">` and "Go", which routes to the `since` lens URL TKT-01M29CTY6E defines (for example `?lens=since:2026-09-01`), keeping the thread.
   - Choosing anything closes the sheet.
2. **`components/thread/LensSheet.tsx`.** A "Lens" button in the phone thread header, where the mockup puts it, opens a `role="dialog"` listing the switcher's entries from TKT-01M29CTY71's exported list (label, href, current). The current lens is marked. A "Full thread" entry, the lens header's return, is first when a lens is active.
3. **Pager and header wiring.** `ThreadBody` in `screens/ThreadScreen.tsx` passes the unread handler and lens to `Pager`, and the header renders `LensSheet` on the phone only. Use CSS at the 720 px breakpoint, as the pager already does, so the same components render at every size.
4. **Styles** go in `components/thread/thread.css`, under the phone query from `styles/base.css`. Use 44 px cells and the bottom inset padding.
5. Run `go generate ./internal/web/spa`, commit the dist, and quote `npm run budget` before and after.

### Tests

Vitest (`components/thread/PageSheet.test.tsx`, `LensSheet.test.tsx`):
- `the pager label is a button that opens the page sheet`, with `aria-haspopup` and `aria-expanded`.
- `the sheet lists every page and marks the current one`: 9 pages, links 1 to 9, and `aria-current` on 3.
- `a long thread's sheet is bounded`: 5000 pages at page 2500 draws at most 43 page links (1, 5000, and 2480 to 2520) plus the "Go to page" field. Entering 17 routes to page 17, and an entry outside 1 to `total_pages` is refused by the field. `sheetPages` is a table test that covers the edges: the current page near 1, near the end, and exactly 60 and 61 pages.
- `choosing a page keeps the lens in the URL`: the href is `pageHref(thread, n, lens)`.
- `first unread is offered only with unread posts`.
- `jump to date routes to the since lens`, using the URL form TKT-01M29CTY6E settles.
- `Escape and the close button return focus to the label` (`useModal`).
- `the lens sheet lists exactly the switcher's lenses for this viewer`: the moderator lens is present for a moderator and absent for a member, from the switcher's list, not a copy.
- `choosing a lens updates the URL`.

e2e (`e2e/phone-sheets.spec.ts`, stored session, one axe scan per test):
- `the page sheet reaches any page at 390 px`: open the sheet, choose page 2, and the URL has `page=2`. axe runs with the sheet open.
- `the lens sheet reaches every lens at 390 px`: open it, choose one, the URL has `lens=`, and the lens header shows.
- `sheet controls are at least 44 px`: the `targets.spec.ts` pattern (press where the target should be) over the sheet's links and buttons.
- `the desktop pager and switcher draw no sheet trigger at 1440 px`, with axe.

### Out of scope

- Every lens and its server side: TKT-01M29CRJ1P, TKT-01M29CTY4J, TKT-01M29CTY58, TKT-01M29CTY5T and TKT-01M29CTY6E.
- The desktop switcher and lens header: TKT-01M29CTY71.
- A timeline scrubber (docs/06 "Later enhancements").

## Notes

**agent:claude-code/phase3-groom-pages** at 2026-10-01T07:51:18Z

run decision: pending maintainer review. The page sheet lists every page as a 44 px numbered link in a grid, current marked and scrolled into view, first unread above it on the thread page's terms, and jump to date as a date field opening the since lens; the lens sheet opens from a Lens button in the phone thread header and lists exactly the switcher's lenses. Recorded in docs/06 Mobile, Pages (2026-10-01), with the alternatives.

**agent:claude-code/phase3-groom-pages** at 2026-10-01T08:18:59Z

run decision: pending maintainer review. Supersedes the earlier run-decision note on the grid: after Terva review 1691 (head 2455be0, base a599b48, finding 'Bound the number of page links rendered in the phone sheet'), the grid holds every page only up to 60 pages. Past that it holds the first, the last and current plus or minus 20, with a Go to page field. docs/06 Pages is updated.

**agent:claude-code/phase3-cgbzd** at 2026-10-01T19:00:04Z

run decision: pending maintainer review. In the page sheet, "First unread" is a button that runs the thread page's own First unread (keyboard/shortcuts.ts firstUnread, with readPosition at the press), not a link. A link would be computed when the sheet is drawn, and reading moves the position after that, so it would send a reader who had read on back to a post they have read (Terva review 1730). To a post already on screen it moves focus without changing the URL, as First unread in the thread actions does. Recorded in docs/06 Mobile, Pages (2026-10-01).

Implementation, prepared on run/cgbzd-phone-sheets (not pushed; the dist waits for the orchestrator):
- `components/thread/Sheet.tsx` is the shared sheet: a useModal dialog drawn with the forum picker's classes (forums.css) and docked to the foot of the screen, padded by `env(safe-area-inset-bottom)`. Focus returns to the opener when focus is in the sheet or on the body. A choice that moved focus elsewhere, such as First unread focusing a post on this page, keeps it there.
- `components/thread/PageSheet.tsx` holds the page sheet and `sheetPages`. Past 60 pages it marks each skipped run with "…" and does not fill a one-page gap, so it never draws more than 43 links. The sheet's title is "Pages", and the number field is labelled "Go to page".
- There is no LensSheet.tsx. LensBar draws a second Lens button, `[data-lens-sheet]`, which CSS shows below 720 px in place of `[data-lens-switch]`. It opens LensMenu itself inside a Sheet, so the phone lists exactly the switcher's lenses from the same code.
- The since form moved out of LensMenu as `SinceForm`, and the page sheet's "Jump to date" uses it.
- The vitest specs are in PageSheet.test.tsx and a "the lens sheet" block in LensBar.test.tsx. The e2e spec is e2e/phone-sheets.spec.ts. lenses.spec.ts now opens the switcher through the sheet on a phone.
- The reader bundle grew from 66189 B to 67245 B (+1056 B), against a budget of 120000 B. The AC's 70000 B figure is older than the maintainer's raise (#331).

## Summary

On a phone the pager's Page N of M is a button opening a page sheet (First unread through the thread page's own handler, every page up to 60 as 44 px links keeping the lens, past that the first, the last and 20 either side with a Go to page field, and a date field opening the since lens). Lens in the lens bar opens the switcher's own LensMenu in a sheet. Both are useModal sheets (components/thread/Sheet.tsx) docked to the foot and padded by the bottom safe-area inset; neither trigger is drawn from 720 px. SinceForm is shared by the switcher and the page sheet. Reader bundle 72754 B to 73762 B (+1008 B) of 120000 B. Run decision pending maintainer review: First unread in the sheet is the thread page's button, not a link (docs/06 Pages).
