---
schema: 3
id: TKT-01M3V6Y4JP8W8WY0JG6NZV7M7S
title: Build the unread, new, participated and watching screens
type: task
status: done
status_reason: null
priority: high
due_on: null
labels:
  - area:client
  - area:views
  - effort:M
assignees: []
milestone: phase-3
parent: TKT-01M29BN36D34YVKVRPNQD3PHDV
origin: null
dependencies:
  - TKT-01M29CTY7K2YTY0V54YKJTKX1W
blocks_on: none
references:
  - ref: pr:294
    path: null
claim: null
archive: null
created_at: 2026-10-01T07:47:49Z
updated_at: 2026-10-01T21:28:27Z
created_by:
  id: agent:claude-code/phase3-groom-unread
  name: ""
updated_by:
  id: agent:claude-code/phase3-v6y4jp
  name: ""
extensions: {}
---

## Description

The four "what's new" screens in place of their Phase 1 placeholders: `/unread`, `/new`, `/participated` and `/watching` (docs/06 "Thread list views", docs/06 "Mobile"). It also adds the New tab's badge, and the "Mark all read" control on `/unread` and in a forum's header. The forum header's control is the F3 divergence from TKT-01M39WBN4Q.

The server half is TKT-01M29CTY7K (Build the cross-forum unread, new, participated, and watching lists). It builds the four `GET` routes, the two mark-read routes and `/me`'s `new` count. This ticket was split out of it during Phase 3 grooming because the built client is committed and client PRs queue one at a time (docs/13 "Running two lanes, changed"). A server PR that also carried the dist would wait in that queue and run into the review's context limit.

## Acceptance criteria

- [x] /unread, /new, /participated and /watching render their lists from the API, each row with its forum, paging by Older, with an empty state each (CrossListScreen.test.tsx)
- [x] Mark all read on /unread and in a forum's header clears the list and the badges without a reload, offered only to members (CrossListScreen.test.tsx, ThreadListScreen.test.tsx, unread-lists.spec.ts)
- [x] The New tab and the desktop New link show /me's unread.new
- [x] The screens work at 390 px and 1440 px, each test with one axe scan and no violations (unread-lists.spec.ts)
- [ ] The committed dist is rebuilt with go generate ./internal/web/spa and npm run budget stays within 70000 B, with the reader's growth, measured eager and lazy, recorded in the PR

## Implementation plan

### Settled in grooming

- **What each list holds, and that a muted thread is in none,** is docs/06 "Thread list views" (run decision 2026-10-01). The API, `/me`'s `unread.new`, and the mark-read routes `PUT /api/v1/me/read` and `PUT /api/v1/forums/{slug}/read` are TKT-01M29CTY7K's. This ticket depends on it.
- **Badges.** The phone's Unread and New tabs carry the same counts as the desktop nav (docs/06 "Mobile"). New has had no badge because `/me` had no count (`state/viewer.ts`). It now does. Participated and Watching keep no badge, as in the mockup's `nav()`.
- **The forum header's "Mark all read"** is the F3 divergence recorded on TKT-01M39WBN4Q and left to TKT-01M29CTY7K. It is here because it is client work.
- **Rows.** Rows are the thread list's own (`components/ThreadRow.tsx`), plus the row's `forum`. Times are absolute (`PostTime`, docs/06 decided 2026-10-01).

### Approach

1. **DTOs.** `api/dto.ts` gains `ThreadRow.forum` (`ForumRef | null`), `UnreadCounts.new` and `MarkedRead`. `dto-contract.ts` gains their rows, and `bootstrap.ts`'s anonymous default gains `new: 0`.
2. **Screen.** `screens/CrossListScreen.tsx` is one component configured per list: path, API endpoint, title, and empty-state text.
   - It fetches through `api/resource.ts` and pages with "Older" on `next_cursor`, as `ThreadListScreen` does.
   - It renders `ThreadRow`, which gains an optional forum link (`/f/{slug}`) drawn when `forum` is set.
   - It records itself with `rememberList`. `state/lists.ts` accepts a list key beside a forum, so marking a thread unread returns to `/unread` when the reader came from there.
   - The four placeholders in `routes.tsx` are replaced. Screen names stay `unread`, `new`, `participated` and `watching`.
   - The empty states:
     - Unread: "Nothing unread. Threads with posts you have not read appear here."
     - New: "No new threads. Threads you have not opened appear here."
     - Participated: "You have not posted yet. Threads you post in appear here."
     - Watching: "You are not watching any threads. Choose Watch in a thread's menu to follow it here."
