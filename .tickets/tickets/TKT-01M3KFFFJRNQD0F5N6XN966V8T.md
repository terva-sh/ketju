---
schema: 3
id: TKT-01M3KFFFJRNQD0F5N6XN966V8T
title: Add the mark-unread control to the thread page
type: task
status: in-progress
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
claim:
  actor: agent:claude-code/phase2-run-kfffjr
  branch: run/kfffjr-mark-unread
  worktree: /home/sothr/workspace/git.local.sothr.com/terva-sh/ketju/.claude/worktrees/agent-ae815ef097eb2996b
  commit: 13709f0d74ab93dbdc040da9e4bd218ed6f0a324
  session: null
  claimed_at: 2026-09-30T18:42:33Z
  expires_at: null
archive: null
created_at: 2026-09-28T07:43:10Z
updated_at: 2026-09-30T18:42:36Z
created_by:
  id: agent:claude-code/phase1-run-438e26c1
  name: ""
updated_by:
  id: agent:claude-code/phase2-run-kfffjr
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
- [ ] A whole-thread Mark unread action (toolbar and phone more menu) and an Unread from here action on each post (footer on desktop, PostMore on a phone) mark the thread unread, then return to the thread list; e2e/read-tracking.spec.ts covers both at 390 px and 1440 px, the server's position stays lowered, and docs/06 Mobile lists unread from here under the post's more control

## Implementation plan

Groomed 2026-09-29 against main at 7af2c5a (TKT-01M3M9RR26, Phase 2 grooming). Refreshed against main at 5124209 on 2026-09-30: line references corrected, the docs/06 step removed because it has landed, and the per-post control fitted to the phone's "more" disclosure from PR #202.

### Settled in grooming
- The server contract is docs/07 "Read position" (`:751-784`), and it landed with TKT-01M29CA9CE:
  - `DELETE /api/v1/threads/{id}/read` takes an optional `{ordinal}`. No body marks the whole thread unread, and `{ordinal}` marks every post after that ordinal unread. It never raises the position and answers `{last_read_ordinal}`.
  - A `PUT` sent before the `DELETE` that arrives after it moves the position forward again.
  - The handler is `internal/web/api/viewerstate.go:64` and the service is `MarkUnread` (`internal/service/threads/viewerstate.go:101`).
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
  - "Unread from here" is in each post's footer (`components/thread/Post.tsx:83-129`). It sends `{ordinal: p.ordinal - 1}`, so that post and every later one are unread.
  - Once the DELETE answers, the page navigates to the thread list the reader came from, or to the thread's forum list when there is no such entry in history. Nothing on screen is read again, so the mark sticks.
  - The options that lost were a thread action only, a per-post action only, staying on the page with tracking paused, and staying with tracking as usual. The last would re-read the visible posts within a second and undo the mark.
- Settled here as engineering choices:
  - Port `markUnread(id, ordinal?)` from `74c4e8f`, with its `held`, `marks` and `after` maps. Since then, `4ccc32d` numbered the loads in `state/threads.ts`. So "requested before the answer" uses that counter, not the old `asking()` wrapper: `load()` records each page's request number in a `WeakMap<ThreadPage, number>`, exposed as `pageRequestedAt(page)`, and `markUnread` records the counter's value when the DELETE answers. The alternative, restoring `asking()` around `loadThreadPage` in `ThreadScreen.tsx`, lost because it keeps a second counter beside one that already orders the same loads.
  - The review-1149 fix: the reads collected while a DELETE is pending are kept per thread past a failed DELETE, in a `carried` map, and are queued against the answer of the next mark that succeeds. They are not queued against the unchanged floor, where they would be dropped.
  - `state/threads.ts` exports `forgetThreadPages(id)`, the existing `forgetThread`, for `markUnread` to call. The old `forgetHeldPages` does not come back as a second function.
