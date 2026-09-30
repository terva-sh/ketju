---
schema: 3
id: TKT-01M3KFFFJRNQD0F5N6XN966V8T
title: Add the mark-unread control to the thread page
type: task
status: done
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
references:
  - ref: pr:248
    path: null
claim: null
archive: null
created_at: 2026-09-28T07:43:10Z
updated_at: 2026-09-30T21:14:11Z
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

- [x] markUnread flushes the beacon and waits for every PUT in flight before its DELETE, sends no PUT for the thread until the DELETE answers, lowers the floor to the answer, and runs two marks of one thread in call order (the restored readTracker.test.ts markUnread tests)
- [x] A page requested before the DELETE answered does not raise the floor and one requested after it does, and reads made while a DELETE is pending survive a failed DELETE and are sent after a retried mark succeeds (keeps what was read during a failed DELETE for the next mark)
- [x] The dist is rebuilt with go generate ./internal/web/spa and the thread-view bundle stays within the 60000 B gzip budget
- [x] A whole-thread Mark unread action (toolbar and phone more menu) and an Unread from here action on each post (footer on desktop, PostMore on a phone) mark the thread unread, then return to the thread list; e2e/read-tracking.spec.ts covers both at 390 px and 1440 px, the server's position stays lowered, and docs/06 Mobile lists unread from here under the post's more control

## Implementation plan

Groomed 2026-09-29 against main at 7af2c5a (TKT-01M3M9RR26, Phase 2 grooming). Refreshed against main at 5124209 on 2026-09-30. Revised at claim on 2026-09-30 against main at 13709f0 (agent:claude-code/phase2-run-kfffjr). The settled items below are carried unchanged. The approach adds where the page returns to and how a failure shows, and fits the post footer and thread screen as the moderation panel, drafts and the redacted marker left them.

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
1. `web/client/src/state/readTracker.ts`: `export function markUnread(id: string, ordinal?: number): Promise<ReadPosition>`, ported from `74c4e8f` with its `held`, `marks` and `after` maps, implementing the settled requirements.
   - The comment at `:34-39` is replaced with what now lowers the floor.
   - `markedAt` records `loadCount()` from `state/threads.ts` when the DELETE answers, in place of the old private clock.
   - `trackPosts(id, list, stored, page?)` takes the page. It takes `stored` as known only when no mark has answered for the thread, or when `pageRequestedAt(page)` is later than the newest mark.
   - `carried` keeps, per thread, the highest read collected during a mark whose DELETE failed, and the next mark that succeeds queues it against its answer (the review-1149 fix). The same read is also queued against the unchanged floor when the DELETE fails, as the removed code did, so a read past that floor still goes out although the mark failed. A read at or below the floor is dropped there and kept only in `carried`, which is the settled fix: before, that drop was all that happened to it.
   - `resetReadTracker` clears the new maps.
2. `web/client/src/state/threads.ts`:
   - `load()` records each page it answers with its request number in a `WeakMap<ThreadPage, number>`. When a 304 hands back a body already recorded, it keeps the larger number. A 304 to a request made after the mark means the server says that body is still right, and its ETag hashes the viewer's position.
   - `pageRequestedAt(page)` reads the map, and `loadCount()` reads the `loads` counter.
   - `forgetThreadPages(id)` is the existing `forgetThread`, exported under that name.
3. `web/client/src/state/lists.ts` (new, a few lines): `ThreadListScreen` records the address of the list it shows, with its cursor and sort, through `rememberList(forum, href)`. `listFor(forum)` answers that address when it is for the thread's forum, and `/f/{forum}` otherwise. This is how the page knows "the list the reader came from": preact-iso keeps no history of its own, and `history` cannot read any entry but the current one.
4. The controls, in `screens/ThreadScreen.tsx`:
   - `markAndReturn` calls `markUnread`. On success it calls `forgetGets((u) => u.startsWith('/api/v1/forums'))` and `refreshViewer()`, as settled, then `route(listFor(forum), true)`, but only if the reader is still on this thread, as `posted` checks. It replaces the thread's history entry rather than pushing one, so Back does not reopen the page just marked and read it again.
   - The whole-thread action, "Mark unread" (`mark-unread`), goes in `threadActions` after First unread. It is passed when `tracksUnread()` is true, on the thread body once a page is on screen, and not on the post history route. Its `run` ignores a press while a mark is out, which a ref tracks.
   - A failure of the whole-thread action shows as a `role=alert` paragraph under the thread head: "This thread could not be marked unread." followed by `problemMessage`'s text, as `FlagDialog` and `ModerateDialog` word their refusals. The reader stays and can press again.
   - Per post: `ThreadBody` passes `onUnread(ordinal)` to `Post` when `tracksUnread()` is true. It sends `ordinal - 1` and returns the mark's promise. `Post` holds the busy and error state for its own control.
   - On desktop the control is a footer button, "unread from here" (`data-post-action="unread"`), after quote and reply, and `disabled` while its DELETE is out. On a phone, `PostMore` takes an optional `onUnread` and shows the item, which closes the list as flag's does. A failure shows as a `role=alert` paragraph under that post's footer.
   - Tombstones get no control. The whole-thread action covers them.
