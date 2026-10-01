---
schema: 3
id: TKT-01M3V6Y4JP8W8WY0JG6NZV7M7S
title: Build the unread, new, participated and watching screens
type: task
status: in-progress
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
claim:
  actor: agent:claude-code/phase3-v6y4jp
  branch: run/v6y4jp-list-screens
  worktree: /home/sothr/workspace/git.local.sothr.com/terva-sh/ketju/.claude/worktrees/agent-a4de4e066da6b5199
  commit: 86752c422f9d6152ce015e2c1b52cdd721f33910
  session: null
  claimed_at: 2026-10-01T18:09:17Z
  expires_at: null
archive: null
created_at: 2026-10-01T07:47:49Z
updated_at: 2026-10-01T18:09:19Z
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

- [ ] /unread, /new, /participated and /watching render their lists from the API, each row with its forum, paging by Older, with an empty state each (CrossListScreen.test.tsx)
- [ ] Mark all read on /unread and in a forum's header clears the list and the badges without a reload, offered only to members (CrossListScreen.test.tsx, ThreadListScreen.test.tsx, unread-lists.spec.ts)
- [ ] The New tab and the desktop New link show /me's unread.new
- [ ] The screens work at 390 px and 1440 px, each test with one axe scan and no violations (unread-lists.spec.ts)
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
