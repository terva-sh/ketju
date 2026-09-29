---
schema: 3
id: TKT-01M3KFFFJRNQD0F5N6XN966V8T
title: Add the mark-unread control to the thread page
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:client
  - effort:M
assignees: []
milestone: phase-2
parent: TKT-01M29BN367F3YQZESM63VN26GP
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-28T07:43:10Z
updated_at: 2026-09-29T05:38:20Z
created_by:
  id: agent:claude-code/phase1-run-438e26c1
  name: ""
updated_by:
  id: agent:claude-code/phase2-groom
  name: ""
extensions: {}
---

## Description

The thread page has no way to mark a thread, or the posts after a given post, unread. The server side landed with TKT-01M29CA9CE (PR #99). The client's read-tracking beacon landed with TKT-01M29CGBWN (PR #101) and does not include this control. An implementation of the client-side ordering, `markUnread` in `web/client/src/state/readTracker.ts`, was written on PR #101 and removed before merge because no criterion there covered it. It is at commit dd761e95ee631b5e07583cbd230e3eedbef3798d on the `run/cgbwn-read-beacon` branch, with its tests in `readTracker.test.ts`, and can be reused.

### Server contract (docs/07 "Read position")

- `PUT /api/v1/threads/{id}/read {ordinal}` moves the position forward only. The server keeps the larger of the stored position and the one sent.
- `DELETE /api/v1/threads/{id}/read`, with an optional `{ordinal}` body, is the only way the position goes down. With no body the whole thread is unread. It never raises the position. Both answer `{last_read_ordinal}`, the position as stored.
- A PUT sent before a DELETE and arriving after it moves the position forward again, because the server cannot tell it from a new read.

### Requirements on the control

- Flush the beacon's pending position and wait for every PUT in flight for the thread before sending the DELETE. Send no beacon for the thread until the DELETE has answered.
- After the DELETE answers, lower the beacon's floor for the thread to the answer. Nothing in `readTracker.ts` lowers it today. Held page bodies of the thread report the old position, so forget them. A page requested before the DELETE answered must not raise the floor again. A page requested after it must.
- Two marks of one thread must take effect in call order, because two DELETEs in flight can land in either order.
- Posts read while a DELETE is pending must be kept and judged against the DELETE's answer, not the old floor. Review 1149 on PR #101 found a medium defect in the removed implementation: when the DELETE fails, the reads kept while it was pending were checked against the unchanged, higher floor and dropped. A retried mark that then succeeded never sent them, and their dwell timers had already fired. Reads made while a DELETE is pending must survive a failed DELETE, so that a retried mark still sends them.

### Design

docs/06 "Read tracking", docs/07 "Read position".

## Acceptance criteria

- [ ] markUnread flushes the beacon and waits for every PUT in flight before its DELETE, sends no PUT for the thread until the DELETE answers, lowers the floor to the answer, and runs two marks of one thread in call order (the restored readTracker.test.ts markUnread tests)
- [ ] A page requested before the DELETE answered does not raise the floor and one requested after it does, and reads made while a DELETE is pending survive a failed DELETE and are sent after a retried mark succeeds (keeps what was read during a failed DELETE for the next mark)
- [ ] The dist is rebuilt with go generate ./internal/web/spa and the thread-view bundle stays within the 60000 B gzip budget
- [ ] A whole-thread Mark unread action (toolbar and phone more menu) and an Unread from here action on each post mark the thread unread, then return to the thread list; a Playwright test covers both, the server's position stays lowered, and docs/06 Read tracking describes them

## Implementation plan

Groomed 2026-09-29 against main at 7af2c5a (TKT-01M3M9RR26, Phase 2 grooming).

### Settled in grooming
- The server contract is docs/07 "Read position" (`:304-338`), and it landed with TKT-01M29CA9CE:
  - `DELETE /api/v1/threads/{id}/read` takes an optional `{ordinal}`. No body marks the whole thread unread, and `{ordinal}` marks every post after that ordinal unread. It never raises the position and answers `{last_read_ordinal}`.
  - A `PUT` sent before the `DELETE` that arrives after it moves the position forward again.
  - The handler is `internal/web/api/viewerstate.go:64` and the service is `MarkUnread` (`internal/service/threads/viewerstate.go:92`).
- The client requirements are in this ticket's description and are settled:
  - flush the beacon and wait for every PUT in flight before the DELETE, and send no beacon until it answers;
  - lower the floor to the answer;
  - forget the thread's held page bodies;
  - a page requested before the answer must not raise the floor again, and one requested after it must;
  - marks of one thread take effect in call order;
  - reads made while the DELETE is pending are judged against its answer, and survive a failed DELETE so that a retried mark still sends them (the review-1149 defect).
- The removed implementation is at `74c4e8f` (`git show 74c4e8f:web/client/src/state/readTracker.ts`, with its tests in `readTracker.test.ts`). The note of 2026-09-28 supersedes the `dd761e9` pointer. `readTracker.ts:34-39` on main names this ticket as the one that lowers the floor.
- **Maintainer decision 2026-09-29** (recorded in the note of that date on this ticket; the option offered in grooming as "C with 1"): mark-unread is offered in both places, and afterwards the page returns to the list.
  - A whole-thread "Mark unread" is in `threadActions` (`web/client/src/shell/threadActions.ts`), so it is on the desktop toolbar and in the phone's more menu. It sends DELETE with no body.
  - "Unread from here" is in each post's footer (`components/thread/Post.tsx:68-98`). It sends `{ordinal: p.ordinal - 1}`, so that post and every later one are unread.
  - Once the DELETE answers, the page navigates to the thread list the reader came from, or to the thread's forum list when there is no such entry in history. Nothing on screen is read again, so the mark sticks.
  - The options that lost were a thread action only, a per-post action only, staying on the page with tracking paused, and staying with tracking as usual. The last would re-read the visible posts within a second and undo the mark.
- Settled here as engineering choices:
  - Port `markUnread(id, ordinal?)` from `74c4e8f`, with its `held`, `marks` and `after` maps. Since then, `4ccc32d` numbered the loads in `state/threads.ts`. So "requested before the answer" uses that counter, not the old `asking()` wrapper: `load()` records each page's request number in a `WeakMap<ThreadPage, number>`, exposed as `pageRequestedAt(page)`, and `markUnread` records the counter's value when the DELETE answers. The alternative, restoring `asking()` around `loadThreadPage` in `ThreadScreen.tsx`, lost because it keeps a second counter beside one that already orders the same loads.
  - The review-1149 fix: the reads collected while a DELETE is pending are kept per thread past a failed DELETE, in a `carried` map, and are queued against the answer of the next mark that succeeds. They are not queued against the unchanged floor, where they would be dropped.
  - `state/threads.ts` exports `forgetThreadPages(id)`, the existing `forgetThread`, for `markUnread` to call. The old `forgetHeldPages` does not come back as a second function.

### Approach
1. `web/client/src/state/readTracker.ts`: `export function markUnread(id: string, ordinal?: number): Promise<ReadPosition>`, implementing the settled requirements. Replace the comment at `:34-39` with what now lowers the floor. `trackPosts(id, list, stored, page?)` takes the page, so that a page requested before the mark's answer does not raise the floor.
2. `web/client/src/state/threads.ts`: `pageRequestedAt` and `forgetThreadPages`, as settled.
3. The controls: the `mark-unread` entry in `threadActions`, and an "Unread from here" button in `Post.tsx`'s footer beside quote and reply (`data-post-action="unread"`), shown only to a signed-in member. Each is disabled while its DELETE is out. A failure is shown the way the page shows other failed actions, and the reader stays on the page and can try again. On success, any cached thread-list entry for the thread must not still show it as read (find where the list keeps unread counts, and drop or refresh that entry), and then the page navigates to the list as settled.
4. `docs/06` "Read tracking": one paragraph on the two controls and the return to the list, citing the 2026-09-29 decision.
5. Run `npm --prefix web/client run build`, which includes the budget check, and `go generate ./internal/web/spa`, and commit `internal/web/spa/dist`. The bundle was 38171 B gzip of the 60000 B budget at `2d86f82`.

### Tests
- Vitest in `readTracker.test.ts`: the seven tests removed with `69f66a0`, restored and adapted:
  - `flushes and waits for every PUT before the DELETE, and sends nothing while it is out`
  - `starts no PUT while it waits for one already out`
  - `keeps what is read while the DELETE is out, and judges it by the answer`
  - `does not send after the DELETE a PUT from before the call that failed`
  - `runs overlapping marks one at a time, in call order`
  - `takes a page position only from a page asked for after the mark`
  - `sends no body to mark the whole thread unread`
  Plus `keeps what was read during a failed DELETE for the next mark`, the review-1149 case.
- Vitest for the controls: the thread action calls `markUnread(id)` and the post action calls `markUnread(id, ordinal - 1)`. Both navigate to the list on success and stay on failure.
- Playwright in `e2e/read-tracking.spec.ts`: mark a read thread unread from the page. The server's position is lower (checked with GET), the page is back on the thread list with the thread shown unread, and the same holds for "Unread from here" on a middle post.

### Out of scope
- The no-JS route `POST /t/{id}/unread` (docs/07 `:533`), which is the HTML client's (TKT-01M29BN36W).
- A keyboard shortcut for the control. It can be added to `keyboard/shortcuts.ts` once the control exists.

## Notes

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T15:01:09Z

This note supersedes the pointer to dd761e9 for the removed markUnread implementation. PR #101 was rebase-merged, which rewrote that commit, so dd761e9 is not in main's history and will not stay reachable. The same code is on main at 74c4e8f ("record review 1148's fixes"): `git diff dd761e9 74c4e8f -- web/client/src` is empty. Use `git show 74c4e8f:web/client/src/state/readTracker.ts`.

**agent:claude-code/phase2-groom** at 2026-09-29T05:10:10Z

Open decision from Phase 2 grooming (TKT-01M3M9RR26), 2026-09-29. It is a product choice, so it is not settled here.

The design says what mark-unread does to the read position, but not where the control is, whether it covers the whole thread or "from this post", or what the page does after it answers. docs/06, docs/07 and the mockups are all silent. The options are in the plan's "Open decision" section:
- placement: A, the thread action only; B, per post only; or C, both;
- afterwards: 1, go back to the list; 2, stay and stop dwell-tracking the marked posts; or 3, stay and track as usual. Option 3 re-reads the posts on screen within a second, which undoes the mark.

Recommendation: C with 1. This ticket needs the answer before it is claimed.

**agent:claude-code/phase2-groom** at 2026-09-29T05:38:20Z

Maintainer decision, 2026-09-29, relayed by the Phase 2 grooming coordinator (TKT-01M3M9RR26): the open decision in the note of 2026-09-29 is settled as recommended, C with 1. Mark-unread is both a whole-thread action (desktop toolbar and phone more menu) and an 'Unread from here' action on each post, and after the DELETE answers the page returns to the thread list. This note supersedes the open-decision note. The plan's Settled section and criterion 4 now say so.