3. **Mark all read.**
   - `/unread`'s header has "Mark all read", which calls `PUT /api/v1/me/read`.
   - `ThreadListScreen`'s header has "Mark all read" for that forum, which calls `PUT /api/v1/forums/{slug}/read`.
   - Both are offered only to a member. Each refetches its list and calls `refreshViewer()`, so the change shows without a reload.
4. **Nav.** In `shell/nav.ts`, the New tab and the desktop New link take `badge: badges.value.new`. `state/viewer.ts`'s `badges` gains `new: viewer.value.unread.new`.
5. **Bundle.** The screens go in the reader bundle as eager imports. The estimate is about 1.4 KB gzip of JS, mostly reuse of `ThreadRow` and the list's paging, and 0.2 KB of CSS for the forum link. `scripts/bundle.mjs` counts lazy non-admin chunks too, so a lazy chunk would not lower the total. Measure both ways and record the reader's growth in the PR.
6. **Build.** Run `go generate ./internal/web/spa`, and queue as the one open client PR.

### Tests

- `CrossListScreen.test.tsx` (Vitest, `renderAt` for each path):
  - rows with their forum link;
  - "Older" appending;
  - each empty state;
  - "Mark all read" calling the route, refetching and refreshing `/me`;
  - signed out: the screen asks the reader to sign in, and no mark is offered.
