---
schema: 3
id: TKT-01M29CEE6D4D07WDBFF8N6NNEC
title: Build the thread list screen
type: task
status: ready
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

A forum's threads: pinned first, then by last post, each row with title, tags, author, reply count, last poster, last post time, and an unread badge.

The badge is a control, not an ornament. Clicking it jumps to the first unread post; clicking the title goes to page 1. That distinction is small to build and it is most of what makes a forum feel fast to a returning reader.

## Acceptance criteria

- [ ] Clicking the unread badge lands on the first unread post and clicking the title lands on page 1
- [ ] Pinned threads are visually distinct and sort first
- [ ] Rows stay readable at 390 px with a long title and several tags
- [ ] Paging through the list preserves scroll position on back navigation

## Implementation plan

Pre-groom plan (2026-09-24). Revise at claim, after reading the code as it stands then.

### Approach
1. `screens/ThreadList.tsx` at `/f/:forum?cursor=&sort=` calls `useResource('/forums/{slug}/threads?…')` and renders `dto.ThreadList.threads` in server order: pinned first, then `last_post_at` descending. The client does not re-sort.
2. Each row shows title, tag chips, author, `reply_count`, `last_post_author`, relative `last_post_at`, and the unread badge. Pinned rows (`pinned !== null`) get a `.row--pinned` class and a pin icon plus text label, so they are not distinguished by color alone. Locked rows get a lock icon.
3. Links: the title goes to `/t/{id}/{slug}` (page 1, explicitly `?page=1`, so landing-at-last-read does not apply). The badge is a separate `<a>` to `/t/{id}/{slug}/{last_read_ordinal + 1}`, the permalink form, which the server resolves to the containing page (CA9AX AC3). The two are sibling elements, never nested.
4. Paging: a "More" or "Next" link carries `?cursor={next_cursor}` in the URL (docs/07: lists are cursor-paged). Screens keep the fetched ThreadList in the `useResource` cache keyed by URL, so back navigation renders synchronously. Save `scrollY` into `history.state` before leaving, and restore it after the render.
5. At 390 px, the title wraps to at most two lines (`line-clamp` with an ellipsis), tags wrap under it, and the counts are right-aligned on their own line. There is no horizontal overflow.
6. Anonymous viewers get no badge; branch on `isSignedIn`, as on the forum index.

### Files
`web/client/src/screens/ThreadList.tsx`, `src/components/ThreadRow.tsx`, `src/styles/components.css`.

### Tests
- AC1: Vitest with `threads.member.json` asserts the title `href` is `/t/{id}/{slug}?page=1` and the badge `href` is `/t/{id}/{slug}/{last_read_ordinal+1}`. Playwright clicks each and asserts the landed page contains `#post-{n}` in view for the badge, and page 1 for the title.
- AC2: Vitest asserts pinned rows come first in the DOM and carry `.row--pinned`, which has a distinct computed background in Playwright.
- AC3: Playwright at 390 px with a 150-character title and six tags asserts `document.documentElement.scrollWidth <= 390`, and that the counts stay visible.
- AC4: Playwright scrolls, follows the Next cursor, scrolls again, opens a thread, goes back, and asserts `scrollY` within 10 px and the same cursor in the URL.

### Design
docs/06 "Thread list views"; docs/07 `GET /forums/{slug}/threads` and cursor paging; `dto.ThreadRow` comments (the badge links to LastReadOrdinal+1). No thread-list mockup exists in `docs/mockups/`, so follow the front page's row styling.

### Interactions
Depends on CCC02, CA9AG (thread list), and CEE5H. The `/unread` and `/new` phase-3 screens (CTY7K) can reuse `ThreadRow`. `n` in CGBX4 reads this data.
Open in docs: no mockup for this screen, so there is nothing to compare screenshots against.

### Settled in grooming (2026-09-24)

- There is no thread-list mockup. Compare screenshots against docs/06 'Thread list views' and the forum-index mockup's list styling, and say so in the PR.
