---
schema: 3
id: TKT-01M3XP6ZRZQ968B8QPZ1RJBEGM
title: Show a thread as read on Unread after going back from reading it
type: bug
status: done
status_reason: null
priority: high
due_on: null
labels:
  - area:client
assignees: []
milestone: phase-3
parent: TKT-01M29BN36D34YVKVRPNQD3PHDV
origin: null
dependencies: []
blocks_on: none
references:
  - ref: pr:350
    path: null
claim: null
archive: null
created_at: 2026-10-02T06:53:16Z
updated_at: 2026-10-02T08:45:32Z
created_by:
  id: agent:claude-code/phase3-exit-check
  name: ""
updated_by:
  id: agent:claude-code/phase3-unread-race
  name: ""
extensions: {}
---

## Description

Found by the Phase 3 exit spec (TKT-01M3V3CFRE, `e2e/phase3-exit.spec.ts`, the Unread surface test) at 1440 px, in the full suite run. The test passed twice when run alone, which fits a race.

### What happens
1. alice opens /unread and sees carol's thread at "2 new". She presses the badge, which lands on #2.
2. #2 and #3 are both on screen past the one-second dwell.
3. She presses Back to /unread.
4. The stored position becomes 3, so the server has it. The list on screen still shows the thread with "2 new", and it stays that way: nothing asks for the list again.

Snapshot from the failure:

    link "Unread surface desktop 1790923939446" … link "2 new"

### Why
Leaving the thread page flushes the read beacon (`state/readTracker.ts`, a keepalive PUT). The list screen revalidates its held answer (`api/client.ts` conditionalGet) at the same moment. When the list GET reaches the server before the PUT, the answer is the old one. Nothing drops or refetches the held list once the PUT answers: `forgetLists` runs after a mark all read and a mark-unread, but not after a read beacon lands.

### Expected
A member who reads a thread and goes back to Unread sees it gone, or at least its badge cleared, without reloading. That is docs/14's "daily-driver quality". One fix is to have the list wait for an in-flight flush before it revalidates. Another is to forget the held lists, and refetch the one on screen, when a beacon answers with a higher position.

### Owners
TKT-01M29CTY7K (Build the cross-forum unread, new, participated, and watching lists) and the read tracker (`state/readTracker.ts`).

### Why phase-3
It fails Phase 3's exit criterion 1 (TKT-01M3V3CFRE). The exit spec is not changed to avoid it: no reload is added and no wait on the beacon.

## Implementation plan

### Chosen fix: forget the lists when a beacon moves the position

When a read beacon's PUT answers with a position above the floor it was
sent over, the read tracker drops every held list and supersedes any list
GET still out (`forgetLists`, as after a mark-unread), and bumps the
`marks` signal so the list on screen asks for itself again (`useAfterMark`,
as after a mark all read). The bump aborts the list request that was out,
so its answer, from before the PUT landed, is never shown.

### Why the other fix lost

Making the list wait for an in-flight flush before it revalidates was
rejected for four reasons:
- it covers only a PUT already in flight when the list asks. A PUT that
  failed as the reader left is retried by the batch timer from whatever
  screen they went to, and that retry would land with nothing to refresh the
  list;
- it ties every list fetch to the read tracker's promise. A slow or hung
  keepalive PUT would hold the list's revalidation with it, which is the
  "a refused or slow request blocks a later one" trap from #341 and #342;
- it orders requests by when they were sent, not by when the server
  stored them. The list GET would still need the PUT's answer to know the
  write had landed, so it would be waiting on the same event the chosen fix
  reacts to;
- the list screens would each need the wait, while the chosen fix lives in
  the one place that knows a position moved.

### Steps
1. `state/lists.ts`: add `readLanded()`, which runs `forgetLists()` and
   bumps `marks`. Reword `marks` and `useAfterMark` to cover the beacon.
2. `state/readTracker.ts`: in `send`'s success, call `readLanded()` when
   `last_read_ordinal` is above the floor before the send. A failed PUT
   calls nothing. Its retry does, when it lands.
