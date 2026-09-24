---
schema: 3
id: TKT-01M29CEE6V358M5KCBJGHD58HM
title: Build the thread page with pager and unread divider
type: task
status: done
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
  - ref: ticket:TKT-01M39WBN4QAMNRBXCGD85CDJF0
    path: null
  - ref: pr:64
    path: null
  - ref: ticket:TKT-01M39WZY8C6ZK5CNZCH4CPXHT1
    path: null
claim: null
archive: null
created_at: 2026-09-11T23:22:04Z
updated_at: 2026-09-24T14:27:44Z
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

- [x] The reply-to indicator is hidden when the referenced post is directly above
- [x] Clicking a reply-to indicator scrolls to the post when it is on the page and opens a popover when it is not
- [x] Tombstones render in place with the ordinal preserved
- [ ] Playwright screenshots at 390 px and 1440 px are committed beside the tests and compared against the matching file in docs/mockups/; every visible divergence is listed in the pull request description
- [x] A returning reader lands on the page containing last_read_ordinal + 1, as docs/06 specifies, with the divider drawn between read and unread

## Implementation plan

Claim-time plan (2026-09-24). It revises the pre-groom plan after reading the client at 6cf16d8 (CEE5H merged). The pre-groom plan had no "Settled" section. Its contradiction note is resolved as it said: docs/06 is the decided text, so a returning reader lands on the page holding `last_read_ordinal + 1`.

### Approach
1. **`screens/ThreadScreen.tsx` becomes the thread page**, keeping its routes and everything TKT-01M29CEE52 and CEE5H pinned:
   - a keyed answer;
   - the canonical-slug replace;
   - a 404 renders NotFound;
   - any other failure renders ErrorPanel with Retry.

   It already loads through `useResource` and `loadThreadPage`. It renders the header (title, forum), the Pager above and below, and the post list.
2. **Landing.** With no `page` and no ordinal in the URL, the request carries no page, and the server lands a signed-in reader on the page holding their first unread post (docs/07, TKT-01M29CA9AX). The client does not choose a page.
   - **Changed from the pre-groom plan:** the URL is not rewritten to `?page=N`. A rewrite changes the request key and costs a second request for the page on screen. A URL with no page also means "where I left off", so reloading it later lands on the new position, and the permalink stays the shareable form.
   - **Scroll targets, in order:**
     - the permalink's post, for `/t/id/slug/N`;
     - `#post-N` from the hash;
     - on a landing, the divider when it is on the page, else `#post-{last_read}`.
3. **`components/Post.tsx`, after the docs/06 anatomy and ThreadLight.dc.html:**
   - `<article id="post-N">`;
   - the header: the ordinal, which links to the permalink; an avatar (`/blobs/{hash}`, or the initial on a colour derived from the handle); the display name; the role and kind badge (shown for moderator, admin and agent; plain members get none, as in the mockup); the time; "(edited)"; and the reply-to indicator;
   - the body: the server-rendered HTML, which internal/render sanitizes;
   - attachments;
   - the footer: only actions that work today, "link" (the permalink) and "history". Quote, reply, flag and the reply expander arrive with their tickets (CEE79, CGBW6); no dead buttons.
4. **Reply-to indicator (`ReplyTo`).**
   - It is hidden when `reply_to` is null or `reply_to_suppressed` is set. The server computes suppression and the client does not repeat the rule.
   - On click: if `#post-N` is on the page, scroll it into view without changing the URL. Otherwise open `PostPopover`, which loads `GET /threads/{id}/posts/{N}` through apiGet and offers "Go to #N".
   - One delegated handler on the post list does the same for `a.quote-source` links in bodies (internal/render/quote.go).
5. **`UnreadDivider`.**
   - It goes before the first post whose ordinal exceeds `viewer.last_read_ordinal` and reads "N new posts below", with N = `post_count - last_read_ordinal`. The right side reads "last read #L".
   - It is absent when `last_read_ordinal` is 0 or at least `post_count`, and absent on a page where no post crosses the line.
   - Unread posts carry the edge marker class.
6. **`Tombstone`.** When `deleted` and `html` is empty (anonymous and member viewers), it renders in place with `id="post-N"`, the ordinal, and "Post removed." A moderator's response carries the HTML, which renders as a post marked deleted. The moved variants belong to CKRJ.
7. **`Pager`.**
   - Desktop: numbered pages with an ellipsis window, plus "posts A–B of T".
   - Phone: "Page N of M" with previous and next, each at least 44 px. The CSS switches between them at 720 px.
   - Links keep the lens and drop the ordinal, through `useThreadView().setPage`. The picker sheet comes later.
