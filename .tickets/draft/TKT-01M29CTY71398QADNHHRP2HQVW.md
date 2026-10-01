---
schema: 3
id: TKT-01M29CTY71398QADNHHRP2HQVW
title: Build the lens header and switcher in the client
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:client
  - area:views
  - effort:L
assignees: []
milestone: phase-3
parent: TKT-01M29BN36D34YVKVRPNQD3PHDV
origin: null
dependencies:
  - TKT-01M29CRJ1PY355KX3HX7A5N785
  - TKT-01M29CEE6V358M5KCBJGHD58HM
  - TKT-01M29CTY4JB7MMAWHVDT4BTCZM
  - TKT-01M29CTY58PZ9W4DJ8Y0NEECE0
  - TKT-01M29CTY5TZ1QMWK6BTGA21BWQ
  - TKT-01M29CTY6EE6T8BWR1DGSG3WGV
blocks_on: none
references:
  - ref: design:lenses
    path: docs/06-thread-views.md
  - ref: ticket:TKT-01M29CGBZD
    path: null
claim: null
archive: null
created_at: 2026-09-11T23:28:54Z
updated_at: 2026-10-01T10:55:02Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase3-groom-lenses
  name: ""
extensions: {}
---

## Description

The header every lens shows: what is being filtered, how many posts are hidden, and a one-click return to the full thread at the same ordinal.

Returning at the same ordinal is the part that stops people getting lost. A reader who filters to one author, finds the post they wanted, and clicks back should be looking at that post in the full thread rather than at page 1.

## Acceptance criteria

- [ ] The lens header shows the active lens, the shown count and the hidden count, and a '… N posts …' divider stands at every gap the server reports
- [ ] Only <handle> is one press from the thread head at both widths and gives the author lens URL, ?lens=author:<handle>
- [ ] A bare ?lens=unread is replaced by unread:<B>, page 2 is unchanged after page 1 is read, and first unread lands on last_read + 1 from the canonical view and from the lens (e2e)
- [ ] The read beacon runs under unread and since and under no other lens (vitest)
- [ ] At 390 px every lens is reachable from the Lens control, which TKT-01M29CGBZD later presents as a sheet, and the header and bar are legible with 44 px targets
- [ ] e2e at 390 and 1440 px pass with one axe scan per test and no violations
- [ ] The committed dist is rebuilt with go generate ./internal/web/spa and the reader stays within 70000 B gzip; the PR states the reader's bytes before and after, and the switcher's bytes measured inside and outside a lazy chunk
- [ ] Every switch (switcher item, Only <handle>, Conversation) sets the address's lens and drops its page and ordinal, and a fresh load of that address shows the same posts (e2e)
- [ ] Empty states: an anonymous unread or mentions lens asks the reader to sign in; a fully read thread and an empty author or since lens offer the full thread at /t/{id}/{slug}; a one-post chain says the post is neither a reply nor replied to (vitest)
- [ ] The since date field writes since:YYYY-MM-DD as chosen, read as midnight UTC, whatever the viewer's timezone (vitest with two non-UTC timezones)
- [ ] Full thread's href is the canonical permalink of the post at the top of the viewport, kept current as the reader scrolls, so a press, a new tab and a copied link all land there (vitest and e2e)

## Implementation plan

Planned 2026-10-01 against main at a599b48 (Phase 3 grooming, lane `lenses`).

### Settled in grooming

- Every lens header shows what is being filtered, how many posts are hidden, and a one-press return to the full thread at the same ordinal (docs/06 "Lenses"). Lenses and pages are URL state, never client-only state (docs/07 "Path to a non-JS client"). `web/client/src/view.ts` (`useThreadView`, `setLens`) already holds the lens in the query string, and `Pager` already carries it on every page link.
- This ticket is the one client ticket for lenses. The four lens tickets are server-only, so the committed dist changes in one PR (docs/13 "Running two lanes, changed").
- The lens bar sits under the thread head at both widths, as the mockups draw it (`docs/mockups/generate.py`, "Only mara" and "Lens" beside the pager on desktop, a row of 44 px buttons on the phone). It holds "Only <handle>" and "Lens", which opens the switcher. The switcher's items, the post's "Conversation" entry, dividers for every lens, and "Full thread" going to the post at the top of the viewport are all in docs/06, under "Decided by the run (2026-10-01): the lens bar". "Only OP" on the header is docs/10 Q15, decided by the run 2026-10-01.
- The unread lens's boundary is held by replacing a bare `?lens=unread` with `?lens=unread:B` (docs/06, under "the unread lens holds its boundary").
- The read beacon runs under unread and since only (docs/06, under "read tracking under a lens").
- An unknown lens is served as the canonical page with `lens: null` (docs/07, under "the `lens` parameter").
- Times are absolute, through `PostTime` (docs/06, decided 2026-10-01).
- The switcher is a disclosure of links, as `PostMore` is, not a modal, so it does not use `useModal`. Focus and key listeners go in layout effects, following TKT-01M3TV4HBF.
- On phones TKT-01M29CGBZD later presents the switcher as a sheet. Until then this ticket's switcher serves both widths.

