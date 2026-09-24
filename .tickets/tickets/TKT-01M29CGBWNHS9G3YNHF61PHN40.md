---
schema: 3
id: TKT-01M29CGBWNHS9G3YNHF61PHN40
title: Implement the read-tracking beacon
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
  - TKT-01M29CEE6V358M5KCBJGHD58HM
  - TKT-01M29CA9CECTE02DFZGBGN2HC2
blocks_on: none
references:
  - ref: design:read-tracking
    path: docs/06-thread-views.md
claim: null
archive: null
created_at: 2026-09-11T23:23:08Z
updated_at: 2026-09-24T05:21:21Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude/phase1-groom
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

Pre-groom plan (2026-09-24). Revise at claim, after reading the code as it stands then.

### Approach
1. `state/readTracker.ts` uses one `IntersectionObserver` (threshold 0.5, rootMargin set to exclude the phone ReplyBar height) over every canonical `article[id^=post-]`. Expanded read-only replies from CGBW6 are not observed.
2. Dwell: on entry, start a 1000 ms timer per ordinal; on exit, cancel it. When the timer fires, raise `pending[threadId] = max(pending, ordinal)`.
3. Batching: every 30 s, send `pending` for each thread whose value exceeds `sent[threadId]`, then set `sent`. Keep `sent` in a session-scoped Map (and in `sessionStorage` so it survives a reload), so a value at or below one already sent is never transmitted (AC3).
4. Flush on navigation: call `flush()` from the router's route-change hook before the thread page unmounts. Flush on unload: listen for `visibilitychange` to hidden and `pagehide` (more reliable on mobile than `unload`).
5. Transport: docs/06 names `navigator.sendBeacon`, but sendBeacon can only POST, while docs/07 specifies `PUT /threads/{id}/read {ordinal}` and the CSRF posture accepts JSON only. The plan is `fetch(url, {method: 'PUT', keepalive: true, headers: {'Content-Type': 'application/json'}, body})` for both batch and unload, with sendBeacon used only if the API lane adds a POST alias. Settle this with CA9CE before claiming (interfaces item 8).
6. Only signed-in viewers: skip everything when `!isSignedIn`. After a successful flush, advance `currentThread.viewer.last_read_ordinal` locally, so returning within the session draws the divider correctly. Do not move the divider on the page currently open.

### Files
`web/client/src/state/readTracker.ts`, `src/screens/ThreadPage.tsx` (register posts), `src/app.tsx` (route-change flush).

### Tests (Vitest with fake timers and a stubbed IntersectionObserver)
- AC1: enter at t=0 and exit at 900 ms, then advance 30 s; assert no request. Enter and hold 1100 ms; assert the ordinal is pending.
- AC2: stub `fetch` (or `navigator.sendBeacon`, whichever transport is chosen) and assert one call on a simulated route change and one on `pagehide` / `visibilitychange`, each with the max ordinal. Playwright adds an end-to-end check with `page.route` on `/api/v1/threads/*/read`, triggering navigation and `page.close({runBeforeUnload: true})`.
- AC3: send 40, then let 35 dwell. Assert no second request, and that `sent` stays 40 across a simulated reload.
- AC4: run the Playwright case at 390 and 1440 px and assert the same request sequence. The module has no `isPhone` branch; check that with a grep.

### Design
docs/06 "Read tracking" and "Mobile" (read tracking bullet); docs/07 `PUT /threads/{id}/read` and "CSRF posture" rule 1; "Agents on the API" (no beacon for agents, which is not relevant here).

### Interactions
Depends on CEE6V and CA9CE (the server moves the position forward only). CEE52 provides the route-change hook. CGBY1's "unread divider" step depends on positions persisting.
Contradiction: the AC's "tested with sendBeacon" does not fit docs/07's PUT plus JSON-only rule; see step 5.

### Settled in grooming (2026-09-24)

- Flush with fetch(..., {method:'PUT', keepalive:true}), not sendBeacon (criterion rewritten in grooming).

## Notes

**agent:claude/phase1-groom** at 2026-09-24T05:17:38Z

Criterion rewritten during Phase 1 grooming (2026-09-24). It moves to the end of the list. sendBeacon can only POST, while docs/07 specifies PUT /threads/{id}/read and the API accepts only JSON on mutating routes. fetch with keepalive does the same job without adding a second route.