8. **Styles.** `thread.css`, imported by the screen, uses custom properties with the mockup's light values as fallbacks, so CCC0F (the CSS system, not yet built) can supply the tokens and dark theme without touching the components.

### Tests
- **Vitest, over pages built from the member golden:**
  - AC1: the indicator is present or absent by `reply_to` and `reply_to_suppressed`.
  - AC2: a click on-page scrolls with the URL unchanged; an off-page click opens a popover with the fetched post and a "Go to" link; a quote-source link does the same.
  - AC3: the tombstone has `#post-N`, its ordinal and the text, and a moderator's deleted post shows its HTML.
  - AC5: the divider sits between `last_read_ordinal` and `+1`, with the count and its absent cases, and a landing asks for no page.
  - The Pager's numbering, window and phone labels.
- **AC4 (screenshots):** attempted at the end with a local headless Chromium against the built client with a stubbed API. It is ticked only if the screenshots are committed and every divergence from the mockups is listed. Playwright in CI is CGBY1's.

### Interactions (carried from the pre-groom plan)
- It depends on CCC02, CA9AX, CEE5H and CEE52, all done. It uses CA9C2 for the popover.
- It hosts CEE79 (composer), CGBW6 (reply expander), CGBWN (read beacon) and CGBX4 (shortcuts). CKRJ extends tombstones.
- TKT-01M39VNEC1 matters before this page ever loads two pages of one thread at once.

## Notes

**agent:claude/phase1-groom** at 2026-09-24T05:06:25Z

Acceptance criteria rewritten during Phase 1 grooming (2026-09-24), agreed with the user as part of preparing the autonomous run. Rewritten criteria move to the end of the list, so their numbers changed. 'Matches the mockups' now names its method: committed Playwright screenshots at both widths compared against docs/mockups/, with every divergence listed in the PR.

**agent:claude/phase1-groom** at 2026-09-24T05:17:38Z

Criterion rewritten during Phase 1 grooming (2026-09-24). It moves to the end of the list. It said 'their last read ordinal', which differs from docs/06's last_read_ordinal + 1 when the last read post ends a page. docs/06 records the decision, so it wins.

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T09:47:41Z

