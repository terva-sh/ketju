---
schema: 3
id: TKT-01M29CGBWNHS9G3YNHF61PHN40
title: Implement the read-tracking beacon
type: task
status: in-progress
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
claim:
  actor: agent:claude-code/phase1-run-438e26c1
  branch: run/cgbwn-read-beacon
  worktree: /home/sothr/workspace/git.local.sothr.com/terva-sh/ketju/.claude/worktrees/agent-a7e792866ff0ef41c
  commit: 9d7b0dca08083d4ecc937ad53ac7d8068956801a
  session: null
  claimed_at: 2026-09-28T06:42:52Z
  expires_at: null
archive: null
created_at: 2026-09-11T23:23:08Z
updated_at: 2026-09-28T06:47:55Z
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

- [ ] A post scrolled past faster than a second is not marked read
- [ ] Batching never sends a position behind one already sent in the same session
- [ ] The behavior is identical on the phone layout
- [ ] Positions flush on navigation and on page hide through fetch with keepalive, and a test covers both

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