### Approach

1. **DTO.** In `web/client/src/api/dto.ts`, `Lens` gains `gaps: {after: number; count: number}[]` and `ThreadHeader` gains `author: Author`, matching TKT-01M29CRJ1P and TKT-01M29CTY58.
2. **`components/thread/lens.ts`** holds the pure helpers:
   - `lensParam(kind, value)` builds the string: `kind` alone when `value` is empty, as for `mentions`, `mod` and an anonymous bare `unread`, and `kind:value` otherwise (docs/07). `onlyHref(thread, handle)` builds the Only link.
   - `gapBefore(posts, gaps)` places each gap before the first post whose ordinal is past `after`, and the trailing gap after the last post.
   - `describe(lens, header)` gives the header text: "Showing 7 of 212 posts by @mara", "Unread since #60", "Conversation around #42", "Since <PostTime>", "Mentioning you", "Removed posts".
   - `tracksUnderLens(kind)` is true for `''`, `unread` and `since`.
3. **`components/thread/LensHeader.tsx`** is drawn above the posts when `page.lens` is set. It shows the description, the shown and hidden counts, and "Full thread".
   - "Full thread" is an anchor whose `href` is always the permalink of the post at the top of the viewport, the first `.post` whose bottom is below the sticky head. It starts as the page's first post, and a passive scroll listener throttled to one `requestAnimationFrame` updates it as the reader scrolls. A press, opening it in a new tab and copying it all go to the same post, and `useArrival` scrolls to it.
   - On a lens page with no posts, "Full thread" and every empty state's link go to the canonical thread address, `/t/{id}/{slug}` with no page, where the server's landing rule places the reader.
   - The empty states:
     - an anonymous viewer on unread or mentions: "Sign in to see what is new here", with a link to `/auth/signin?return_to=`;
     - a fully read thread: "Nothing unread. Read the full thread";
     - an author lens or since with no posts: "No posts here. Read the full thread";
     - a chain of one post: "#N is not a reply and has no replies".
4. **`components/thread/GapDivider.tsx`** draws "… N posts …" wherever `gapBefore` says. Its count links to the canonical permalink of the first skipped post.
5. **`components/thread/LensBar.tsx`** is drawn under the thread head at both widths.
   - "Only <handle>" links to `?lens=author:<header.author.handle>`, with `aria-current` while that lens is on.
   - "Lens" opens the switcher. Its items are links that change only the `lens` and drop `page` and the ordinal:
     - Full thread;
     - Unread, for a signed-in viewer;
     - one "By @handle" for each distinct author on the page;
     - Mentioning me, for a signed-in viewer;
     - Since a date, a date field whose chosen day goes into the address as `since:YYYY-MM-DD`, which the server reads as midnight UTC (docs/07). The client never converts it to a local moment, so the field and a shared date link select the same posts;
     - Removed posts (`mod`), only when `viewer.can_moderate`.
   - Escape and a press outside close it, returning focus to "Lens".
   - Targets are 44 px below 720 px.
6. **Conversation.** `Post` gains a "Conversation" link to `?lens=chain:<ordinal>` when the post has `reply_to` or `reply_count > 0`. On desktop it is inline in the footer. On a phone it is in `PostMore`. It is not on `Tombstone` and not in the reply expander's copies.
7. **`ThreadScreen.tsx`.**
   - When the address holds a bare `unread` and the answer's `lens` is `unread` with a value, route with replace to `unread:<value>`, keeping `page` and the hash.
   - When the address holds a lens and the answer's `lens` is null, route with replace to the address without it, so the address stays the view.
   - Under a lens, page links use `lensParam(page.lens.kind, page.lens.value)`.
   - `useReadTracking` runs only when `tracksUnderLens(kind)`.
   - `firstUnread` under the unread lens focuses the lens's first post on page 1, or routes to page 1 of `unread:B`. Anywhere else it keeps the canonical permalink of `last_read + 1`.
   - `Pager`'s "posts x–y of n" stays hidden under a lens, as now, and its count comes from `page.total_pages`.