5. `docs/06` "Mobile" (`:240-242`): add "unread from here" to the controls under "more".
6. Run `npm --prefix web/client run build`, which ends with the budget check, then `go generate ./internal/web/spa`, and commit `internal/web/spa/dist`. Report the reader bundle's change from 54924 B.

### Tests
- Vitest in `readTracker.test.ts`: the seven tests removed with `69f66a0`, restored and adapted. The page-order test uses `loadThreadPage` in place of `asking`.
  - `flushes and waits for every PUT before the DELETE, and sends nothing while it is out`
  - `starts no PUT while it waits for one already out`
  - `keeps what is read while the DELETE is out, and judges it by the answer`
  - `does not send after the DELETE a PUT from before the call that failed`
  - `runs overlapping marks one at a time, in call order`
  - `takes a page position only from a page asked for after the mark`
  - `sends no body to mark the whole thread unread`

  Plus `keeps what was read during a failed DELETE for the next mark`, the review-1149 case.
- Vitest for the controls, in `ThreadScreen.test.tsx`:
  - the thread action sends DELETE with no body, and the post action sends `{ordinal: N - 1}`, from the footer on desktop and from `PostMore` on a phone;
  - on success both drop the held forum and list bodies and navigate to the list, which is the remembered list when it belongs to this forum;
  - on failure both stay on the page and show an alert;
  - the post action is offered to a signed-in member in a thread they cannot reply in, and to nobody signed out.
- `threadActions.test.ts`: Mark unread is offered only when it is handed over.
- Playwright in `e2e/read-tracking.spec.ts`, signed in as carol from `stateFile('carol')`. Each test sets carol's position with `setReadPosition` first, and waits out the dwell before pressing, so that no read is pending when the DELETE goes.
  - Mark the thread unread from the page, through the desktop toolbar or the phone's more menu. The server's position is 0, and the page is back on General's list with the thread's unread badge showing.
  - "Unread from here" on post 3, reached on a phone through `[data-post-more]` first, as `e2e/flag.spec.ts` does. The DELETE's body is `{ordinal: 2}`, the position is 2, and the page is on the list.

  Each test runs at most one axe scan.

### Out of scope
- The no-JS route `POST /t/{id}/unread` (docs/07 `:1009`), which is the HTML client's (TKT-01M29BN36W).
- A keyboard shortcut for the control. It can be added to `keyboard/shortcuts.ts` once the control exists.
- A server change. The DELETE acts on the session's own user, taken from the session and never from the request (`internal/web/api/viewerstate.go:64`, `internal/service/threads/viewerstate.go:101`), so no member can move another member's position through it.

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

**agent:claude-code/phase2-run-kfffjr** at 2026-09-30T19:06:26Z

Built on a local branch from main at 13709f0, and not pushed yet: the client dist is held by TKT-01M3PTT97X. The branch will be replayed onto origin/main when the dist is free, and the dist regenerated then.

Engineering choices made while building, beyond the plan's settled items:

