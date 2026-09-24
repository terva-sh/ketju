---
schema: 3
id: TKT-01M29CEE6V358M5KCBJGHD58HM
title: Build the thread page with pager and unread divider
type: task
status: ready
status_reason: null
priority: urgent
due_on: null
labels:
  - area:client
  - effort:L
assignees: []
milestone: phase-1
parent: TKT-01M29BN367F3YQZESM63VN26GP
origin: null
dependencies:
  - TKT-01M29CCC02K8F14P0SKAYFMSE6
  - TKT-01M29CA9AXKRTS6KMN5CTH7461
  - TKT-01M29BW9P4PXMFKF4VAP8K9QPD
  - TKT-01M29BW9NWKJGM3ZQ2DJH8WPWH
  - TKT-01M29CA9C2CNBHGBD9304N0EJE
blocks_on: none
references:
  - ref: design:canonical-view
    path: docs/06-thread-views.md
  - ref: design:mockups-thread
    path: docs/mockups/ThreadDark.dc.html
claim: null
archive: null
created_at: 2026-09-11T23:22:04Z
updated_at: 2026-09-24T10:51:56Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase1-run-415af6aa
  name: ""
extensions: {}
---

## Description

The canonical view: posts in order, the post anatomy from docs/06, the pager, and the "N new posts below" divider between read and unread.

docs/06 calls the divider the single most valuable piece of UI in a linear forum, and it ships in the first milestone rather than with the lenses. A returning reader lands on the page containing their last read position with the divider drawn in the right place.

The reply-to indicator is suppressed when the referenced post is immediately above, which is Discourse's rule and the reason most threads read as plain flat text.

## Acceptance criteria

- [ ] The reply-to indicator is hidden when the referenced post is directly above
- [ ] Clicking a reply-to indicator scrolls to the post when it is on the page and opens a popover when it is not
- [ ] Tombstones render in place with the ordinal preserved
- [ ] Playwright screenshots at 390 px and 1440 px are committed beside the tests and compared against the matching file in docs/mockups/; every visible divergence is listed in the pull request description
- [ ] A returning reader lands on the page containing last_read_ordinal + 1, as docs/06 specifies, with the divider drawn between read and unread

## Implementation plan

Pre-groom plan (2026-09-24). Revise at claim, after reading the code as it stands then.

### Approach
1. `screens/ThreadPage.tsx` at `/t/:id/:slug` and `/t/:id/:slug/:ordinal` calls `loadThreadPage(id, page, lens)` (CEE5H), stores the result in `currentThread`, and applies the stale-slug replace (CEE52).
2. Landing: when there is no `page` in the URL and the viewer is signed in, open the page containing `last_read_ordinal + 1` (docs/06, decided in docs/10 Q14). Then `route(url + '?page=N', true)` so the URL states the view. This needs the server to choose the page (a missing `page` means the last-read page) or to accept an ordinal; see interfaces item 7. Scroll to the divider, or to `#post-{last_read}` when the divider is off this page.
3. `components/Post.tsx` follows the docs/06 anatomy: `<article id="post-{ordinal}">` with a header (ordinal, avatar, handle, role and kind badge, relative time, "(edited)" when `edited_at`, and the reply-to indicator), a body (`html`), attachments, and a footer (reply expander slot for CGBW6, then quote, reply, link, flag, history, more). The `#post-N` id matches the anchors `internal/render/quote.go` emits.
4. `UnreadDivider.tsx` goes between the posts with `ordinal <= viewer.last_read_ordinal` and those above it, reading "N new posts below" where N = `thread.post_count - last_read_ordinal`. Skip it when `last_read_ordinal` is 0 (never read) or equals `post_count`.
5. Reply-to indicator: hide it when `reply_to === null` or `reply_to_suppressed`. The server computes suppression, so the client does not repeat the rule. On click: if `#post-{n}` is in the DOM, `scrollIntoView` it; otherwise open `PostPopover`, which fetches `GET /threads/{id}/posts/{n}` (CA9C2) and offers a "go to" permalink. The same handler catches clicks on `a.quote-source`.
6. `Pager.tsx`: desktop numbered pages ("posts 51–75 of 212"); phone "Page 3 of 9" with previous and next, 44 px each. The picker sheet is phase-2 or later.
7. Tombstones: when `deleted` and there is no html, render `Tombstone` with the ordinal and "Post removed" in place, keeping the `id`. Moved and moderator variants belong to CKRJ (phase-2).

