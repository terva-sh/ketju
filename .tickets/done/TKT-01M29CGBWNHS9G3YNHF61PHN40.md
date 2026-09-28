---
schema: 3
id: TKT-01M29CGBWNHS9G3YNHF61PHN40
title: Implement the read-tracking beacon
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
  - TKT-01M29CA9CECTE02DFZGBGN2HC2
blocks_on: none
references:
  - ref: design:read-tracking
    path: docs/06-thread-views.md
  - ref: pr:101
    path: null
claim: null
archive: null
created_at: 2026-09-11T23:23:08Z
updated_at: 2026-09-28T07:06:13Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase1-run-438e26c1
  name: ""
extensions: {}
---

## Description

A post counts as read once it has been on screen for at least a second. Positions batch every 30 seconds and flush on navigation and unload through `navigator.sendBeacon`.

The one-second dwell is what stops a fast scroll to the bottom from marking an entire thread read. The same beacon works on the phone layout, which is deliberate: read tracking is the same integer with the same semantics everywhere, including the coarser page-load marking the non-JS client will do.

## Acceptance criteria

- [x] A post scrolled past faster than a second is not marked read
- [x] Batching never sends a position behind one already sent in the same session
- [x] The behavior is identical on the phone layout
- [x] Positions flush on navigation and on page hide through fetch with keepalive, and a test covers both

## Implementation plan