8. **Bundle.**
   - The header, bar, divider, empty states, the Conversation link and the glue belong in the reader, since a lens URL can be the first page loaded. Estimated at about 1.4 KB gzip.
   - The switcher's menu, with its date field and author list, is estimated at about 0.9 KB gzip. It is measured both ways and becomes a lazy chunk (`components/thread/LensMenu.tsx`, loaded on the first press of "Lens") if that saves at least 400 B from the reader. If it does, `testing.tsx` gains a `lazyChunks` row for `/^\/t\//`.
   - Expected reader: about 61.9 to 62.8 KB of 70000 B. If the reader would pass 70000 B, stop: a budget raise is the maintainer's (docs/14, class 5).
   - Rebuild the committed dist with `go generate ./internal/web/spa`.

### Tests

- Vitest:
  - `lens.test.ts`: `lensParam` gives bare `mentions` and `mod` and `since:<ms>`; gap placement across pages, the description per kind, and `tracksUnderLens`.
  - `LensHeader.test.tsx`: counts; each empty state; "Full thread"'s `href` follows the post at the top of the viewport after a scroll; on an empty lens page it and every empty state's link go to `/t/{id}/{slug}`.
  - `LensBar.test.tsx`: the date field writes `since:YYYY-MM-DD` unchanged with the viewer's timezone set to `America/Los_Angeles` and to `Pacific/Auckland`; the Only href equals the author lens URL; `aria-current`; Moderator offered only with `can_moderate`; Unread and Mentioning me only when signed in; Escape returns focus.
  - `ThreadScreen.lens.test.tsx`, through `renderAt`:
    - a bare unread is replaced by `unread:B`;
    - an unknown lens is dropped from the address;
    - `trackPosts` is not called under author, chain, mentions or mod, and is called under unread and since;
    - dividers are drawn from `gaps`;
    - Conversation is on a reply and on a replied-to post, and absent otherwise.
  - The fixtures are a lens golden from `internal/dto/testdata/golden`.
- e2e `e2e/lenses.spec.ts`, with stored sessions, one axe scan per test, each test at 390 and 1440 px. It calls no `/auth/*`, so it needs no `paceAuth`.
  - Only <handle> from the thread head gives `?lens=author:<op>`, "Showing N of M", and the same posts on a fresh load.
  - Conversation from a reply gives the chain with dividers, and "Full thread" lands on the post that was at the top.
  - The unread lens holds while reading: page 2 is unchanged after page 1 is read. First unread lands on `last_read + 1` from the canonical view and from the lens.
  - The switcher reaches every lens at 390 px with 44 px targets. A moderator sees Removed posts, and a member does not.
- `npm run typecheck`, `npm test`, `npm run build` (budget), and the Playwright suite as `ci.yml` runs them.

### Out of scope

- The phone lens sheet and page picker: `TKT-01M29CGBZD`. It should depend on this ticket.
- Server behaviour of each lens: `TKT-01M29CRJ1P`, `TKT-01M29CTY4J`, `TKT-01M29CTY58`, `TKT-01M29CTY5T`, `TKT-01M29CTY6E`.
- Moving the existing menus to layout effects: `TKT-01M3TV4HBF`.
- The cross-forum unread surfaces: `TKT-01M29CTY7K`.

## Notes

**agent:claude-code/phase3-groom-lenses** at 2026-10-01T07:51:20Z

run decision: pending maintainer review. The decisions are recorded in docs/06 'Lenses', under the lens bar and read tracking under a lens. (1) The lens bar sits under the thread head at both widths: Only <handle> and Lens, as in the mockups. (2) The switcher items are recorded there. (3) Conversation is the chain entry on any reply or replied-to post. (4) Every lens draws gap dividers. (5) Full thread goes to the post at the top of the viewport. (6) The read beacon runs under unread and since only. This ticket also absorbs the client parts of the four lens tickets: the Only button, the unread address replacement and first unread under the lens, the empty states, the dividers, and the since date entry. The dist then changes in one client PR, so effort rises from M to L. TKT-01M29CGBZD (phone sheets, another lane) should depend on this ticket; it is referenced rather than linked so as not to write that lane's file. Decided in the Phase 3 grooming of 2026-10-01, lane lenses.

**agent:claude-code/phase3-groom-lenses** at 2026-10-01T08:13:55Z

Review 1692 (head 4065cf7, base a599b48): two medium findings were fixed in the plan. (1) The date field now writes since:YYYY-MM-DD as chosen, which the server reads as midnight UTC, instead of a local-midnight Unix ms; it is tested under two non-UTC timezones. (2) On an empty lens page, Full thread and the empty states go to /t/{id}/{slug}. docs/06 says both.

**agent:claude-code/phase3-groom-lenses** at 2026-10-01T09:22:55Z