### Files
`web/client/src/screens/ThreadPage.tsx`, `src/components/{Post,PostHeader,UnreadDivider,ReplyToIndicator,PostPopover,Pager,Tombstone,Avatar,RoleBadge,RelativeTime}.tsx`.

### Tests
- AC1: Vitest with `thread.member.json` asserts the divider sits between `last_read_ordinal` and `last_read_ordinal+1`. Playwright seeds a 60-post thread with last-read 30 and page size 25, opens `/t/{id}/{slug}`, and asserts `?page=2` and the divider after #30.
- AC2: Vitest with fixtures where `reply_to_suppressed` is true and false asserts the presence of the indicator.
- AC3: Playwright clicks an on-page indicator and asserts `#post-N` is in view with the URL unchanged, then an off-page one and asserts the popover shows post N with a "go to" link.
- AC4, reworded: commit screenshots at 1440 and 390 px, light and dark, compare them by eye against Thread{Light,Dark}.dc.html and MobileThread{Light,Dark}.dc.html, and list any divergence in the PR.
- AC5: Vitest with a `deleted: true` fixture asserts `#post-N` exists with the tombstone text and ordinal.

### Design
docs/06 "Paged, not infinite", "Landing on return", "Post anatomy", "Mobile" (Posts, Pages); docs/07 thread response shape; `dto.Post` comments; ThreadLight.dc.html ("3 new posts below", "last read #63", "Post removed by its author.").

### Interactions
Depends on CCC02, CA9AX, CEE5H and CEE52. It needs CA9C2 for the popover, which is not a declared dependency. It hosts CEE79 (composer), CGBW6 (expander), CGBWN (beacon) and CGBX4 (shortcuts). CKRJ extends tombstones.
Contradiction: the AC says "page containing their last read ordinal", while docs/06 says the page containing `last_read_ordinal + 1`. They differ when last-read falls on a page boundary; docs/06 is the decided text.

## Notes

**agent:claude/phase1-groom** at 2026-09-24T05:06:25Z

Acceptance criteria rewritten during Phase 1 grooming (2026-09-24), agreed with the user as part of preparing the autonomous run. Rewritten criteria move to the end of the list, so their numbers changed. 'Matches the mockups' now names its method: committed Playwright screenshots at both widths compared against docs/mockups/, with every divergence listed in the PR.

**agent:claude/phase1-groom** at 2026-09-24T05:17:38Z

Criterion rewritten during Phase 1 grooming (2026-09-24). It moves to the end of the list. It said 'their last read ordinal', which differs from docs/06's last_read_ordinal + 1 when the last read post ends a page. docs/06 records the decision, so it wins.

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T09:47:41Z

The server now chooses the landing page (TKT-01M29CA9AX, merged in PR #56). With no page and no ordinal, GET /api/v1/threads/{id} serves a signed-in reader the page holding last_read_ordinal + 1, or the last page once everything is read, and ?ordinal=N serves N's page. The client reads page.number from the response and does not recompute it (plan item 2 / interfaces item 7). The ETag hashes the body, so any change the client can see is a new tag.

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T10:51:56Z

From TKT-01M29CEE52 (Build the client router and route table), now built: routes.tsx sends /t/:id/:slug, /t/:id/:slug/:ordinal and .../history to screens/ThreadScreen.tsx. That screen is a stand-in: it fetches the thread and replaces a stale slug. Replace it with the thread page. When you do, read page and lens only through useThreadView() (src/view.ts); hasPage tells 'no page named' from page 1, for the landing rule. After the thread loads, call canonicalThreadUrl(location, thread) and route(to, true) when it returns a URL. CEE52's AC2 and AC3 stay true only if both are kept. view.test.tsx's stale-slug test renders the app at a thread URL with a stubbed fetch, and it will keep passing through the new screen if the redirect stays.
