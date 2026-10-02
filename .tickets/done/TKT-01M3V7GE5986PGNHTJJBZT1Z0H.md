---
schema: 3
id: TKT-01M3V7GE5986PGNHTJJBZT1Z0H
title: Add the / shortcut that opens search on desktop
type: task
status: done
status_reason: null
priority: low
due_on: null
labels:
  - area:client
  - area:search
  - effort:S
assignees: []
milestone: phase-4
parent: TKT-01M29BN367F3YQZESM63VN26GP
origin: null
dependencies:
  - TKT-01M3V9DWTNNAMF2WA9CWY0AJGQ
blocks_on: none
references:
  - ref: pr:360
    path: null
claim: null
archive: null
created_at: 2026-10-01T07:57:49Z
updated_at: 2026-10-02T21:39:09Z
created_by:
  id: agent:claude-code/phase3-groom-search
  name: ""
updated_by:
  id: agent:claude-code/phase4-run-7GE59
  name: ""
extensions: {}
---

## Description

The desktop mockup's search control shows a `/` key hint (`docs/mockups/FrontDark.dc.html`). The built top bar links to `/search` with no key. docs/06's shortcut list (`j`, `k`, `g`, `u`, `n`, `?`) does not include `/`, and `keyboard/shortcuts.ts` installs the shortcuts on the thread page only.

