---
schema: 3
id: TKT-01M29CGBW6Z87FHP4DPGREVFD6
title: Build the inline reply expander
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
  - TKT-01M29CEE6V358M5KCBJGHD58HM
  - TKT-01M29CA9C2CNBHGBD9304N0EJE
blocks_on: none
references:
  - ref: design:post-anatomy
    path: docs/06-thread-views.md
  - ref: pr:96
    path: null
claim: null
archive: null
created_at: 2026-09-11T23:23:08Z
updated_at: 2026-09-28T05:26:36Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase1-run-438e26c1
  name: ""
extensions: {}
---

## Description

The "3 replies" control that expands the replying posts inline, in place, read-only, and visually recessed, then collapses again. Ordering inside the expansion is ordinal order.

This is the whole non-tree bargain in one component. It gives a reader the reply-to-reply relationship they want without indenting the thread, and the rule that keeps it honest is that nothing on the canonical page moves.

Suppress the expander when the only reply is the post directly below, which is the same suppression rule the reply-to indicator uses.

## Acceptance criteria

- [x] Expanding and collapsing never changes the position of any canonical post on the page
- [x] Expanded posts are read-only and visually recessed, in ordinal order
- [x] The expander is suppressed when the sole reply is directly below
- [x] Expansion works for a reply that lives on another page

## Implementation plan

Revised at claim, 2026-09-28, after reading the client as it stands (CEE6V, CA9C2, CEE79, CGBXK merged). The replies endpoint exists (`GET /api/v1/threads/{id}/posts/{ordinal}/replies`, dto `PostReplies`), so no server work.

### Approach
1. `components/thread/ReplyExpander.tsx` holds three things: `expanderShown(post, next)`, the suppression rule; `ReplyToggle`, the "N replies ▾" button that leads the post footer, with `aria-expanded` and `aria-controls`; and `Replies`, the region the toggle opens.
2. Suppression: hidden when `reply_count === 1` and the next post on this page is `ordinal + 1` with `reply_to === ordinal`. When that post is on the next page, the reader cannot see it below, so the toggle stays. This settles the page-edge gap and goes into docs/06 "Post anatomy".
3. `Replies` asks the replies endpoint through `apiGet` and `useResource`, as `PostPopover` does. The endpoint serves replies on any page, so a reply on another page needs nothing extra. The held-GET cache keys on the URL and revalidates by ETag. `storeThreadPage` already forgets every held URL under the thread when the log grows. Nothing new goes in `threadCache`.
4. Expanded posts are a read-only, recessed variant: no quote, reply, link or history actions, no reply-to indicator (every one answers the same post), no `id="post-N"` and no `data-ordinal`, so nothing that looks up a canonical post finds a copy. The class is `.post--recessed`, on a `surface2` inset. Each post's `#N` is a link to its permalink. A tombstone shows "Post removed." The posts come in the server's order, which is ordinal order.
5. "Nothing moves": the region goes inside the post, after its footer, so no canonical post changes place in the order, gains an indent, or is replaced. A layout effect keeps the toggle at the same viewport offset across expand and collapse, measured before and after, so the post and everything above it hold still on screen. Posts below move down by the region's height and come back when it closes. That is what "inline" requires, and docs/06 will say so.
6. Collapse is the same toggle, which stays where it is, so focus stays on it.

### Files
`src/components/thread/ReplyExpander.tsx` (new), `src/components/thread/Post.tsx`, `src/screens/ThreadScreen.tsx` (passes the rule's result), `src/components/thread/thread.css`, `cmd/ketju/e2e.go` (seeds `reply_to`), `web/client/e2e/replies.spec.ts` (new), `docs/06-thread-views.md`, `internal/web/spa/dist` (regenerated).

### Tests
- AC1: a Playwright test on page 1 of the seeded thread records the viewport top of the toggle and of every canonical `article[id^=post-]`. It expands, then collapses, and asserts the toggle and every post at or above it have not moved, and every post is back where it was after the collapse.
- AC2: a Vitest test asserts the expanded posts come in ordinal order, carry `.post--recessed`, have no action buttons, no `id` and no reply-to indicator, and that the canonical post keeps its id.
- AC3: a Vitest table test of `expanderShown` covering: sole reply below, which is suppressed; two replies; a sole reply further down; and a sole reply at the page edge. A render test asserts no toggle for the suppressed case.
- AC4: the seed gives post 1 replies on pages 2 and 3. Playwright expands them on page 1 and asserts both render. A Vitest test does the same against a stubbed endpoint.

### Seed
The main e2e thread gets `reply_to` on a few posts: 4→3 (sole reply directly below, suppressed), 6 and 8→5, 26→25 (page edge), and 30 and 55→1 (other pages). Post counts and read positions do not change.

### Settled in grooming (2026-09-24)

- The suppression rule on page edges is a UI detail: settle it, write it into docs/06, and note it.

## Notes

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T05:25:58Z

Settled two gaps in docs/06 "Post anatomy", both UI details within the grooming note's allowance.

1. **Page edge.** The expander is suppressed only when the sole reply is the next post on this page, meaning `ordinal + 1` with `reply_to` equal to this post. When the sole reply opens the next page, the reader cannot see it below, so the expander stays. The alternative was fetching or peeking at the next page to hide it. That lost because it costs a request per page foot and hides a reply the reader cannot see.
2. **"Nothing about the canonical page moves."** An inline expansion has to push what is below it down, so I read the rule as: no canonical post changes order, gains an indent, or is replaced, and the expanded post and everything above it hold still on screen. The posts below move by the region's height while it is open and return exactly when it closes. The pre-groom plan asked whether "position" meant viewport pixels, which would need an overlay. That lost because docs/06 says "inline, in place", and an overlay is neither. AC1 is ticked on this reading. The Playwright test asserts every part of it at 390 px and 1440 px.

Other choices:
- Replies go through `apiGet` and `useResource`, keyed on the URL, like PostPopover. `threadCache` is not extended. `storeThreadPage` already drops every held URL under the thread when the log grows.
- Tombstone posts, which are the canonical deleted rows, get no expander, because they have no footer. The moderator's tombstone variants belong to TKT-01M29CKRJ.
- The e2e seed now puts `reply_to` on main-thread posts 4, 6, 8, 26, 30 and 55. The a11y scan (desktop) and the target check (phone) now run with #1's replies open. Both found a problem, and both are fixed in this PR.

## Summary

Built the inline reply expander (`web/client/src/components/thread/ReplyExpander.tsx`). "N replies" leads a post's footer and opens the post's direct replies inside the post, below its footer. They are read-only copies, recessed on `surface2`, come from `GET /threads/{id}/posts/{n}/replies` in ordinal order, and a reply on any page opens the same way. The copies carry no id, `data-ordinal`, actions or reply-to indicator.

The expander is suppressed when the sole reply is the next post on this page, and it stays at a page's foot. A layout effect holds the toggle's place on screen across expand and collapse.

docs/06 "Post anatomy" now records both rules. The e2e seed gives the main thread `reply_to` on posts 4, 6, 8, 26, 30 and 55.

Tests:
- Vitest in `ReplyExpander.test.tsx` covers the suppression table, the render, order, read-only, tombstone, error and scroll hold.
- Playwright in `e2e/replies.spec.ts` covers position stability and replies on pages 2 and 3, at both widths.
- The a11y (desktop) and targets (phone) checks now run with #1's replies open.