- Settled in the refresh of 2026-09-30, as engineering choices:
  - **On a phone, "Unread from here" is an item in `PostMore`** (`components/thread/PostMore.tsx`, PR #202), beside link, flag and history. On desktop it is an inline footer button. docs/06 "Mobile" (`:240-242`) keeps reply, quote and the reply expander inline and puts the rest under "more", and `PostMore`'s own comment says six inline controls with 44 px targets do not fit the footer at 390 px. A fourth inline control on the phone lost for that reason. The item follows flag's pattern: `Post` passes an optional `onUnread` handler, and `PostMore` shows the item only when it is given.
  - **The per-post control has its own gate.** `Post`'s `actions` prop exists only when the reader may reply (`ThreadScreen.tsx:180-182`), and a member who may not reply in a locked thread may still mark it unread. So `onUnread` is passed when `tracksUnread()` (`keyboard/shortcuts.ts`) is true, meaning a signed-in member, as `onFlag` is passed by its own rule. Hanging it off `actions` lost.
  - **`ShellAction` does not gain a `disabled` field.** It is `{id, label, run}` (`state/shell.ts:8-12`), and neither `Toolbar` nor `MoreMenu` draws a disabled state. The whole-thread action's `run` ignores a press while a mark of that thread is out. The post control is a real `<button>` and is `disabled` while its DELETE is out. Adding `disabled` to `ShellAction` lost: it changes two shell components and their tests for one action, and a second press would be harmless anyway, because marks run in call order.
  - **Stale counts are dropped, not patched.** Thread-list rows carry `unread` and `last_read_ordinal` (`api/dto.ts:145-146`) and are held by `api/client.ts` under `/api/v1/forums/{slug}/threads?…`. The forum index holds per-forum counts under `/api/v1/forums`, and the nav badge reads `viewer.unread`. After a successful mark, `forgetGets((u) => u.startsWith('/api/v1/forums'))` drops both, and `refreshViewer()` refreshes the badge, before the page navigates. Editing the held rows in place lost, because the list's order and counts are the server's to compute.

### Approach
1. `web/client/src/state/readTracker.ts`: `export function markUnread(id: string, ordinal?: number): Promise<ReadPosition>`, implementing the settled requirements. Replace the comment at `:34-39` with what now lowers the floor. `trackPosts(id, list, stored)` (`:175`) gains a fourth parameter, `page`, so that a page requested before the mark's answer does not raise the floor. Its one caller, `useReadTracking` in `screens/ThreadScreen.tsx:279-285`, already has the page in its effect's dependencies.
2. `web/client/src/state/threads.ts`: `pageRequestedAt` over the `loads` counter (`:190`, bumped in `load()` at `:84-119`), and `forgetThreadPages`, which exports `forgetThread` (`:194`), as settled.
3. The controls:
   - the `mark-unread` entry in `threadActions`, passed from `ThreadScreen.tsx:96-104` when `tracksUnread()` is true;
   - on desktop, an "Unread from here" button in `Post.tsx`'s footer (`data-post-action="unread"`), after quote and reply;
   - on a phone, the same action as an item in `PostMore`, as settled.

   A failure is shown the way the page shows other failed actions, and the reader stays on the page and can try again. On success, drop the held forum and thread-list bodies and refresh the viewer as settled, then navigate to the list.
4. `docs/06` "Mobile" (`:240-242`): add "unread from here" to the controls under "more". The "Read tracking" paragraph on the two controls is already on main (`docs/06-thread-views.md:64-68`).
5. Run `npm --prefix web/client run build`, which ends with the budget check (`scripts/budget.mjs`), and `go generate ./internal/web/spa`, and commit `internal/web/spa/dist`. The thread view was 47415 B gzip of the 60000 B `threadViewGzipBytes` budget after PR #202 (TKT-01M3QQWA5Z). Record the new figure in a note.

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
- Vitest for the controls: the thread action calls `markUnread(id)` and the post action calls `markUnread(id, ordinal - 1)`, from the footer on desktop and from `PostMore` on a phone. Both drop the held list bodies and navigate to the list on success, and stay on failure. The post action is offered to a signed-in member in a thread they cannot reply in, and to nobody signed out.
- Playwright in `e2e/read-tracking.spec.ts`, which signs in as carol from `stateFile('carol')`: mark a read thread unread from the page. The server's position is lower (checked with `readPosition` from `e2e/session.ts`), and the page is back on the thread list with the thread shown unread. The same holds for "Unread from here" on a middle post, reached on a phone through `[data-post-more]` first, as `e2e/flag.spec.ts` does. Each test sets carol's position with `setReadPosition` before it starts, because other specs move it.

### Out of scope
- The no-JS route `POST /t/{id}/unread` (docs/07 `:1009`), which is the HTML client's (TKT-01M29BN36W).
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

**agent:claude-code/phase2-groom** at 2026-09-30T08:51:44Z

Plan refreshed against main at 5124209 on 2026-09-30. Changed: line references (docs/07 Read position is now :751-784 and POST /t/{id}/unread :1009; MarkUnread is viewerstate.go:101; Post.tsx's footer is :83-129; trackPosts :175; the loads counter :190; forgetThread :194). Step 4's docs/06 Read tracking paragraph has landed (docs/06-thread-views.md:64-68), so the step now only adds 'unread from here' to docs/06 Mobile's list under 'more'. The bundle figure is now 47415 B of 60000 B (after PR #202), not 38171 B. Settled as engineering choices, recorded in the plan with the alternatives that lost: on a phone, Unread from here is an item in PostMore (PR #202), passed as an optional onUnread like onFlag, because a fourth inline control does not fit at 390 px; it is gated on tracksUnread(), not on actions, which exists only when the reader may reply; ShellAction gains no disabled field, and the whole-thread action ignores a press while its mark is out; after a mark, forgetGets drops the held /api/v1/forums bodies (forum index and thread lists) and refreshViewer refreshes the badge. Criterion 4 now names the spec, both widths, and the docs/06 Mobile change in place of the Read tracking paragraph that is already there. The Settled items are carried with their decisions unchanged; only their line numbers were corrected.
