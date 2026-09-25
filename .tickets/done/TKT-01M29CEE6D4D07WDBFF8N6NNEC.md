---
schema: 3
id: TKT-01M29CEE6D4D07WDBFF8N6NNEC
title: Build the thread list screen
type: task
status: done
status_reason: null
priority: high
due_on: null
labels:
  - area:client
  - effort:M
assignees: []
milestone: phase-1
parent: TKT-01M29BN367F3YQZESM63VN26GP
origin: null
dependencies:
  - TKT-01M29CCC02K8F14P0SKAYFMSE6
  - TKT-01M29CA9AGD2JEG8KMT1TJB9GD
blocks_on: none
references:
  - ref: design:thread-lists
    path: docs/06-thread-views.md
  - ref: pr:87
    path: null
claim: null
archive: null
created_at: 2026-09-11T23:22:04Z
updated_at: 2026-09-25T04:22:16Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase1-run-415af6aa
  name: ""
extensions: {}
---

## Description

A forum's threads: pinned first, then by last post, each row with title, tags, author, reply count, last poster, last post time, and an unread badge.

The badge is a control, not an ornament. Clicking it jumps to the first unread post; clicking the title goes to page 1. That distinction is small to build and it is most of what makes a forum feel fast to a returning reader.

## Acceptance criteria

- [x] Clicking the unread badge lands on the first unread post and clicking the title lands on page 1
- [x] Pinned threads are visually distinct and sort first
- [x] Rows stay readable at 390 px with a long title and several tags
- [x] Paging through the list preserves scroll position on back navigation

## Implementation plan

Revised at claim (2026-09-25), after reading the code. `GET /forums/{slug}/threads` (TKT-01M29CA9AG) and the forum index (TKT-01M29CEE5Y) are merged.

### Approach
1. **Screen.** `screens/ThreadListScreen.tsx` at `/f/:forum?cursor=&sort=` loads `useResource('/api/v1/forums/{slug}/threads?…')` and renders `ThreadList.threads` in the server's order, with pins first. The client does not re-sort.
   - The header has the forum's name, and "New thread" to `/f/{slug}/new` for a signed-in reader.
   - The phone top bar gets the forum's name and a back link to Forums.
   - A failure shows `ErrorPanel`, and not-found shows `NotFound`.
2. **Rows.** `components/ThreadRow.tsx` is reusable by the cross-forum lists (TKT-01M29CTY7K). Each row has:
   - a pin icon with a visible "Pinned" label and a `.row-pinned` background, so pins are not marked by colour alone;
   - a lock icon with a hidden "Locked";
   - the title, tag chips, author, reply count, last poster and last post time (`PostTime`, as on the index).
3. **Links (AC1).** The title and the badge are sibling links, never nested.
   - The title links to `/t/{id}/{slug}?page=1`, so the reader lands on page 1, not at their last read.
   - The badge ("N new", signed-in only) links to `/t/{id}/{slug}/{last_read_ordinal+1}`, the permalink, which the server resolves to its page, scrolled to the post.
   - With no read position (0), the badge links to `/t/{id}/{slug}`, where the server lands on the first unread post. After mark-all-read, that is not post 1.
4. **Paging (AC4).**
   - "Older threads" pushes `?cursor=next_cursor` (and `sort`), and "Newest" returns to the first page.
   - A sort toggle switches between "Latest activity" and "Newest threads" (`?sort=created`).
   - The screen saves `scrollY` into `history.state` (debounced `replaceState` on scroll) and restores it when an entry is shown again with its data. The data is the held GET, so it renders at once on back. A new page pushed without saved state goes to the top, because preact-iso scrolls only when the path changes.
5. **390 px (AC3).**
   - The title wraps to two lines at most, with an ellipsis.
   - The tags wrap under it.
   - The counts go on their own line, and the badge stays at the right.
   - Nothing overflows horizontally.
6. **Seed.** The e2e seed grows:
   - alice's read position at 30 on the main thread, so her badge is a permalink;
   - a thread with a 150-character title and six tags;
   - a "Chatter" forum with 30 one-post threads, for paging.

### Tests
- AC1: Vitest checks the title's and badge's hrefs, including the no-position fallback. Playwright clicks the badge (lands on `#post-31` in view, page 2) and the title (page 1).
- AC2: Vitest checks the pins come first with `.row-pinned` and a visible "Pinned". Playwright compares the computed background of a pinned and an unpinned row.
- AC3: Playwright at 390 px checks `scrollWidth <= 390` on the forum with the long title and six tags, and that the counts are visible.
- AC4: Playwright scrolls, follows "Older threads", scrolls, opens a thread, goes back, and checks `scrollY` within 10 px and the cursor still in the URL. Vitest covers the save and restore logic.
- There is no thread-list mockup (settled). The PR compares its screenshots with docs/06 "Thread list views" and the forum index's row styling.