- **A failed DELETE queues its reads against the unchanged floor as well as carrying them.** The settled review-1149 item says the reads "are not queued against the unchanged floor, where they would be dropped". That is read here as "not only queued there", because queueing is what the removed code did and was the whole defect. Dropping the queue as well would lose a read above the old floor, which a failed mark leaves true. So those reads are carried to the next mark that succeeds and also queued as before. The test "still sends a read past the floor when the DELETE fails" holds the second half, and "keeps what was read during a failed DELETE for the next mark" holds the first.
- **"The list the reader came from" is `state/lists.ts`.** `ThreadListScreen` records the list address it shows, cursor and sort included, and the thread page goes there when the list is the thread's forum, and to `/f/{forum}` otherwise. Reading the previous history entry lost, because a page cannot see any entry but its current one. Remembering it in `history.state` lost too, because the reader may have paged through the thread since leaving the list.
- **The thread's history entry is replaced (`route(href, true)`), not pushed.** Otherwise Back reopens the page just marked, which reads the posts on screen again within a second and undoes the mark, as the maintainer's decision says staying would.
- **A failure** is a `role=alert` paragraph under the thread head for the whole-thread action, or under the post for unread from here, worded as the flag and moderation refusals are. The reader stays and can press again.
- **Tombstones get no per-post control.** The whole-thread action covers them.
- **The hold had no test.** A mutation removing it survived the seven restored tests, so "sends no PUT a failed one put back while the mark waits" was added. Every mutation tried is listed in the code commit's message.

Security: the DELETE takes its user from the session (`internal/web/api/viewerstate.go:64`, `internal/service/threads/viewerstate.go:101`), and the client sends no user, so the control cannot move another member's position. No server change was needed.

Figures: the reader bundle is 55740 B gzip of 70000 B, up 816 B from 54924 B. Tests: Vitest 1856 passed; the full Playwright suite passed at 390 px and 1440 px, apart from `e2e/targets.spec.ts`'s list of PostMore items, which now names unread from here and passes. `go test -race ./...`, gofmt, build and vet are clean.

**agent:claude-code/phase2-run-kfffjr** at 2026-09-30T20:47:57Z