Review 1714 (head 656ce4e, base a599b48), medium, 'Make the Full thread anchor target the visible post': fixed. The anchor's href follows the post at the top of the viewport, updated by a passive scroll listener throttled to one requestAnimationFrame, so a new tab or a copied link lands there too (docs/06, criterion 11).

**agent:claude-code/phase3-groom-lenses** at 2026-10-01T10:06:10Z

Requirement carried from review 1730 (2026-10-01; PR #277, head 297cc75, base f3538e6). This was the fifth and last grooming round, so the finding is carried here rather than fixed in the plan, and it binds the implementing lane.

Finding (medium), "Do not advance read position across the since lens's leading gap":
- A since lens can begin well after the viewer's last-read ordinal.
- Under the planned rule, the read beacon runs under since and sends the highest post seen.
- A reader at #10 who reads a since page starting at #100 would therefore have #11 to #99 marked read without seeing them.
- The docs/06 decision "read tracking under a lens" says the beacon runs under since because its posts run on without a gap. That holds only after the gap before the first post.

What the implementation must do:
- Run the read beacon under the canonical view and the unread lens only. Exclude since, along with author, chain, mentions and mod.
- `tracksUnderLens(kind)` is true for `''` and `unread` alone.
- The vitest criterion "The read beacon runs under unread and since and under no other lens" is read as "under unread and under no other lens". Its test asserts `trackPosts` is not called under since.
- In the same PR, amend docs/06 "Decided by the run (2026-10-01, pending maintainer review): read tracking under a lens" to say the beacon runs under unread only. Give the leading-gap reason, and add the since alternative to the losing options.
- The unread lens is not affected. It starts at B + 1, with B the position when it opened, and the position only moves forward, so it has no leading gap.

**agent:claude-code/phase3-groom-lenses** at 2026-10-01T10:06:12Z

Requirement carried from review 1730 (2026-10-01; PR #277, head 297cc75, base f3538e6). This was the fifth and last grooming round, so the finding is carried here rather than fixed in the plan, and it binds the implementing lane.

Finding (medium), "Keep First unread tied to the current read position":
- The plan's step 7 says First unread under a held `unread:B` lens focuses the lens's first post on page 1, B + 1.
- The read beacon runs under the unread lens, so once the reader has moved on, B + 1 is already read, and the action jumps backwards.
- That contradicts the criterion that First unread lands on `last_read + 1`.

What the implementation must do:
- First unread under the unread lens targets the current position plus one: the reader's `last_read_ordinal` as the read tracker now knows it (`state/readTracker.ts`), not B and not the page's stale `viewer.last_read_ordinal`.
- When that post is on the lens page on screen, focus it.
- Otherwise, route to `?lens=unread:B&ordinal=<last_read + 1>`, which serves the lens page holding it (TKT-01M29CRJ1P `PageOf`). `useArrival` then scrolls to it.
- When nothing is unread, show the existing "Nothing unread in this thread." hint.
- Test it in vitest by advancing the tracker past B + 1 under `unread:B` and pressing First unread (and `u`). The target must be the current `last_read + 1` on a later lens page, never B + 1.
- The e2e check in criterion 3 covers it from the lens after reading page 1.

**agent:claude-code/phase3-groom-lenses** at 2026-10-01T10:55:02Z

Requirement carried from review 1741 (2026-10-01; PR #283, head e462522, base 3890b38). The grooming PR is past its five-round cap, so the finding is carried here rather than fixed in the plan, and it binds the implementing lane. It refines the earlier carried requirement from review 1730, which limits the read beacon to the canonical view and the unread lens.

Finding (medium), "Do not track reads from an arbitrary unread:B boundary":
- `unread:B` serves the posts after B to whoever opens it (TKT-01M29CTY4J), so a shared or typed link need not start at the reader's own position.
- A reader at #10 who opens `unread:99` would have #11 to #98 marked read when the beacon sends #100 or later.

What the implementation must do:
- Under the unread lens, run the read beacon only when B is at or below the reader's current read position: B ≤ `last_read_ordinal` as the read tracker knows it now (`state/readTracker.ts`), or as the page's `viewer.last_read_ordinal` says when the tracker knows nothing newer. The lens then starts at or before the reader's first unread post and has no leading gap for them.
- When B is above the reader's position, leave the beacon off for that view, as under the filtering lenses. A lens opened from the reader's own bare `?lens=unread` always has B equal to their position, so their normal path is unaffected.
- Test it in vitest:
  - `unread:B` with B above the reader's position sends no `PUT /read`;
  - B equal to the position tracks;
  - B below the position tracks, which only moves the position forward over posts the reader has already read or is now shown.
- In the same PR, amend docs/06 "read tracking under a lens" to state this condition, alongside the since change carried from review 1730.