3. Tests that fail before the fix:
   - `CrossListScreen.test.tsx`: read on a thread page, go back to /unread
     with the PUT held and the list GET answered first. The list must refresh
     once the PUT answers. A list GET held past the PUT's answer must not
     overwrite the fresh list when it lands late. A refused PUT changes
     nothing, and its successful retry refreshes the list.
   - `readTracker.test.ts`: the held lists are dropped, and the `marks`
     signal moves, only when a PUT answers with a higher position.
4. Prove the Phase 3 exit spec's Unread-surface test at 1440 px inside the
   full Playwright suite, several runs, with the spec borrowed uncommitted
   from `run/cfre-phase3-exit`.
5. tsc, vitest, `go test -race ./...`, the full Playwright suite, and the
   reader bundle bytes measured only.

## Notes

**agent:claude-code/phase3-unread-race** at 2026-10-02T07:44:07Z

Phase A (prepare) done on the unpushed branch run/p6zrz-unread-race, fix commit 6481e855, from origin/main 628f1a6b. The dist is not committed yet. Another lane holds it.

### Fix
A read beacon's PUT that answers successfully now calls `readLanded()` (state/lists.ts). That runs `forgetLists()`, which drops the held lists and supersedes any list GET still out, and bumps `marks`, so the list on screen asks again through `useAfterMark`. Asking again aborts the request that was out, so an answer from before the PUT is neither shown nor held.

A PUT goes out only above the floor, and the server keeps the larger position, so every successful answer is a higher position. The tracker therefore calls `readLanded()` on every successful PUT and does not compare positions. A failed PUT calls nothing. Its retry calls it when the retry lands.

### Tests (all five fail with the call removed and pass with it)
- CrossListScreen.test.tsx, "going back to a list while the read beacon is out":
  - the list answers before the held PUT and is asked for again once the PUT lands;
  - a list GET held past the PUT's answer does not overwrite the fresh list, and is not held;
  - a refused PUT asks for nothing, and its retry refreshes the list.
- readTracker.test.ts: the held lists are dropped and `marks` moves when a PUT lands, not when it fails, and they are dropped once the retry lands.

### Checks
- tsc is clean. vitest: 2553 passed.
- `go test -race ./...` passed.
- Playwright full suite without borrowed files: 258 passed, 18 skipped (the width-specific tests).
- Exit spec e2e/phase3-exit.spec.ts and the cmd/ketju/e2e.go seed, borrowed uncommitted from run/cfre-phase3-exit, full suite three times on port 8795. The Unread surface test passed at 390 and 1440 px in all three runs (282 passed each time). The one failure in every run was the desktop Alerts test's axe `link-in-text-block` on `.alert-thread.hit`. That is TKT-01M3XNQ772 (Tell an unread alert's thread link apart by more than colour) and not this ticket. I did not run the exit spec without the fix in this session. The exit check's failure, and its passing twice alone, are the evidence for the race.

### Reader bundle (measured, not committed)
79192 B on main, 79196 B with the fix (+4 B), of the 120000 B budget.

### Run decisions
None. The fix is how the client keeps its lists current, and it changes nothing a member is offered.

### Not done here
A landed beacon does not ask for /me again. The forum index's summary line (`me.unread.threads`) therefore waits for the next /me, as it did before this change. The ticket asks only for the lists, so I left the summary alone.

## Summary

A read beacon's PUT that lands now drops every held list, keeps any list GET still out from being stored, and makes the list on screen ask for itself again (`readLanded()` in state/lists.ts, called from `send` in state/readTracker.ts). Asking again aborts the stale request, so a list answered before the PUT landed is neither shown nor stored. A failed PUT asks for nothing, and its retry does once it lands. I chose this over making the list wait for an in-flight flush. The plan gives the reasons.

Five regression tests fail without the fix: three app-level tests in CrossListScreen.test.tsx and two in readTracker.test.ts. The Phase 3 exit spec's Unread surface test, borrowed uncommitted, passed at 390 and 1440 px in three full-suite runs. On the rebased branch, tsc, vitest (2553), `go test -race ./...` and the full Playwright suite (260 passed) are green.

Reader bundle: 79205 B on main 78a84635, 79213 B with the fix (+8 B), of 120000 B.

No run decisions. The forum index's /me summary line still waits for the next /me, as it did before this change.
