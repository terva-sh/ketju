---
schema: 3
id: TKT-01M3S3ZS0H5485CG25KMRCT5N8
title: Show the last-read time beside the unread divider's ordinal
type: task
status: done
status_reason: null
priority: low
due_on: null
labels:
  - area:client
assignees: []
milestone: phase-3
parent: TKT-01M29BN367F3YQZESM63VN26GP
origin: null
dependencies: []
blocks_on: none
references:
  - ref: pr:314
    path: null
claim: null
archive: null
created_at: 2026-09-30T12:17:48Z
updated_at: 2026-10-01T13:47:46Z
created_by:
  id: agent:claude-code/phase2-run-39wbn4q
  name: ""
updated_by:
  id: agent:claude-code/phase3-polish-client
  name: ""
extensions: {}
---

## Description

Found comparing the thread screenshots with `docs/mockups/Thread{Light,Dark}.dc.html` (TKT-01M39WBN4Q, divergence T5).

The mockup's divider reads "last read #63 · Tue 14:10". The client's reads "last read #53" (`web/client/src/components/thread/UnreadDivider.tsx`). docs/06 asks only for "N new posts below", so this is the mockup adding detail rather than a defect. The database keeps `read_positions.last_read_at` (migration 0005), but the thread page's viewer DTO carries only `last_read_ordinal`.

### What to do
Carry `last_read_at` in the thread page's viewer DTO and draw it after the ordinal, in the format TKT-01M3S3ZF48 (Decide between relative and absolute post times) settles.

## Acceptance criteria

- [x] At 390 px and 1440 px the divider reads last read #N · TIME, TIME an absolute PostTime of read_positions.last_read_at
- [x] ThreadViewer carries last_read_at, null with no read position and for anonymous viewers, in the per-viewer block outside the response cache (TestThreadViewerLastReadAt, TestThreadViewerLastReadAtIsPerViewer; golden, dto-contract.ts and docs/07 updated)
- [x] The thread page passes axe at 390 px and 1440 px, and thread-*.png are refreshed with the divider's time masked
- [x] The dist from go generate ./internal/web/spa is committed, npm run budget keeps the reader within 70000 B gzip, and the PR states the reader's growth

## Implementation plan

Groomed 2026-10-01 for the Phase 3 run (docs/14), against main at a599b48.

### Settled in grooming