Replayed onto origin/main at c9909b0 (after #243, #245 and #247), with the old dist commit dropped and the dist regenerated. This note supersedes the figures in the build note.

- **Conflicts with #243 (TKT-01M3PTT97X, edit and delete) in `Post.tsx`, `PostMore.tsx` and `ThreadScreen.tsx`** were resolved by keeping both features. The phone's post "more" list now reads link, flag, edit, delete, unread from here, history. The desktop footer uses the same order: "unread from here" sits after delete and before history, which is still after quote and reply, as settled.
- **Unread from here is not offered on a post whose editor or delete confirmation is open.** It leaves the page, which would drop the edit. Main hides quote and reply while editing on the same reasoning. Test: `offers no unread from here on a post being edited or deleted` in `PostControls.test.tsx`, which fails when the gate is removed.
- **`PostControls.test.tsx` lists the footer actions.** Its `footer()` helper now leaves out `unread`, which every signed-in member has on every post and `ThreadScreen.test.tsx` covers. The two assertions of the full order name it in its place.

Figures: the reader bundle is 58062 B gzip of 70000 B, up 859 B from 57203 B on main. Vitest 1934 passed; gofmt, build, vet and `go test -race ./...` are clean; the full Playwright suite on port 18961 is recorded in the summary.

**agent:claude-code/phase2-run-kfffjr** at 2026-09-30T20:56:17Z

Terva review 1619 (run 1617) on PR #248, head b52b599, base 9b142b7. One medium finding, "Wait for queued marks before returning to the list". Accepted and fixed in 3a0c99e:
- Two marks of one thread, from a post and from the thread's actions, ran in call order, but each returned to the list as soon as its own DELETE answered. So the list could load counts the later mark was about to change.
- `markAndReturn` now drops the held counts after every mark that succeeds, but navigates only when `marking(id)`, a new export of the read tracker, says no mark of the thread is still out.
- Tests: "takes one press at a time while a mark is out" now asserts that the page stays on the thread and asks for no list until the second mark lands, and fails when the guard is removed. "says a later mark is still out when an earlier one settles" covers `marking`.

Reader bundle after the fix: 58075 B gzip of 70000 B, up 872 B from 57203 B.

**agent:claude-code/phase2-run-kfffjr** at 2026-09-30T21:00:58Z

Terva review 1622 (run 1619) on PR #248, head edf2fbc, base 9b142b7. It marks review 1619's finding resolved and raises one medium finding, "Do not leave post history when an earlier mark finishes". Accepted and fixed:
- The return check took every path under `/t/{id}/` for the thread, so a mark that answered after the reader had opened a post's history replaced that screen with the list.
- `markAndReturn` now returns only while the reader is on the thread's posts (`onThreadBody`).
- The test "leaves a reader who went to %s where they are" covers a post's history and another screen, and fails with the old check.

Reader bundle: 58080 B gzip of 70000 B, up 877 B from 57203 B.

**agent:claude-code/phase2-run-kfffjr** at 2026-09-30T21:05:32Z

Terva review 1623 (run 1621) on PR #248, head cf97e89, base 9b142b7. It marks review 1622's finding resolved and raises three medium findings.

1. "Preserve a failed PUT's pending read if the DELETE also fails": accepted and fixed in 22de596. A PUT from before the call that fails is put back, and is dropped as what the mark undoes. It is now queued again when the DELETE fails too. Test: "keeps a failed PUT from before the call when the DELETE fails too".
2. "Return to the list when a queued mark fails after an earlier success": accepted and fixed in 22de596. When the last queued mark of a thread settles, `markAndReturn` returns to the list if any of them succeeded, and the failure still rejects for its caller. Test: "returns once the last queued mark settles if an earlier one succeeded".
3. "Await the viewer refresh before treating the return as complete": **disagreement: pending maintainer.** This is not a security finding: it concerns the unread badge's timing, not sessions, roles, status, access to posts or credentials.
   - The badge reads `viewer`, a signal, so it updates whenever `/me` answers, on whatever screen is showing, including the list the reader returns to.
   - Awaiting the refresh would add a round trip to every return. A slow or hung `/me` would hold the reader on a page that reads its posts back up, which is the outcome the maintainer's decision of 2026-09-29 returns to the list to avoid.
   - A failed refresh leaves the badge as it was until the next `/me`, as the two existing callers do: `UsersScreen` and `QueueScreen` also call `refreshViewer()` without waiting. The settled item "refreshViewer() refreshes the badge, before the page navigates" is met: the call is made before the navigation.
   - Code unchanged; the comment on `markAndReturn` now says why.

Reader bundle: 58122 B gzip of 70000 B, up 919 B from 57203 B.

**agent:claude-code/phase2-run-kfffjr** at 2026-09-30T21:14:11Z

Terva review 1624 (run 1624) on PR #248, head caf6dbe, base 9b142b7. It marks review 1623's findings 1 and 2 resolved. It reports finding 3, the unawaited viewer refresh, as still present; that is the disagreement recorded in the previous note and pending maintainer. It raises one new medium finding.

"Prevent in-flight forum requests from restoring stale unread counts": accepted and fixed.
- `forgetGets` takes an `inFlight` option that also supersedes matching requests still out, so their answers are returned but not held.
- `markAndReturn` uses it for `/api/v1/forums…`. The default is unchanged for the thread cache's callers.
- Test: "supersedes matching requests still out when asked to" in `api/client.test.ts`, which fails without the option's loop.
- A screen-level test could not tell the option from none. Every forum GET in the app is aborted when its screen unmounts, and an aborted answer is never held. So the option guards only the window between the mark landing and that unmount, for example the `n` shortcut's paged list search.

Reader bundle: 58151 B gzip of 70000 B, up 948 B from 57203 B. This is review round 4 of the five the run allows.

## Summary

Mark unread is on the thread page in the two places the maintainer chose on 2026-09-29:
- "Mark unread" is a whole-thread action, on the desktop toolbar and in the phone's more menu.
- "unread from here" is on each post, in the footer on desktop and in `PostMore` on a phone.

After the DELETE answers, the page drops the held forum index and thread lists, refreshes the viewer's badge, and replaces its history entry with the list the reader came from (`state/lists.ts`), or with the thread's forum.

`state/readTracker.ts` has `markUnread`, ported from 74c4e8f:
- It flushes and waits for the PUTs before the DELETE, and holds beacons until the DELETE answers.
- It lowers the floor to the answer, and forgets the thread's held pages.
- It judges a page by the thread cache's load number (`pageRequestedAt`, `loadCount`).
- It runs marks of one thread in call order.
- It carries reads from a failed DELETE to the next mark that succeeds (review 1149).

A failure shows as an alert and the reader stays on the page. The control is gated on a signed-in member, not on replying, and is not offered on a post being edited or deleted. docs/06 "Mobile" lists it under the post's more.

The reader bundle is 58062 B gzip of 70000 B, up 859 B from 57203 B. All four criteria are met:
- Vitest: 1934 passed, including eight markUnread tracker tests and a test of the hold.
- Playwright: 185 passed at 390 px and 1440 px, including the two new read-tracking tests.
- Go: `go test -race ./...` passed. No server change was needed: the DELETE takes its user from the session.