- `ThreadListScreen.test.tsx`: the forum header's "Mark all read" calls the forum route, is offered only to a member, and refetches.
- `nav` and `Shell` tests: the New badge in the tab bar and the top bar shows `/me`'s `unread.new`.
- `e2e/unread-lists.spec.ts` at 390 and 1440 px, from alice's stored session, with no `/auth/*` spend:
  1. A seeded unread thread is on `/unread` with its forum, and the New badge shows.
  2. "Mark all read" empties `/unread` and clears the Unread badge.
  3. A forum's "Mark all read" clears that forum's rows only.

  Each test has one axe scan. Each restores the read state other specs rely on (`setReadPosition`), and prints its error context on failure (#261).
- `go generate ./internal/web/spa` leaves no diff, and `npm run budget` passes.

### Out of scope

- The API, its access rules and its badges' counts: TKT-01M29CTY7K.
- The watch and mute control that fills `/watching`: TKT-01M3V6Y87D.
- Live list updates: TKT-01M29CTYA.

## Notes

**agent:claude-code/phase3-groom-unread** at 2026-10-01T10:55:40Z

Requirement carried from review 1743 (2026-10-01; PR #285, head fb1c38d, base ccb234d). Finding (medium): 'Restore the site-wide mark in the unread-list E2E test'. users.mark_all_read_at only moves forward, so setReadPosition cannot undo a site-level Mark all read on alice's shared stored session, and other specs would see changed unread state. What the implementation must do: run the site-level 'Mark all read' test as a member whose stored session no other spec reads its unread state from, or reset that member's mark in an e2e fixture. Adding a stored session means a row in e2e/session.ts and paceAuth for its sign-in. Seed separate unread activity for the forum-level check, such as a fresh post in that forum made through another stored session, so it has something to clear and does not depend on the site-level test's order.

**agent:claude-code/phase3-v6y4jp** at 2026-10-01T18:42:45Z

run decision: pending maintainer review (2026-10-01). Recorded in docs/06 "Thread list views" under "Decided by the run (2026-10-01, pending maintainer review; TKT-01M3V6Y4JP)".

- Where "Mark all read" is offered. The site-level mark (PUT /me/read) is a button on /unread, shown while the list has rows, and in the forum index's header beside New thread from 720 px up, as the desktop mockup draws it. The forum-level mark (PUT /forums/{slug}/read) is a button in a forum's header beside New thread, at every width. Each is offered to a member only, with no confirmation.
- A signed-out reader on any of the four lists is asked to sign in, and no request is sent.
- A row names its forum as a link, first in the meta line.

Why the front page too: divergence F3 on TKT-01M39WBN4Q is the front page's button (Main.dc.html and FrontDark.dc.html draw "Mark all read" beside New thread on the forum index). The settled item calls the forum header's control "the F3 divergence", but F3 names the front page. Building only the forum header would have left F3 open, so both are built, and the settled item (the forum header's control) stands as written.

Alternatives that lost: only /unread and the forum page (leaves F3 open); the front-page button on phones (the phone mockup draws none, and /unread is one tab away); a confirmation before the irreversible site mark (the mockup draws a plain button, and a mistaken mark loses unread markers, not posts); the forum as a chip before the title (titles would no longer start at one edge).

**agent:claude-code/phase3-v6y4jp** at 2026-10-01T18:42:47Z

Implementation notes (prepared on run/v6y4jp-list-screens; dist not yet committed, waiting for the orchestrator's turn).

- DTOs. `ThreadRow.forum` and `UnreadCounts.new` are required in dto.ts, as the server lane asked. No hand-built fixture needed a change. bootstrap.ts's anonymous default gains `new: 0`.
- Screen. `screens/CrossListScreen.tsx` serves all four routes, configured per list. It reuses `ThreadRow` and the forum list's paging. The cursor is carried in the address, with "Older threads" and "Newest" links, so a page can be linked and gone back to with its scroll position kept. The plan's test list said "Older appending". The forum list does not append, and the lane prompt asked to reuse its paging, so the tests check the cursor link and the next page's request instead.
- Mark all read is `components/MarkAllRead.tsx`, used on /unread, in the forum header and in the forum index header. On success it drops every held list (`state/lists.ts forgetLists`) and every held thread page and the thread cache (`state/threads.ts forgetAllThreadPages`), since the read boundary, divider and landing page have moved. It then refetches its own list and calls `refreshViewer()` without waiting. A failure keeps the list and shows an alert.
- Mark unread on the thread page now drops the cross-forum lists too (`forgetLists`), so a reader sent back to /unread does not see the held list first.
- `state/lists.ts`: `rememberList(null, href)` records a cross-forum list, and `listFor` returns it for a thread in any forum.
- Nav: New carries `badges.new` at both widths. Each badge's screen-reader words now come from its nav item ("new threads" for New; the desktop top bar used to say "unread threads" for every link).
- e2e (review 1743's requirement): the site-wide mark runs as `nell`, a member seeded for it in cmd/ketju e2e.go (after ada, so the earlier ids keep their values). She signs in inside the spec with `paceAuth(2)`, and has a row in e2e/session.ts's budget table. No other spec signs her in. Each test first has carol start a fresh thread in site-feedback, so there is something to clear whatever ran before. The forum-level mark runs as alice in site-feedback, where only live.spec.ts reads her, and only in threads it starts itself. It sets Minutes back to the seeded #30 with setReadPosition, as thread-list.spec.ts also does, and checks that the mark left it unread.
- targets.spec.ts gains /participated at 390 px (alice always has rows there), which measures the forum link's hit area beside the title.
- Bundle. The reader was 66189 B on main. With the screens eager it is 67146 B (+957 B). With CrossListScreen lazy it is 67629 B (+1440 B), because lazy non-admin chunks count and the chunk adds its own overhead. Eager stays.

**agent:claude-code/phase3-v6y4jp** at 2026-10-01T21:28:09Z

Criterion 5 left unticked. The dist is rebuilt with `go generate ./internal/web/spa` and committed, and the reader's growth is recorded, measured both ways. But the criterion names a 70000 B budget, and the reader is now 72670 B gzip. The maintainer raised the budget to 120000 B on 2026-10-01 (#331, docs/14 class 5). Main was already 71808 B before this change, so no client change could meet the number as written. `npm run budget` passes against the current 120000 B.

Growth on main d2b4f208: 71808 B to 72670 B (+862 B), with the four screens eager. On the earlier base 86752c42 it was 66189 B to 67146 B eager (+957 B), against 67629 B with CrossListScreen lazy (+1440 B). Lazy non-admin chunks count toward the budget, and the split adds chunk overhead, so eager stays.

## Summary

/unread, /new, /participated and /watching are built (screens/CrossListScreen.tsx). They replace the Phase 1 placeholders. Each list renders from its API route with the forum list's ThreadRow, names each row's forum as a link, pages by the server's cursor in the address, and has its own empty state. A signed-out reader is asked to sign in, and no request is sent.

Mark all read (components/MarkAllRead.tsx) is offered to members only:
- on /unread, for the whole site;
- in a forum's header, for that forum;
- in the forum index's header from 720 px up, for the whole site (run decision, mockup divergence F3).

After a mark the client drops every held list and thread page, refetches the list and refreshes /me, so the list and the badges change without a reload. The New tab and the desktop New link show /me's unread.new. dto.ts makes ThreadRow.forum and UnreadCounts.new required.

The e2e site-wide mark runs as nell, a member seeded for it who signs in with paceAuth(2) and whom no other spec reads (review 1743).

Reader bundle: 71808 B to 72670 B (+862 B) of the 120000 B budget. Criterion 5 is left unticked because it names the old 70000 B budget; see the note.