The server now chooses the landing page (TKT-01M29CA9AX, merged in PR #56). With no page and no ordinal, GET /api/v1/threads/{id} serves a signed-in reader the page holding last_read_ordinal + 1, or the last page once everything is read, and ?ordinal=N serves N's page. The client reads page.number from the response and does not recompute it (plan item 2 / interfaces item 7). The ETag hashes the body, so any change the client can see is a new tag.

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T10:51:56Z

From TKT-01M29CEE52 (Build the client router and route table), now built: routes.tsx sends /t/:id/:slug, /t/:id/:slug/:ordinal and .../history to screens/ThreadScreen.tsx. That screen is a stand-in: it fetches the thread and replaces a stale slug. Replace it with the thread page. When you do, read page and lens only through useThreadView() (src/view.ts); hasPage tells 'no page named' from page 1, for the landing rule. After the thread loads, call canonicalThreadUrl(location, thread) and route(to, true) when it returns a URL. CEE52's AC2 and AC3 stay true only if both are kept. view.test.tsx's stale-slug test renders the app at a thread URL with a stubbed fetch, and it will keep passing through the new screen if the redirect stays.

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T12:53:34Z

From TKT-01M29CEE5H (fetch layer). ThreadScreen already fetches through useResource(key, s => loadThreadPage(id, q, s), () => peekGet(key)), with the title falling back to cachedThread(id).header and failures rendered by ErrorPanel with Retry; build the page on that rather than a new fetch. Types are in src/api/dto.ts (ThreadPage, Post, ...). Use isProblem(err, 'not-found') rather than comparing .type: a source-scan test (src/api/one-path.test.ts) fails any screen or component that compares .type/.status or calls fetch.

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T14:03:57Z

Before this page loads two pages of one thread at once (a pager prefetch, say), TKT-01M39VNEC1 (Stop returning a thread page the cache refused to its caller) matters: loadThreadPage can resolve with a page the cache refused. ThreadScreen alone is safe because it aborts a superseded request.

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T14:16:10Z

### What was built
**`screens/ThreadScreen.tsx` is the thread page.** It keeps every guarantee from CEE52 and CEE5H: a keyed answer, the slug replace, NotFound for a 404, and ErrorPanel with Retry for any other failure.

**Landing.** With no page and no ordinal in the URL, the request names no page and the server lands the reader. The URL is left as it is. This changes the pre-groom plan; the reason is in the plan.

**Scrolling.** The page scrolls once per request key, on arrival, to the first of:
- the permalink's post;
- `#post-N` from the hash;
- on a landing, the divider, or else `#post-{last_read}`.

A revalidation of the same page does not scroll again.

**Components in `components/thread/`:**
- `Post`: the anatomy from docs/06 and the mockups. The footer has only link and history, because the other actions arrive with their own tickets.
- `ReplyTo`: hidden when `reply_to` is null or `reply_to_suppressed` is set. The server's word on suppression is final.
- `PostPopover`: fetches `GET /threads/{id}/posts/N` through apiGet and offers "Go to #N". It closes on Escape and on moving to another page.
- `Tombstone`.
- `UnreadDivider`: `dividerBefore` draws it only where the line is on the page: a read post is on the page too, or the page opens at `last_read + 1`.
- `Pager`: a numbered window with the "posts A–B of T" span, and "Page N of M" at phone width. A page link always names its page, including page 1, because an address with no page lands the reader instead.
- `Avatar`, `RoleBadge` (moderator, admin, agent), `PostTime`.

**Following links.** One delegated handler covers reply-to and quote-source links. It scrolls to the post when it is on the page and leaves the URL alone; otherwise it opens the popover.

**`thread.css`.** Custom properties with the mockup's light values as fallbacks, so CCC0F supplies the tokens and the dark theme. The phone layout applies below 720 px, with 44 px targets.

### Unticked: AC4, the screenshots
Nothing in the repository runs Playwright yet; CGBY1 adds it. The page also lacks the app shell (CEE4K), the CSS system and fonts (CCC0F). Screenshots now would diverge mostly in ways those tickets remove.

I rendered the page locally in headless Chromium at 1440 and 390 px against a stubbed API, and did not commit the result. The structure matched the mockups and the console showed no errors. The criterion is filed as draft TKT-01M39WBN4Q (Commit thread page screenshots at 390 and 1440 px against the mockups), which depends on CGBY1, CEE4K and CCC0F and is linked here.

### Verification
- 40 new Vitest tests cover:
  - AC1: indicator presence, trusting the server's suppression flag.
  - AC2: scrolling on the page with the URL unchanged; a popover with its fetched post and a Go-to link; quote-source links; the popover's error state; closing on a page move.
  - AC3: a tombstone in place with its ordinal and anchor; a moderator's deleted post shows its content, marked.
  - AC5: the divider from the golden (152 new, above #61); mid-page placement; four cases where it is absent and three where it is drawn; a landing asks for no page and scrolls to the divider, else to the last read post; a permalink scrolls to its post; a chosen page does not scroll; no unread edge for a reader who has read nothing.
  - The pager's window, links and span; the badges, edited mark, avatar and attachments.
- Mutation: 32 mutants were run and 31 killed. The survivor is equivalent: `lastRead >= postCount` against `>`, where no ordinal on the page can exceed `post_count`.
- The suite has 368 Vitest tests. The bundle is 46 kB of JavaScript (17.3 kB gzipped) and 5.6 kB of CSS.

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T14:16:22Z

Correction to the previous note's mutation line: 28 mutants were run, not 32. 27 were killed, 4 of them only after tests were added for the first-round survivors (the pager span on a partial last page, no unread edge with nothing read, one scroll per arrival, the popover closing on a page move). 1 is equivalent: lastRead >= postCount against >.

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T14:27:44Z

### Review 421, answered

**Medium: the history route had become the thread page.** `/t/id/slug/N/history` rendered the ordinary post list, so the Post footer's "history" link led nowhere distinct.

The route now renders a placeholder, as the other unbuilt screens do. It keeps the thread's title and slug correction, shows "History of post #N" and "This screen is not built yet", and links back to the post. It does not show the post list or the pager.

No ticket covered a history screen, so I filed draft TKT-01M39WZY8C (Build the post history screen). It describes the CA9C2 endpoint's shape and access rules, and it is linked here.

**Low: an unread tombstone had no unread edge.** A tombstone past the read line now carries the edge, the same way posts do. It still carries none when nothing has been read.

**Tests:**
- the history route: its screen name, the heading, the back link, and no post list or pager;
- the tombstone edge in both cases.

**Mutation:** 3 mutants, all killed. There are 371 Vitest tests.

## Summary

The thread page landed: posts after the docs/06 anatomy, the reply-to indicator (server-decided suppression) with scroll-or-popover following for it and quote links, tombstones in place, the unread divider drawn only where the read line is on the page, landing on the server-chosen page with a scroll to the divider, and a numbered/phone pager. Styles use custom properties with mockup fallbacks for CCC0F to take over. AC4 (committed Playwright screenshots against the mockups) is unticked and carried by draft TKT-01M39WBN4Q, which waits on CGBY1, CEE4K and CCC0F.
