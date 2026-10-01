---
schema: 3
id: TKT-01M29CTYA0E5Y06WWYY2ETBF8V
title: Add the client SSE subscription with the new-posts pill
type: task
status: draft
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
claim: null
archive: null
created_at: 2026-09-11T23:28:54Z
updated_at: 2026-10-01T07:59:38Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase3-groom-live
  name: ""
extensions: {}
---

## Description

While a thread page is open the client holds an SSE subscription. On a hint it fetches the new posts and shows a "3 new posts, show" pill at the bottom rather than inserting them unasked, because inserting them moves the page under the reader. If the reader is already at the very bottom, posts stream in directly.

Polling every 30 seconds is the fallback when SSE is unavailable, which covers proxies that buffer streams.

## Acceptance criteria

- [ ] New posts never shift content under a reader who is not at the bottom: they wait behind the pill and no post element is inserted (ThreadScreen.test.tsx 'new posts wait behind the pill for a reader above the bottom', e2e live.spec.ts 'a new post waits behind the pill')
- [ ] A reader at the bottom receives posts inline with no pill (ThreadScreen.test.tsx 'a reader at the bottom gets posts inline with no pill', e2e 'a reader at the bottom sees the post arrive')
- [ ] Show reveals held posts on the last page and focuses the first, and on an earlier page goes to the page holding the first new post (ThreadScreen.test.tsx)
- [ ] A post behind the pill is not marked read, and the reader's own reply never counts in the pill (ThreadScreen.test.tsx)
- [ ] An edit, removal or restore of a post already shown applies at once without moving the post in view, and a length that moved with no post to show refetches the page through loadThreadPage (ThreadScreen.test.tsx, threads.test.ts)
- [ ] Polling every 30 s takes over when the stream errors or its ready event is 10 s late, retries the stream every fourth poll, and stops when a stream says ready (live.test.ts, e2e 'polling takes over when the stream is refused')
- [ ] The subscription closes on navigation away and after 60 s in a hidden tab, and leaks no connection (live.test.ts, ThreadScreen.test.tsx 'leaving the thread closes the stream')
- [ ] e2e live.spec.ts passes at 390 and 1440 px with one axe scan per test and no violations
- [ ] The dist committed from go generate ./internal/web/spa keeps the reader within the 70000 B gzip budget, and the PR records the change from 60465 B

## Implementation plan

Groomed 2026-10-01 for Phase 3 (TKT-01M3V3BVPY), against main at a599b48. Read for this plan:
- docs/06 "Live updates" and docs/07's `after_log_length` rules ("What the thread endpoint does");
- the server contract in TKT-01M29CRJ3X (Build the SSE endpoint and fan-out) and docs/02 "Live updates";
- note 1 on this ticket;
- in `web/client`: `src/state/threads.ts`, `src/state/readTracker.ts`, `src/screens/ThreadScreen.tsx`, `src/api/client.ts`, `src/testing.tsx`, `scripts/bundle.mjs` and `e2e/session.ts`.

### Settled in grooming

- **Behaviour** is docs/06 "Live updates" and its run decision (2026-10-01, pending maintainer review).
  - "At the very bottom" means the end of the last post is in view, within 48 px.
  - The pill counts new posts, never edits, and never the reader's own replies. "Show" reveals the held posts on the last page and navigates to the first new post from an earlier page.
  - Edits, removals and restores of posts already shown apply at once, with the scroll corrected against the first post in view.
  - Polling every 30 s takes over on a stream error, or when no `ready` arrives within 10 s. The client retries the stream every fourth poll.
  - A tab hidden for 60 s closes its stream.
- **Server contract** (TKT-01M29CRJ3X, docs/02 decision).
  - `EventSource('/events?threads=<id>')` receives `event: ready`, then `event: thread` with `{thread_id, log_length}` for the current state, then hints as they come. It also receives heartbeat comments.
  - A refused subscription is an HTTP error, which `EventSource` treats as fatal. A lost session or lost access closes the stream, and the browser's reconnect is judged afresh.
  - A full limit is a 429, so the client polls.
- **Fragments** (docs/07). `?after_log_length=L` answers the requested page's posts whose current revision was written at or after L, with a complete header and viewer block. A delete or restore writes no revision, so a length that moved with no post to account for it means refetching the page through `loadThreadPage` (note 1).
- **The cache rule in `state/threads.ts` holds.** An entry has one log length. A merged fragment advances it, and the thread's other pages, which are from the old length, are dropped with their held GETs, as `storeThreadPage` drops them for a longer page.
- **Bundle.**
  - This is reader code: it runs on the thread page from its first render.
  - `scripts/bundle.mjs` counts every dynamic import outside `src/screens/admin/`, so a lazy chunk would save nothing against the budget.
  - The estimate is about 1.5 KB gzip: `live.ts` about 0.7 KB, the fragment merge about 0.4 KB, and the pill and its CSS about 0.4 KB.
  - The reader is 60465 B of 70000 B on main. If this pushes another lane's planned work over budget, that is a docs/14 class 5 stop, not a reason to raise the budget.
- **Conventions.**
  - e2e tests use stored sessions and one axe scan per test. This spec makes no `/auth/*` call, so `e2e/session.ts` needs no `paceAuth` row.
  - There is no new lazy chunk, so `testing.tsx`'s `lazyChunks` table is unchanged.
  - The pill is not a modal, so no `useModal`.
  - The built client is committed: run `go generate ./internal/web/spa`.

### Approach