### Settled in grooming (2026-09-24)

- There is no thread-list mockup. Compare screenshots against docs/06 'Thread list views' and the forum-index mockup's list styling, and say so in the PR.

## Notes

**agent:claude-code/phase1-run-415af6aa** at 2026-09-25T04:00:38Z

Built. Where it went and where it moved off the plan:

### What landed
- `screens/ThreadListScreen.tsx` at `/f/:forum` (`?cursor=`, `?sort=created`), `components/ThreadRow.tsx` (reusable by TKT-01M29CTY7K), `state/scroll.ts` (`useScrollMemory`), and `.unread-badge` moved to `components/unread.css` so the index and the rows share it. It adds a `pin` icon.
- The title links to `/t/{id}/{slug}?page=1`. The badge links to `/t/{id}/{slug}/{last_read+1}`, or to the bare thread address when there is no read position. They are sibling links.
- Scroll memory: the position goes into `history.state`. It is written 100 ms after scrolling stops, and at once on any click in the capture phase, which runs before preact-iso pushes. It is restored once per list address, when the data is on screen. A newly pushed page starts at 0.

### Off the plan
- **Chatter has 60 threads, not 30.** Page 2 of 30 would hold 5 rows, which do not scroll at 900 px, so AC4 could not check a page-2 position.
- **The badge thread is its own thread** ("Minutes from the September meeting", alice read to 30), not the main thread. smoke.spec runs first and alice's reply there moves her position to the end, so the main thread has no badge for her by the time thread-list.spec runs. The badge test reads the badge's post number rather than hard-coding 31, because the phone run reads posts and the desktop run then lands later. The phone run does land on #post-31.
- **Playwright follows the list's links with `dispatchEvent('click')`.** `click()` scrolls its target into view first, so the list remembered where Playwright had scrolled it, the bottom, instead of where the reader was. The app was right and the test was wrong; the spec explains why.

### Verification
- Vitest: 17 tests in ThreadListScreen.test.tsx; 682 in the full run. tsc is clean.
- 34 hand mutants. All are killed except `restored.current === key` → `false`, which is equivalent: it differs only if a list's data goes from present to absent and back under the same key, and useResource keeps data while it revalidates.
- Playwright: 16 pass at both widths, 8 of them the new spec's (4 tests × 2 widths).
- The seed changes the forum index's screenshots: Chatter is added and General shows 4 threads. They were regenerated. There is no thread-list mockup (settled in grooming); the PR compares the screenshots with docs/06 "Thread list views" and the index's rows.

**agent:claude-code/phase1-run-415af6aa** at 2026-09-25T04:22:16Z

Review 1 on #88 (head 9bc8885, run #460) had one medium finding: a scroll followed by Back within the 100 ms debounce was lost. It was valid, and is fixed. The suggested fix, flushing in the effect's cleanup, would not work: by the time cleanup runs after a Back, `history.state` already names the next entry, so the position would be written into the wrong one.

### What changed in `state/scroll.ts`
- **Entry ids.** Each history entry gets an id in `history.state`. Every scroll event records the position synchronously, in memory, against the id of the entry the list was rendered for. `savedScroll` prefers that map, then `history.state.scrollY` for a reload.
- **Persisting.** The debounced write, the click-capture write and a new `pagehide` write persist the position to `history.state`, but only while the entry is still the list's own.
- **Ignored scrolls.** Scroll events while `history.state` names another entry are ignored, and so are scroll events before the list has restored. Both are the browser's own restoration, and one of them had overwritten page 1's position with 0 in the Playwright trace.
- **Browser restoration off.** `history.scrollRestoration` is `manual` while a list is mounted, and the previous value is put back on unmount. Chromium's traversal restoration could otherwise land after the list's own and move the reader: this was the one flake left on the phone, forward after an immediate back.

### Tests
- Vitest now has 21 tests on the screen, including: the scroll-then-leave case, events in the gap, nothing recorded before the restore, pagehide, and the scrollRestoration toggle.
- 13 more mutants, all killed.
- Playwright gained a step for the reviewer's case: scroll, Back at once, Forward, and the position is the latest one. Eight consecutive full runs passed, 16/16 each.

## Summary

The thread list is at /f/:forum. Pins come first, tinted and labelled. The title goes to page 1 and the unread badge to the first unread post, as sibling links. Pages use the server cursor in the address, and the scroll position is kept in history.state and restored on back. The e2e seed gains a pinned thread, a thread alice has read to post 30, a thread with a 150-character title and six tags, and a 60-thread Chatter forum. thread-list.spec checks all four acceptance criteria at 390 and 1440 px, and screenshots were added.
