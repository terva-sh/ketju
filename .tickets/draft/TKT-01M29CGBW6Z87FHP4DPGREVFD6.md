---
schema: 3
id: TKT-01M29CGBW6Z87FHP4DPGREVFD6
title: Build the inline reply expander
type: task
status: draft
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
  - TKT-01M29CEE6V358M5KCBJGHD58HM
  - TKT-01M29CA9C2CNBHGBD9304N0EJE
blocks_on: none
references:
  - ref: design:post-anatomy
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

The "3 replies" control that expands the replying posts inline, in place, read-only, and visually recessed, then collapses again. Ordering inside the expansion is ordinal order.

This is the whole non-tree bargain in one component. It gives a reader the reply-to-reply relationship they want without indenting the thread, and the rule that keeps it honest is that nothing on the canonical page moves.

Suppress the expander when the only reply is the post directly below, which is the same suppression rule the reply-to indicator uses.

## Acceptance criteria

- [ ] Expanding and collapsing never changes the position of any canonical post on the page
- [ ] Expanded posts are read-only and visually recessed, in ordinal order
- [ ] The expander is suppressed when the sole reply is directly below
- [ ] Expansion works for a reply that lives on another page

## Implementation plan

Pre-groom plan (2026-09-24). Revise at claim, after reading the code as it stands then.

### Approach
1. `components/ReplyExpander.tsx` in the post footer shows "N replies ▾" when `post.reply_count > 0`.
2. Suppression: hide it when `reply_count === 1` and the next canonical post on this page has `reply_to === post.ordinal` and `ordinal === post.ordinal + 1`. When that post is on the next page, the client cannot know without fetching. Keep the control in that case; the docs leave it open.
3. On expand, fetch `GET /threads/{id}/posts/{ordinal}/replies` (CA9C2), which works whether the replies are on this page or another. Cache the result per `(threadId, logLength, ordinal)` in `threadCache`, then render the replies in ordinal order inside a `<div class="expansion" role="region">` placed after the footer.
4. The expanded posts render through `Post` with a `readOnly` prop: no reply, quote or flag actions, no `id="post-N"` (the canonical post keeps the id), and a `.post--recessed` class (inset, `surface2` background, smaller type). Each carries a "go to #N" link.
5. "Nothing canonical moves": the expansion inserts content only below the post whose control was clicked. Posts above it keep their positions and posts below shift down, as any in-flow content does. At claim, confirm with the ticket's author whether "position" means document order (kept) or viewport pixels (which would call for an overlay). Keep scroll anchored on the clicked control with `scrollTop` compensation where the browser lacks `overflow-anchor`.
6. Collapse removes the region and restores focus to the toggle. The toggle carries `aria-expanded` and `aria-controls`.

### Files
`web/client/src/components/ReplyExpander.tsx`, `src/components/Post.tsx` (readOnly variant), `src/state/threads.ts` (replies cache), `src/styles/components.css`.

### Tests
- AC1: Playwright records the `boundingClientRect().top` of every canonical `article[id^=post-]` above the toggle and the viewport offset of the toggle, expands and collapses, and asserts they are unchanged. After a collapse, it asserts every post's position equals the original.
- AC2: Vitest asserts the expanded posts are in ascending ordinal order, have no action buttons, and carry `.post--recessed`.
- AC3: Vitest with a fixture where post 10 has `reply_count: 1` and post 11 has `reply_to: 10` asserts no toggle; with `reply_count: 2` it asserts the toggle.
- AC4: Playwright with a seeded reply on page 3 to a post on page 1 expands on page 1 and asserts the reply renders.

### Design
docs/06 "Post anatomy" (expander bullet); docs/07 `GET /threads/{id}/posts/{ordinal}/replies`; ThreadLight.dc.html footer ("3 replies" control).

### Interactions
Depends on CEE6V (Post, ThreadPage) and needs CA9C2 (replies endpoint), which is not a declared dependency. CGBXK checks focus and `aria-expanded`. On the phone, the expander sits in the footer beside reply and quote (docs/06 Mobile).
Open in docs: the suppression rule when the sole reply is the first post of the next page.

### Settled in grooming (2026-09-24)

- The suppression rule on page edges is a UI detail: settle it, write it into docs/06, and note it.