1. **`src/state/live.ts`** (new): `subscribe(id, onHint: (logLength: number) => void, poll: () => Promise<number>): () => void`.
   - It opens `new EventSource('/events?threads=' + id)`. The constructor comes from `liveDeps.EventSource` so tests can replace it.
   - The states are `connecting`, `live` and `polling`, exported as the signal `liveState` for tests.
   - `ready` moves it to `live`. Each `thread` event calls `onHint(log_length)`.
   - An `error` event, or no `ready` within 10 s, closes the source and moves it to `polling`. A `setTimeout` chain runs every 30 s and calls `poll()`, which answers the current log length through a conditional GET of the page on screen; a 304 is cheap. A longer length calls `onHint`. Every fourth poll opens a new source, and a `ready` from it stops the polling.
   - On `visibilitychange`, a tab hidden for 60 s closes its source and stops polling. Showing the tab again resubscribes, and the snapshot catches it up.
   - The returned function closes everything, and nothing stays open after it.
2. **`src/state/threads.ts`**:
   - `fetchFragment(id, q, after)` asks `threadUrl(id, q) + '&after_log_length=' + after` through `apiGet`, then drops the held body with `forgetHeld`, since a fragment must never be served as a page.
   - `applyThreadFragment(id, key, fragment): 'merged' | 'refetch' | 'stale'`.
     - It returns `stale` when `fragment.thread.log_length` is no longer than the entry's.
     - It returns `refetch` when the length moved but the fragment holds no post with a new ordinal or a newer revision than the held page. The caller then calls `loadThreadPage`.
     - Otherwise it merges posts by ordinal into the held page under `key`, takes the fragment's header and viewer, sets `logLength`, drops the other pages and their held GETs, and returns `merged`.
   - The comment on `loadThreadPage` that defers fragments to this ticket is replaced by a pointer to `applyThreadFragment`.
3. **`src/screens/ThreadScreen.tsx`**:
   - **`useLive(id, page)`** subscribes while the screen shows the thread and closes on unmount or on a change of thread. A hint longer than the entry's length fetches a fragment of the page on screen and applies it.
   - **`revealedThrough`** is a per-thread signal. It is set to the highest ordinal on the page when the page first renders, and `ThreadBody` renders only posts with `ordinal <= revealedThrough`.
   - **Bottom tracking.** An `IntersectionObserver` on a sentinel after the last post, with `rootMargin: '0px 0px 48px 0px'`, tracks "at the bottom". While the reader is there, `revealedThrough` follows the newest ordinal, so posts stream in.
   - **Own replies.** The composer's `onPosted` raises `revealedThrough` to the new ordinal.
   - **Scroll anchoring.** When a merge changes posts already shown, the screen records the first post in view and its `getBoundingClientRect().top` before the update. In a layout effect after it, the screen calls `scrollBy` with the difference.
4. **`src/components/thread/NewPostsPill.tsx`** (new):
   - A `<button>` reading "1 new post — show" or "N new posts — show", sticky at the bottom of the post list. On a phone it sits above the reply bar with `env(safe-area-inset-bottom)` padding, as a 44 px target.
   - A visually hidden `aria-live="polite"` region beside it announces the count, at most once a second.
   - On the last page, a click raises `revealedThrough` and focuses the first revealed post, which gets `tabIndex=-1`. On an earlier page, where `header.post_count` exceeds the page's last ordinal, it routes to `?ordinal=<first new>`.
   - The styles go in `components/thread/thread.css`.
5. **Read tracking.** Posts beyond `revealedThrough` are not rendered, so `trackPosts` never sees them. A test asserts this rather than assuming it.
6. **`src/testing.tsx`** gains a `FakeEventSource`, installed by `renderAt`, which records instances and has `emit(type, data)` and `fail()`.
7. **Build.** Run `go generate ./internal/web/spa`, commit `internal/web/spa/dist`, and record the reader size against 60465 B in the PR.

### Tests

- `src/state/live.test.ts` (fake timers):
  - 'opens one stream for the thread and closes it on close()';
  - 'falls back to polling when ready does not arrive in 10 s';
  - 'falls back to polling on a stream error';
  - 'polls every 30 s and stops when a stream says ready';
  - 'retries the stream every fourth poll';
  - 'a hidden tab closes its stream after 60 s and reopens when shown';
  - 'a hint no longer than the held length does nothing'.
- `src/state/threads.test.ts`:
  - 'a fragment merges into its page and advances the log length';
  - 'a fragment drops the thread's other pages';
  - 'a length that moved with no post to show asks for a refetch';
  - 'a fragment older than the entry is stale';
  - 'a fragment is never held as a page'.
- `src/screens/ThreadScreen.test.tsx`:
  - 'new posts wait behind the pill for a reader above the bottom' asserts the post elements above are the same nodes in the same order and that no new post element is rendered;
  - 'a reader at the bottom gets posts inline with no pill';
  - 'show reveals the posts and focuses the first';
  - 'the pill on an earlier page goes to the page with the first new post';
  - 'a post behind the pill is not marked read';
  - 'the reader's own reply never counts in the pill';
  - 'an edit applies in place without moving the post in view';
  - 'leaving the thread closes the stream', where `FakeEventSource` reports no open instance after navigating away.
- `e2e/live.spec.ts`, in both projects (390 and 1440 px), from stored sessions:
  - 'a new post waits behind the pill': alice opens the seeded thread at its top, a post is made through bob's stored session with `request.post`, and the pill reads "1 new post — show" while the first post's position is unchanged. This test runs the one axe scan.
  - 'a reader at the bottom sees the post arrive'.
  - 'polling takes over when the stream is refused', using `page.route('**/events*', r => r.abort())` and `page.clock` to pass 30 s.

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