- **What the divider shows.** Decided by the run (2026-10-01, pending maintainer review) and recorded in docs/06 "Landing on return". The divider ends with "last read #N · TIME" at both widths. TIME is `read_positions.last_read_at` through `PostTime`.
- **What `last_read_at` holds.** It is when the viewer's place in this thread was last set. `UpsertReadPosition` writes it on every beacon, even one that does not move the ordinal. `LowerReadPosition` writes it on a mark-unread, which sets the place where the reader says it is at that moment, so that time is the true one to show. A separate beacon-only column lost, because it is a schema change for one line of text (docs/06, review 1705).
- **The phone draws the same divider.** docs/06 "Mobile" says the divider is identical to desktop. That beats the phone mockup's "read to #63".
- **Times are absolute** (docs/06 "Post anatomy", maintainer 2026-10-01, TKT-01M3S3ZF48).
- **The time is per viewer**, so it goes in `ThreadViewer`. That block is never cached (`dto.ThreadViewer`'s doc, docs/07 "The response cache"). The ETag hashes it, so reading an older page now moves the ETag too. That costs a 304 the page seldom gets, which is accepted.

### Approach

1. **Server.**
   - `dto.ThreadViewer` gains `LastReadAt *Millis json:"last_read_at"`. It is null when the viewer has no `read_positions` row, which covers every anonymous viewer and a member who never opened the thread.
   - `threads.View` (`view.go`) already reads the row with `GetReadPosition`. Keep `rp.LastReadAt` beside `rp.LastReadOrdinal`.
   - If another route builds a `ThreadViewer`, it fills the field the same way.
2. **Contract.** Update the golden, `api/dto.ts`, `api/dto-contract.ts`, and docs/07 "Thread response shape (abridged)".
3. **Client.**
   - `UnreadDivider` takes `at: Millis | null` and renders `last read #N`, then ` · <PostTime at={at}/>` when `at` is set.
   - `ThreadScreen` passes `page.viewer.last_read_at`.
   - `aria-label` on the separator is unchanged.
4. **Screenshots.** `e2e/thread.spec.ts` `capture` masks `.unread-divider time` beside `.post-header time`. Refresh `thread-*.png`.
5. **Build.** Run `go generate ./internal/web/spa` and `npm run budget`.

**Reader bundle.** This is in the reader. Estimated at about +40 B gzip.

### Tests

- `TestThreadViewerLastReadAt` (`internal/service/threads`):
  - It is null for an anonymous viewer and for a member with no position.
  - After `MarkRead` at a fixed clock, it is that time.
  - After a `MarkRead` that does not move the ordinal, it is the later time.
  - After `MarkUnread`, it is the mark's time.
- `TestThreadViewerLastReadAtIsPerViewer`: two members who read the thread at different times are served one cached page body, and each gets their own `last_read_at`. This keeps the response-cache invariant: the field never enters the per-class part.
- `UnreadDivider` in `ThreadScreen.test.tsx`:
  - "the divider reads last read #N · TIME", with a `time` element and `datetime`.
  - "with no time it reads last read #N".
- `e2e/thread.spec.ts`: `.last-read` matches `^last read #53 · ` and holds a `time` with a `datetime`, at 390 px and 1440 px. It replaces today's exact `last read #53`.
- `e2e/a11y.spec.ts`'s thread scans at both widths pass.

### Out of scope

- The unread lens, and whatever it draws at its boundary: TKT-01M29CTY4J.
- Any change to when `last_read_at` is written.

## Notes

**agent:claude-code/phase3-groom-polish** at 2026-10-01T07:56:06Z

run decision: pending maintainer review. The divider ends with 'last read #N · TIME' at both widths. TIME is read_positions.last_read_at, the last time the viewer's position in this thread was written, through PostTime. docs/06 Mobile makes the divider identical to the desktop's, so it beats the phone mockup's 'read to #63'. Recorded in docs/06 'Landing on return' with the alternatives.

**agent:claude-code/phase3-groom-polish** at 2026-10-01T08:30:43Z

Terva review 1705 on PR #269 (head 8aa50f3, base a599b48), medium, 'Do not describe a mark-unread timestamp as the last time reading'. Fixed in docs/06 and the plan. TIME is now described as when the viewer's place in this thread was last set, by a beacon or by a mark-unread, which states the place at that moment. A separate beacon-only column is recorded as the alternative that lost, because it is a schema change for one line of text. This supersedes the run-decision note's 'the last time the viewer's position in this thread was written'.

**agent:claude-code/phase3-polish-client** at 2026-10-01T13:44:18Z

Built against main at 0d5a4d0 (after #309). What the plan did not say, and what I found:

- **Server.** `dto.ThreadViewer.LastReadAt *Millis json:"last_read_at"` is `read_positions.last_read_at` from the same `GetReadPosition` row as `last_read_ordinal`. It is null when there is no row, which covers every anonymous viewer and a member who never set a place. `threads.View` builds the only `ThreadViewer`. Nothing else constructs one, so no other route needed the field.
- **Per viewer, outside the cache.** The viewer block is built per request in `View` and is never stored; `cached.go` caches only posts. TestThreadViewerLastReadAtIsPerViewer has two members set their place 90 minutes apart. It checks that the second view is a response-cache hit serving byte-identical posts, and that each member gets their own `last_read_at`. TestCachedPayloadHoldsNoViewer now also refuses `"last_read_at"` in the stored bytes.
- **TestThreadViewerLastReadAt** covers: null for anonymous, null with no position, the time of a beacon, the later time of a beacon that does not move the ordinal, and the time of a mark-unread. A mutation that read the ordinal into the time failed both tests.
- **Contract.** Goldens in internal/dto (member and moderator carry a time, anon null) and internal/web/api (thread.*.json, thread.lens.json) are regenerated. dto.ts is updated. dto-contract.ts needs no edit because it checks the goldens against dto.ts. docs/07 "Thread response shape" shows the field.
- **Client.** `UnreadDivider` takes `at` and renders `last read #N · <PostTime>` when it is set. The separator's aria-label is unchanged. The divider tests are in ThreadPage.test.tsx, beside the divider's other tests, rather than ThreadScreen.test.tsx as the plan named.
- Reader bundle: 60809 B before, 60823 B after (+14 B gzip) of 70000 B. PostTime was already in the reader.
- e2e on this host at 390 and 1440 px: thread, a11y, read-tracking and shortcuts passed (41 passed, 5 skipped by design). thread-*.png are refreshed, with `.unread-divider time` masked.

## Summary

The unread divider reads 'last read #N · TIME' at both widths, where TIME is PostTime of read_positions.last_read_at. The thread page carries that time in the per-viewer block as viewer.last_read_at: null with no read position (every anonymous viewer included), outside the response cache, which holds only posts. TestThreadViewerLastReadAt covers beacons, a beacon that does not move the ordinal, and mark-unread. TestThreadViewerLastReadAtIsPerViewer shows two members served one cached page, each with their own time. Goldens, dto.ts and docs/07 are updated. Reader bundle 60809 -> 60823 B gzip (+14 B).