Filed in Phase 3 search grooming (TKT-01M3V3BVPY) and left out of TKT-01M3V70X0F (Build the search results screen). Before building it:
- decide whether `/` belongs in docs/06's list;
- decide whether a shortcut outside the thread page fits docs/06, given "each shortcut has a visible control" (the top bar's search link would be that control);
- check that `ignored()` keeps it out of the composer and inputs, and that the help overlay lists it.

Not a docs/09 Phase 3 deliverable, so no milestone.

## Acceptance criteria

- [x] On a desktop screen with the top bar, / goes to /search with its field focused while the shortcuts are on, and on /search it focuses the field and selects its text
- [x] / does nothing in a text field, the composer or a dialog, on a phone, or with the shortcuts off (shortcuts.test.tsx)
- [x] The shortcut list has a / row naming the top bar's Search, and docs/06's key list includes /
- [x] e2e/shortcuts.spec.ts covers / at 390 px and 1440 px, with one axe scan per test
- [x] The dist from go generate ./internal/web/spa is committed, npm run budget keeps the reader within 120000 B gzip, and the PR states the reader's growth

## Implementation plan

Groomed 2026-10-02 for the Phase 4 run (docs/15), against main at ed82f15.

### Settled in grooming

- **`/` belongs in docs/06's list, once the shortcuts can be turned off.** docs/06 "Paged, not infinite" (decided 2026-10-01, maintainer approved) left `/` out until the off switch existed. TKT-01M3V9DWTN (Let a member turn off the single-key shortcuts (WCAG 2.1.4)) adds that switch, so this ticket depends on it.
- **What `/` does, and where.** Decided by the run, pending maintainer review, in docs/06 "Paged, not infinite" (2026-10-02), with the alternatives that lost:
  - It works on every desktop screen that has the top bar, while the shortcuts are on (`shortcutsOn`, from TKT-01M3V9DWTN).
  - Away from `/search` it routes to `/search`. With no query, the field takes focus on arrival. `SearchScreen`'s `useLayoutEffect` on `asked.q`, and the route focuser's `data-autofocus` (`components/focus.ts`), already do that.
  - On `/search` it focuses the query field and selects its text, so typing replaces the search.
- **Its visible control** is the top bar's Search link (`shell/TopBar.tsx`). docs/06 asks that every shortcut have one, and docs/07 "Search" chose the link over a field. This ticket adds no field.
- **Not on a phone.** The phone layout has no shortcuts (docs/06 "Mobile").
- **Never in text.** `ignored()` keeps `/` out of inputs, textareas, the composer and dialogs, as it does every other key. `/` typed in the search field types a slash.
- **A signed-out reader gets it too.** `GET /search` serves anonymous readers (docs/07 "Search").

### Approach

1. **The listener.** `useGlobalShortcuts`, a hook in `keyboard/shortcuts.ts`, is mounted once by `shell/Shell.tsx` beside the top bar.
   - Its `keydown` listener goes on the document in a `useLayoutEffect` (the TKT-01M3TV4HBF rule).
   - It acts on `/` only, and only when all of these hold: the viewer is not on a phone, `shortcutsOn.value` is true, `ignored(e)` is false, and `helpOpen.value` is false.
   - It calls `preventDefault`, which stops Firefox's quick find.
2. **The action.**
   - On `/search`, it focuses `#search-q` (`SearchScreen.tsx`) and selects its text.
   - Anywhere else, it calls `route('/search')`.
   - A `g` in progress on a thread page already ends on any other key (`useThreadShortcuts`), so `/` after `g` ends `g` and then searches.
3. **The list.**
   - `shortcuts` gains a row: keys `['/']`, "Search", "Search, in the top bar".
   - The overlay's lead lines say where keys work. On a thread page, `/` works as well. On the forum index (from TKT-01M3V9DWTN), the lead names `?` and `/` as working there and the rest on a thread page.
   - The thread page's list shows every row as it does now.
4. **docs/06.** At implementation, the keyboard bullet's key list gains `/`, beside the decision line already there.
5. **Build.** Run `go generate ./internal/web/spa` and `npm run budget`.

**Reader bundle.** This is in the reader (`keyboard/`, `shell/`). Estimated at +200 B gzip. Measure it and state it in the PR.

### Tests

- **Vitest** (`keyboard/shortcuts.test.tsx`, or a new `shell/Shell.shortcuts.test.tsx`). Each press goes one microtask after mount, outside `act`, as the existing cases do.
  - "/ on the forum index routes to /search and the field has focus".
  - "/ on /search with a query focuses the field and selects its text".
  - "/ typed in the composer, in the search field or in a dialog is left alone".
  - "with the shortcuts off, / does nothing", for a member and for a signed-out reader.
  - "on a phone, / does nothing".
  - The help overlay test: the list has a `/` row naming the top bar's Search.
- **e2e** (`e2e/shortcuts.spec.ts`).
  - At 1440 px: from a thread page `/` lands on `/search` with the field focused. Typing a word and Enter searches. `/` again on the results selects the query.
  - At 390 px: `/` does nothing.
  - With carol's shortcuts off, `/` does nothing. Reset her `shortcuts` in `afterEach`, as the switch's spec does.
  - One axe scan per test. No `/auth/*` call, so no row in `e2e/session.ts`.

### Out of scope

- A search field in the top bar (docs/07 "Search" chose the link).
- Any other new shortcut.

## Notes

**agent:claude-code/phase4-groom-client-account** at 2026-10-02T17:21:26Z

run decision: pending maintainer review. / opens search on every desktop screen with the top bar while the shortcuts are on: it goes to /search, whose field takes focus, and on /search it focuses and selects the field. The top bar's Search link is its visible control. Recorded with the alternatives in docs/06 'Paged, not infinite' (2026-10-02). The questions the description asked (whether / belongs in the list, and whether a shortcut outside the thread page fits 'each shortcut has a visible control') are answered there.

**agent:claude-code/phase4-run-7GE59** at 2026-10-02T21:31:35Z

Implemented as planned. useGlobalShortcuts and openSearch live in keyboard/shortcuts.ts; shell/Shell.tsx mounts the hook once with preact-iso's route. The tests are in a new shell/Shell.shortcuts.test.tsx. Wording the plan left open: the index list's lead reads 'Here ? opens this list and / opens search. The other keys work on a thread page.' while the shortcuts are on, and 'When they are on, ? and / work here, and the other keys on a thread page.' while off (replacing TKT-01M3V9DWTN's 'These keys work on a thread page.', which / made untrue). The /settings hint now lists '/'. The '/' row's visible control is checked by the existing 'has one' table (.tools a.search). The 390 px inert e2e test gains its axe scan. Reader bundle measured at 79859 B -> 79988 B gzip (+129 B) in Phase A.

## Summary

/ opens search on every desktop screen with the top bar while the single-key shortcuts are on. useGlobalShortcuts (keyboard/shortcuts.ts), mounted once by shell/Shell.tsx, listens in a layout effect; away from /search it routes there and the field takes focus, and on /search it focuses the query field and selects its text. It does nothing on a phone, with the shortcuts off, with the shortcut list open, or in text, the composer or a dialog (ignored()). The list has a / row naming the top bar's Search, the index's lead line names ? and /, the /settings hint lists /, and docs/06's key list includes /. Reader bundle 79859 B -> 79988 B gzip. Every criterion met.