Revised at claim (2026-09-28), from the code on main at 9d7b0dc: PUT/DELETE /threads/{id}/read landed in CA9CE (#99), the keyboard shortcuts in CGBX4 (#98), and reply copies without data-ordinal in CGBW6 (#96).

### Approach
1. `web/client/src/state/readTracker.ts` owns the client's read state for the page session: per thread, `pending` (highest ordinal that has dwelled and not gone out) and `floor` (highest ordinal sent, answered, or known stored). A dwelled ordinal at or below either is dropped, so nothing behind a position already sent goes out (AC2). The floor is seeded from the page's `viewer.last_read_ordinal`, so a page already read sends nothing.
2. `trackPosts(threadId, list, stored)` observes the direct children of `section.posts` whose id starts `post-` (posts and tombstones) with one `IntersectionObserver` on the viewport. Reply copies are nested inside a post and carry no `data-ordinal`, so they are never observed. A post is on screen when at least half of it is in the viewport, or it fills at least half the viewport (a post taller than two screens). Entering starts a 1000 ms timer; leaving cancels it; the timer firing raises `pending` (AC1). No layout branch: phone and desktop run the same code (AC3).
3. While the document is hidden no timer runs; timers restart for the posts still on screen when it is shown again.
4. Transport: `apiSend('PUT', ..., {ordinal}, {keepalive: true})` — apiSend gains a `keepalive` option. Flush every 30 s while a thread is tracked; on navigation, when the thread body unmounts (every route change away from the page, including to another page of the thread, because the body is keyed on the request); and on `pagehide` and `visibilitychange` to hidden (AC4).
5. Answers: the stored position raises the floor. A network, rate-limited, internal or unavailable failure puts the ordinal back for the next flush, unless a later one went out meanwhile; any other refusal drops it. After an answer the held landing-page bodies of that thread (no page, no ordinal) are forgotten, so returning lands where the server now says rather than on a stale held page. The page on screen is not re-rendered: the divider does not move under the reader.
6. `markUnread(threadId, ordinal?)` holds docs/07's ordering rule for the future mark-unread control: flush the thread's pending beacon and wait for every PUT in flight, send nothing more for it until the DELETE answers, then set the floor to the answer.
7. Only a signed-in viewer is tracked (`viewer.user`).

### e2e isolation
Specs that assert on a seeded read position set it themselves at the start of the test through the API (DELETE then PUT, e2e/session.ts `setReadPosition`), instead of relying on no earlier spec having moved it: shortcuts (alice, Minutes, 30), thread-list (alice, Minutes, 30) and smoke (carol, main thread, 60). The beacon spec uses carol on Minutes, which no other spec asserts on, and resets it at the start of each test.

### Files
`src/state/readTracker.ts` (+ test), `src/api/client.ts` (keepalive), `src/state/threads.ts` (forget landing bodies), `src/screens/ThreadScreen.tsx` (track the list), `e2e/read-tracking.spec.ts`, `e2e/session.ts`, `e2e/{shortcuts,thread-list,smoke}.spec.ts`, regenerated `internal/web/spa/dist`.

### Tests
Vitest with fake timers, a stubbed IntersectionObserver and a stubbed fetch: AC1 (900 ms then out sends nothing; 1100 ms held is sent), AC2 (a lower dwell after a send sends nothing; a failed send is retried at the same ordinal, never lower), AC4 (route-change unmount and pagehide/visibilitychange each send one keepalive PUT with the max ordinal), reply copies not observed, hidden tab pauses the dwell, markUnread ordering. Playwright at 390 and 1440 px: a fast scroll to the foot and back marks only what stayed on screen; an in-app navigation flushes; a full navigation (pagehide) flushes; a lower read after a send sends nothing.

### Design
docs/06 "Read tracking" and "Mobile"; docs/07 "Read position" and "CSRF posture" rule 1.

### Settled in grooming (2026-09-24)

- Flush with fetch(..., {method:'PUT', keepalive:true}), not sendBeacon (criterion rewritten in grooming).

## Notes

**agent:claude/phase1-groom** at 2026-09-24T05:17:38Z

Criterion rewritten during Phase 1 grooming (2026-09-24). It moves to the end of the list. sendBeacon can only POST, while docs/07 specifies PUT /threads/{id}/read and the API accepts only JSON on mutating routes. fetch with keepalive does the same job without adding a second route.

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T06:58:49Z

Decisions taken while building, with the alternatives:

- "Session" for criterion 2 is the page's life. The floor per thread is the highest position sent, answered, or reported by the page (viewer.last_read_ordinal), kept in memory. The pre-groom plan kept it in sessionStorage as well; that lost, because after a reload the page reports the stored position anyway, and a stored floor would outlive a mark-unread made on another device and silently stop the beacon there.
- Flush on navigation runs from the thread body's unmount rather than the router's onRouteChange. The body is keyed on the request, so it unmounts on every move away from its page (another page of the thread included), and the tracker's cleanup is in one place with the observer it owns.
- On screen: at least half the post in the viewport, or half the viewport filled by the post (a post taller than two screens is never half visible). No rootMargin for the phone bars: the rule is the same number at every width, which is criterion 3, and the list's bottom padding already keeps the last post clear of the reply bar (docs/06 "Mobile"). Written into docs/06 "Read tracking" in this PR.
- Every view of the thread is tracked, lenses included; docs/06 gives the non-JS client the same rule (highest ordinal rendered), with no lens exception.
- markUnread is in the tracker although no control calls it yet: docs/07 makes flushing before a DELETE the client's job, and the tracker is the only place that knows what is pending and in flight.
- e2e isolation: a spec that asserts on a seeded position sets it through the API at its start (DELETE then PUT), instead of each beacon-affected spec avoiding the thread. That keeps the seed as it is and holds whatever a future spec reads. The thread-list badge assertion is now exact (31) rather than at least 31.

Criterion 3 is verified in Playwright's 390 px phone emulation; real devices stay with TKT-01M38WTKSQ (Verify the Phase 1 client on real iOS and Android devices).
Playwright does not report a request sent as the page unloads, so the page-hide e2e test reads the stored position; the unit tests assert keepalive and the PUT on pagehide and on visibilitychange.

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T07:06:13Z

Review 1143 (request ready-review-c4bc551, Actions run 550; head c4bc55130002c7b0faa4d9d7773c70aa544899af, base 9d7b0dca08083d4ecc937ad53ac7d8068956801a) failed the gate with one high and two medium findings. All three were accepted and fixed in ac90671, by changing the behaviour flagged: markUnread now holds the thread before it waits and drains every PUT (high); a post's geometry is measured again when its second is up, so a tall post that slipped below the line between observer thresholds is not counted (medium); the batch timer keeps running while a position is waiting, so a navigation PUT that failed after the last thread page closed is retried (medium). A fresh review of the new head follows.

## Summary

The beacon is in web/client/src/state/readTracker.ts, wired from ThreadScreen's thread body: one second on screen counts, the highest ordinal goes out by PUT with fetch keepalive every 30 s, on navigation and on page hide, and nothing at or below a position already sent or known goes out. markUnread holds the flush-before-DELETE rule for a future control. e2e specs that depend on seeded positions now set them per test; read-tracking.spec.ts covers the beacon at 390 and 1440 px.
