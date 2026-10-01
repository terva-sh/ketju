---
schema: 3
id: TKT-01M29CTYA0E5Y06WWYY2ETBF8V
title: Add the client SSE subscription with the new-posts pill
type: task
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area:client
  - area:realtime
  - effort:M
assignees: []
milestone: phase-3
parent: TKT-01M29BN36D34YVKVRPNQD3PHDV
origin: null
dependencies:
  - TKT-01M29CRJ3XXH2CHG4GKC4CGXAS
  - TKT-01M29CEE6V358M5KCBJGHD58HM
blocks_on: none
references:
  - ref: design:live-updates
    path: docs/06-thread-views.md
  - ref: pr:320
    path: null
claim: null
archive: null
created_at: 2026-09-11T23:28:54Z
updated_at: 2026-10-01T15:10:58Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase3-ctya0
  name: ""
extensions: {}
---

## Description

While a thread page is open the client holds an SSE subscription. On a hint it fetches the new posts and shows a "3 new posts, show" pill at the bottom rather than inserting them unasked, because inserting them moves the page under the reader. If the reader is already at the very bottom, posts stream in directly.

Polling every 30 seconds is the fallback when SSE is unavailable, which covers proxies that buffer streams.

## Acceptance criteria

- [x] New posts never shift content under a reader who is not at the bottom: they wait behind the pill and no post element is inserted (ThreadScreen.test.tsx 'new posts wait behind the pill for a reader above the bottom', e2e live.spec.ts 'a new post waits behind the pill')
- [x] A reader at the bottom receives posts inline with no pill (ThreadScreen.test.tsx 'a reader at the bottom gets posts inline with no pill', e2e 'a reader at the bottom sees the post arrive')
- [x] Polling every 30 s takes over when the stream errors or its ready event is 10 s late, retries the stream every fourth poll, and stops when a stream says ready (live.test.ts, e2e 'polling takes over when the stream is refused')
- [x] The subscription closes on navigation away and after 60 s in a hidden tab, and leaks no connection (live.test.ts, ThreadScreen.test.tsx 'leaving the thread closes the stream')
- [x] e2e live.spec.ts passes at 390 and 1440 px with one axe scan per test and no violations
- [x] The dist committed from go generate ./internal/web/spa keeps the reader within the 70000 B gzip budget, and the PR records the change from 60465 B
- [x] The pill counts only posts that arrived after the page opened or the pill was last taken, never posts that already existed on later pages; show reveals them in place when they all fall on the page in view and otherwise goes to the page holding the first (ThreadScreen.test.tsx 'the pill on an earlier page counts only posts that arrived after it opened, and goes to the first of them', 'a full last page sends the pill to the next page', 'show reveals the posts and focuses the first')
- [x] Every hint refetches the page on screen in full, so an edit, a removal and a restore all show, together or apart, without moving the post in view (ThreadScreen.test.tsx 'an edit and a removal in one change both show', 'an edit applies in place without moving the post in view')
- [x] A post behind the pill is not marked read (ThreadScreen.test.tsx 'a post behind the pill is not marked read')
- [x] Posting a reply routes to its permalink as today, showing it at once on its own page when the page in view is full or earlier, with held posts above it and no pill, so a reply posted here never counts in the pill (ThreadScreen.test.tsx 'a reply from a full page goes to its own page', 'posting a reply with posts held takes the reader to the reply', 'the reader's own reply never counts in the pill')

## Implementation plan

Groomed 2026-10-01 for Phase 3 (TKT-01M3V3BVPY), against main at a599b48. Read for this plan:
- docs/06 "Live updates" and docs/07's `after_log_length` rules ("What the thread endpoint does");
- the server contract in TKT-01M29CRJ3X (Build the SSE endpoint and fan-out) and docs/02 "Live updates";
- note 1 on this ticket;
- in `web/client`: `src/state/threads.ts`, `src/state/readTracker.ts`, `src/screens/ThreadScreen.tsx`, `src/api/client.ts`, `src/testing.tsx`, `scripts/bundle.mjs` and `e2e/session.ts`.

### Settled in grooming

- **Behaviour** is docs/06 "Live updates" and its run decision (2026-10-01, pending maintainer review).
  - "At the very bottom" means the end of the last post is in view, within 48 px.
  - The pill counts posts that arrived after the reader opened the page or last took the pill: `header.post_count` now, less the count then. It never counts posts that already existed on later pages, edits, or the reader's own replies.
  - "Show" reveals the held posts in place when they all fall on the page in view, and otherwise goes to the page holding the first one.
  - Edits, removals and restores of posts already shown apply at once, with the scroll corrected against the first post in view.
  - Each hint refetches the whole page, never a fragment (Terva review 1696, below).
  - Polling every 30 s takes over on a stream error, or when no `ready` arrives within 10 s. The client retries the stream every fourth poll.
  - A tab hidden for 60 s closes its stream.
- **Server contract** (TKT-01M29CRJ3X, docs/02 decision).
  - `EventSource('/events?threads=<id>')` receives `event: ready`, then `event: thread` with `{thread_id, log_length}` for the current state, then hints as they come. It also receives heartbeat comments.
  - A refused subscription is an HTTP error, which `EventSource` treats as fatal. A lost session or lost access closes the stream, and the browser's reconnect is judged afresh.
  - A full limit is a 429, so the client polls.
- **No fragments** (changed after Terva review 1696 on PR #271). `?after_log_length=L` answers only posts with a revision written at or after L (docs/07). A delete or restore writes no revision. So a fragment that holds an edit and also spans a removal would merge the edit and leave the removed post's body on screen. Note 1 suggested merging fragments and refetching only when nothing in the fragment explains the change. That cannot tell "an edit and a removal" from "an edit". Every hint therefore asks for the page on screen again through `loadThreadPage`. The response cache answers it (docs/07 "The response cache"), and `storeThreadPage` already replaces the entry for a longer log. Preact's keyed diff keeps the DOM nodes of unchanged posts. This supersedes note 1's merge.
- **The cache rule in `state/threads.ts` holds** unchanged: one log length per entry, and a longer page replaces the entry and drops the thread's other held pages.
- **Bundle.**
  - This is reader code: it runs on the thread page from its first render.
  - `scripts/bundle.mjs` counts every dynamic import outside `src/screens/admin/`, so a lazy chunk would save nothing against the budget.
  - The estimate is about 1.2 KB gzip: `live.ts` about 0.7 KB, and the pill, the `knownCount` logic and the CSS about 0.5 KB.
  - The reader is 60465 B of 70000 B on main. If this pushes another lane's planned work over budget, that is a docs/14 class 5 stop, not a reason to raise the budget.
- **Conventions.**
  - e2e tests use stored sessions and one axe scan per test. This spec makes no `/auth/*` call, so `e2e/session.ts` needs no `paceAuth` row.
  - There is no new lazy chunk, so `testing.tsx`'s `lazyChunks` table is unchanged.
  - The pill is not a modal, so no `useModal`.
  - The built client is committed: run `go generate ./internal/web/spa`.

### Approach

Revised at claim, 2026-10-01, against main at c403e67 (after the thread head #303, the lens framework #299, the unread and author lenses #304 and #310, `last_read_at` #314 and the layout-effect menus #292). The settled items above are carried unchanged. Where the build departs from the groomed approach, the departure and its reason are given.

1. **`src/state/live.ts`** (new): `subscribe(id, held: () => number, refresh: () => void): () => void`.
   - It opens `new EventSource('/events?threads=' + id)`, read from `globalThis` when it opens, so a test stubs the global. No `EventSource` at all goes straight to polling.
   - `ready` stops any polling. A `thread` event for this thread whose `log_length` is longer than `held()` calls `refresh()`.
   - An `error`, or no `ready` within 10 s, closes the source (the browser's own reconnect is not used) and starts a 30 s `setTimeout` chain that calls `refresh()`. Every fourth poll opens a new source, and its `ready` stops the polling.
   - A tab hidden for 60 s closes its source and stops polling. Shown again with nothing open, it opens a new source, whose snapshot event catches it up.
   - Departure: the groomed `poll: () => Promise<number>` and the `liveState` signal are dropped. The poll and the hint both ask for the same page, so one `refresh` serves both, and the tests read the fake sources and the requests instead of a state signal.
2. **`src/state/threads.ts`**: unchanged except the `loadThreadPage` comment, which now says why the live client asks for whole pages.
3. **`src/screens/ThreadScreen.tsx`**:
   - **`useLive`** subscribes on the canonical view of a thread the reader has been shown (`res.data` or the thread's cache entry), so moving between the thread's pages keeps one stream. It closes on leaving the thread, and when the thread leaves the cache (access withdrawn).
   - **A hint or a poll** calls `loadThreadPage(id, { page: <number on screen> })`. A landing request names no page and is not repeated, because the server would land it on the reader's moved position. The answer is kept as `live` state, keyed on the request key and the viewer, and shown in place of `res.data` while its log is longer. An answer that withdraws access calls `res.retry()`, so the screen shows the refusal.
   - **The divider** is drawn from the request's own viewer block (`read`), not the refetched page's, so it does not move under a reader whose position has moved since.
   - **`known`** is a ref in `ThreadBody`, which is keyed on the request, holding `header.post_count` when the page opened. Departure: not a per-thread signal; a ref in the keyed body is taken afresh on every page open, the reply's permalink included, which is what "Own replies" needs. While the page's own request is still out (a held copy on screen), `known` follows the count, so a revalidation does not hold back posts that existed when the reader opened the page.
   - **The pill's count** is `post_count - known`, floored at 0. It is 0 while the reader's own post is in flight (`composerFor(id).posting`), so a hint for their own reply that lands before the POST answers never shows a pill.
   - **The body** draws posts with `ordinal <= known`. **Bottom tracking** is an `IntersectionObserver` on an empty element after the list, with `rootMargin: '0px 0px 48px 0px'`. At the bottom, with every held post on this page, `known` follows the count, so posts stream in, and held posts are drawn when the reader scrolls down to them.
   - **Own replies keep today's behaviour**, as settled: `posted` routes to `permalink(thread, res.ordinal)`, a page open.
   - **Scroll anchoring.** Before a live page is set, the first post in view and its top are recorded; a layout effect in the body then calls `scrollBy` with the difference.
   - **Lens pages hold no subscription** (run decision recorded in docs/06 "Live updates", 2026-10-01, pending maintainer review): the unread lens is computed from the read position the reader is moving, so asking for it again would take posts away under them, and the pill counts the whole thread by ordinal.
4. **`src/components/thread/NewPostsPill.tsx`** (new): a `<button class="new-posts">` reading "1 new post — show" or "N new posts — show", sticky at the foot of the list, a 44 px target on a phone and above the reply bar there. A visually hidden `aria-live="polite"` paragraph announces "N new posts" a second after the count last changed. Show with every held post on the page raises `known` and focuses the first revealed post (posts already carry `tabIndex=-1`); otherwise it routes to `permalink(thread, known + 1)`. Styles in `components/thread/thread.css`.
5. **Read tracking.** Held posts are not drawn, so `trackPosts` never observes them, and the tracking effect runs again when more are drawn. A test asserts both.
6. **`src/testing.tsx`** gains `FakeEventSource`, installed by `renderAt`. It says `ready` once opened unless a test sets `FakeEventSource.ready = false`, so screens that are not about live updates never fall back to polling.
7. **Build.** `go generate ./internal/web/spa`, and the reader size against 60823 B on main at claim (the groomed 60465 B was main's at grooming).

### Tests

- `src/state/live.test.ts` (fake timers): one stream opened and closed; a hint longer than the held length refreshes and one no longer does nothing; polling when ready is 10 s late; polling on an error; polling with no EventSource; a retry every fourth poll; polling stops on ready; a ready stream is not dropped at 10 s; a hidden tab closes after 60 s and reopens when shown; a tab shown again within 60 s keeps its stream; a hidden tab stops polling; closing leaves no stream and no timer.
- `src/screens/ThreadScreen.test.tsx`, "live updates on the thread screen": the groomed cases ('new posts wait behind the pill for a reader above the bottom', 'a reader at the bottom gets posts inline with no pill', 'show reveals the posts and focuses the first', 'the pill on an earlier page counts only posts that arrived after it opened, and goes to the first of them', 'a full last page sends the pill to the next page', 'a reply from a full page goes to its own page', 'an edit and a removal in one change both show', 'a post behind the pill is not marked read', "the reader's own reply never counts in the pill", 'posting a reply with posts held takes the reader to the reply', 'an edit applies in place without moving the post in view', 'leaving the thread closes the stream'), and 'posts held while above the bottom are drawn when the reader reaches it', 'a hint for the reply while it is out shows no pill', 'a hint asks for the page on screen by its number, never a fragment', 'polls the page on screen when the stream is refused', 'a held copy shown while it is asked for again holds back nothing it brings', 'the divider stays where it was drawn while the reader reads on', 'a lens page holds no stream'.
- `e2e/live.spec.ts`, both projects, from stored sessions, one axe scan per test: 'a new post waits behind the pill', 'a reader at the bottom sees the post arrive', 'polling takes over when the stream is refused'. alice starts a thread in site-feedback, which no other spec reads, and the reply comes from carol's stored session (the seed has no bob).

### Out of scope

- The server endpoint: TKT-01M29CRJ3X.
- Live updates on forum and thread lists: no Phase 3 screen asks for them.
- Throttling the pill in a busy agent thread: docs/06 leaves it "unsolved" on purpose.
- The non-JS client's refresh link: Phase 5.

## Notes

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T12:53:34Z

From TKT-01M29CEE5H (fetch layer). `state/threads.ts`'s loadThreadPage refuses nothing at runtime, but it is only for full pages: storing an after_log_length answer through it would replace a page with a fragment, because a new log_length replaces the whole entry. Add a merge that applies the fragment to the held page (threadCache entry, pages keyed by pageKey) and advances logLength, and route SSE refetches through it. A delete/restore moves log_length with no post to account for it (docs/07); refetch the full page through loadThreadPage then.

**agent:claude-code/phase3-groom-live** at 2026-10-01T07:59:38Z

run decision: pending maintainer review. Recorded in docs/06 'Live updates' (2026-10-01). 'At the very bottom' means the last post's end is in view, within 48 px. The pill counts new posts only, never edits and never your own. Show reveals in place on the last page and navigates from an earlier page. Edits, removals and restores apply at once with the scroll anchored. Polling every 30 s takes over on an error, or when ready is 10 s late, and retries the stream every fourth poll. A tab hidden for 60 s closes its stream. Alternatives and why they lost are in the doc.

**agent:claude-code/phase3-groom-live** at 2026-10-01T08:13:52Z

run decision: pending maintainer review. Amends note 2 after Terva review 1696 on PR #271. This supersedes note 1's fragment merge: every hint refetches the page on screen in full. A removal or restore writes no revision, so a fragment that also held an edit would leave the removed post visible. The pill counts header.post_count less the count when the page opened (or the pill was last taken), so posts that already existed on later pages never count. docs/06 'Live updates' is updated to match.

**agent:claude-code/phase3-groom-live** at 2026-10-01T08:58:40Z

run decision: pending maintainer review. Amends notes 2 and 3 after Terva reviews 1703 and 1716 on PR #271. Posting a reply keeps today's behaviour: ThreadScreen's posted handler routes to the reply's permalink (TKT-01M3ART992). That is a page open, so the page holding the reply loads afresh, held posts appear above it, and the reader is placed at the reply. A reply from a full or earlier page lands on its own page the same way. The ownPosted set from the review-1703 fix is dropped: it could not render a reply that falls on another page (review 1716). docs/06 'Live updates' is updated to match.

**agent:claude-code/phase3-ctya0** at 2026-10-01T14:41:59Z

run decision: pending maintainer review. A lens page holds no live subscription; live updates run on the canonical view. The unread lens is computed from the read position, which moves while the reader reads, so asking for its page again on a hint would take posts away under them, and the pill counts and reveals the whole thread's posts by ordinal, which a filtered page does not hold in order. Alternatives that lost: refetching every lens but unread (live behaviour would differ by lens, and the pill would count posts the lens hides); a pill on lens pages linking to the full thread (needs the post count, which a hint does not carry, so a second request per hint). Recorded in docs/06 'Live updates' in the implementation PR.

**agent:claude-code/phase3-ctya0** at 2026-10-01T15:01:18Z

Terva review 1778 on PR #320 (run abf964df-4c31-4253-bfb1-2a87c430541c, Actions run 2129), head d9ada3a347a9ea272bd7272e1f4c0ba44fbc20f0, base 04224e4912f7b9cca839ffcba119e66a4ecf6b37: three medium findings, all fixed. (1) 'Retain hints received while the destination page is loading': a hint or poll with no page on screen is kept, and the page is asked for again once it arrives (ThreadScreen useLive; test 'a hint while another page of the thread loads is caught up once it arrives'). (2) 'Update screen actions and reply-bar permissions from the live page': can_reply, First unread's counts, can_moderate, rename_until, slow mode and mark-unread now read the page shown; only the divider keeps the request's read position (test 'what the screen offers follows the live page'). (3) 'Exclude the fixed mobile reply bar from bottom detection': thread.css moves the bottom sentinel down by the reply bar's height plus the safe-area inset, so the 48 px allowance is measured from the bar's top (e2e 'a list end behind the reply bar is not the bottom', phone only; it fails with the rule removed).

**agent:claude-code/phase3-ctya0** at 2026-10-01T15:06:10Z

Terva review 1779 on PR #320 (run 95ddbb02-dd0c-473c-a78d-71a6b39a432e, Actions run 2137), head c9b260738492d1d859606aac0982412932dbae34, base 40b9c2ddd0b3113b8bb2fe344f1f37be8b935766: the three review-1778 findings reported resolved; one new medium finding, fixed. 'Retry failed hint refreshes while the stream remains healthy': a live refresh that fails for any reason other than withdrawn access is tried again 30 s (pollMs) later until one answers, and the timer is cleared when the subscription closes (ThreadScreen useLive; test 'a refresh that fails is tried again without another hint', which fails with the retry removed).

**agent:claude-code/phase3-ctya0** at 2026-10-01T15:10:58Z

Terva review 1780 on PR #320 (Actions run for request ready-review-1f0cd4c), head 1f0cd4c46ce59ed38e6df42efe2956e3c6ed7b98, base 40b9c2ddd0b3113b8bb2fe344f1f37be8b935766: review 1779's finding reported resolved; one new medium finding, fixed. 'Suspend failed-refresh retries when the hidden subscription stops': the retry moved from ThreadScreen into state/live.ts, so it shares the subscription's lifecycle. A refresh rejects on a failure other than withdrawn access; while the stream is up, the subscription tries it again 30 s later; while polling, the next poll is the retry; dropping or stopping the stream (a hidden tab's included) clears it, and the new stream's snapshot catches up when the tab is shown. subscribe now answers {close, refresh}, and the screen's catch-up after a page load goes through that refresh. Tests in live.test.ts: 'a failed refresh on a live stream is tried again every 30 s until one answers', 'a hidden tab stops trying a failed refresh, and its new stream catches up when shown', 'a failed refresh while polling waits for the next poll'.

## Summary

Landed: the thread page holds a GET /events subscription on its canonical view (web/client/src/state/live.ts, ThreadScreen useLive). A hint longer than the page on screen asks for that page again in full by its number, and shows it with the first post in view held in place; a hint that comes while a page is loading is caught up once it arrives. Posts that arrived after the page opened wait behind NewPostsPill ('N new posts — show') unless the reader is at the bottom (the end of the list within 48 px of what is visible, above the phone's reply bar) with all of them on the page; show reveals in place and focuses the first, or routes to the permalink of the first. Polling every 30 s on an error or a ready 10 s late, retrying the stream every fourth poll; a tab hidden 60 s closes its stream. The screen's actions read the page shown; the divider stays at the opening read position; held posts are not read; the pill is withheld while the reader's own post is in flight (the post then routes to its permalink as before). Run decision, pending maintainer review: lens pages hold no subscription (docs/06). testing.tsx renderAt installs FakeEventSource (says ready by default). Reader bundle 60823 B at claim (60465 B at grooming) to 62035 B of 70000 B. All ten criteria met; Vitest, Go race and the full Playwright suite ran locally.
