---
schema: 3
id: TKT-01M29CRJ4E4AZ079KEHVGJ01DS
title: Build the moderator queue
type: task
status: done
status_reason: null
priority: high
due_on: null
labels:
  - area:moderation
  - effort:L
assignees: []
milestone: phase-2
parent: TKT-01M29BN36JH4M1W5819D06PGPP
origin: null
dependencies:
  - TKT-01M29CP2PJ23SK6AN8M0CJY4JT
  - TKT-01M29CP2MGC6FD35PJHR2Y2WNA
  - TKT-01M3NRYCP5BSA717S1HANWY1AA
  - TKT-01M29CRJ5J9XMJZ30FMZX4D473
blocks_on: none
references:
  - ref: design:flags
    path: docs/08-moderation.md
  - ref: pr:183
    path: null
  - ref: pr:184
    path: null
  - ref: pr:185
    path: null
claim: null
archive: null
created_at: 2026-09-11T23:27:36Z
updated_at: 2026-09-30T01:48:40Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase2-run-crj4e
  name: ""
extensions: {}
---

## Description

`/admin/queue`: open flags and pending applications in one list, newest first, with counts in the moderator navigation.

One list rather than two is the point. A moderator checking in wants a single place that is either empty or not, and splitting the queue means one half gets ignored.

Actions resolve in place, and a resolved item leaves the queue while staying in the record.

## Acceptance criteria

- [x] GET /api/v1/admin/queue lists open flags, one item per flagged post, and pending applications in one list by newest time first, scoped to forums where the viewer has moderate (TestQueueOneListNewestFirst, TestQueueGroupsFlagsByPost, TestQueueScopesFlagsToModeratedForums)
- [x] Resolving, dismissing or removing an item closes its flags in place with resolver, time and note, drops it from the queue, and GetFlag and audit_log still return it (TestResolvedItemLeavesQueueAndStaysQueryable)
- [x] GET /me's queue equals the number of items the queue lists for the same viewer and is null for a viewer with no queue, and the top bar shows it as a badge (TestQueueCountMatchesList, Shell.test.tsx)
- [x] A reporter learns nothing from a resolution: no notification and no change to what their flag request answered (TestReporterLearnsNothing)
- [x] Dismissing every flag on an auto-hidden post restores it, and a post a moderator hid by hand stays hidden (TestDismissRestoresOnlyAnAutoHide)
- [x] A member granted moderate on one forum through a group can open and work the queue for that forum (TestForumScopedModeratorUsesQueue)
- [x] Queue actions hold TKT-01M29CP2PJ's per-post flag lock; two moderators acting on one item produce one closure, one flag audit row and at most one Delete, and the second gets conflict before touching the log (TestConcurrentResolveOneOutcome, TestQueueSharesTheFlagLock)
- [x] A dismissal restores an auto-hidden post only by dismissing every flag open under the lock, and a flag reported afterwards counts from zero and hides the post again only at the threshold (TestLateFlagAfterDismiss)
- [x] A failed log step leaves the post's flags open (the restore path reopens those it dismissed), an ErrPostState is conflict and never success, and the audit row records what the request did, including already hidden (TestFailedActionStaysInQueue, TestStateChangeIsConflictNotSuccess)
- [x] A dismissal's flag.dismissed row is written only after its Restore append succeeds, so a failed restore leaves reopened flags and no dismissal row (TestFailedRestoreWritesNoDismissal)
- [x] Keeping an auto-hidden post restores it through the same restore path as a dismissal, and keeping a post a moderator hid by hand is conflict (TestKeepAutoHiddenPostRestoresIt)
- [x] A forum-scoped moderator's /me queue is a number, computed through perm.ModeratesAny, and a member with no grant gets null at no extra statement (TestForumScopedMemberGetsQueueCount)
- [x] A Restore that is durable but whose flag.dismissed write failed answers 200, keeps its flags dismissed as PR #138 specifies, and has its row written on a retry or by FlagAuditCatchUp, exactly once (TestRestoredButUnauditedIsRecovered)

## Implementation plan

### Settled in grooming

- `/admin/queue` shows open flags and pending applications in one list, newest first, with counts in the moderator navigation (docs/08 "Flags").
- Auto-hide: a post with at least N open flags (default 3, `flags.auto_hide_threshold`) is hidden pending review, as `hidden-by-mod`, through a `Delete` with `auto=true`. Its author and moderators still see it, marked "hidden pending review". Who flagged it and how many did is visible only to moderators. Decided 2026-09-28, TKT-01M29CP2PJ, docs/08 "Flags".
- A moderator's delete of someone else's post is `hidden-by-mod`, and only a moderator restores it. Decided 2026-09-28, TKT-01M29CRJ5J, docs/08 "Restoring a moderator's delete".
- Reporters see "thanks, a moderator will look" and nothing else (TKT-01M29CP2PJ AC3). Resolving an item notifies no reporter.
- **Maintainer decision 2026-09-29:** dismissing every flag on an auto-hidden post restores it. The hide was pending review, and a review that dismisses the flags has found nothing to hide. A post a moderator hid by hand is not restored by a dismissal. This settles the open decision raised in grooming, and a note on this ticket records it.
- A flag is resolved or dismissed in place and never deleted. A trigger refuses a delete (docs/04 "flags"). `CloseFlag` closes only an open flag, so a second click finds nothing to change (`queries/moderation.sql`).
- Application decisions are TKT-01M29CP2MG's service and routes. The queue calls them and adds no second approval path. Approval never creates a session here: the applicant's next sign-in goes through `session.IssueUserWith` (docs/05).
- The gates, the audit helper and the admin scaffold come from TKT-01M3NRYCP5. The queue's routes use its `moderates` gate, which admits a forum-scoped moderator, and not `staff`, which admits by role only. The screen is in the lazy admin chunk. The badge is in the shell, which is reader code and counts against the budget. The built client is committed: run `go generate ./internal/web/spa`.

Engineering choices made here:
- **One item per flagged post**, holding all its open flags, rather than one item per flag. Three reports of one spam post are one decision. Per-flag rows would make a moderator clear the same post three times, and the badge would count reports rather than work. An item's time is its newest open flag.
- **Scope follows `moderate`.** Flags appear for forums where the viewer has `moderate` (`perm.CanEach`), so a member of the `moderators` group granted one forum sees that forum's flags, and `role = moderator` sees every forum they can read (docs/04 "permissions"). Applications appear when TKT-01M29CP2MG's authorisation lets the viewer review them. A viewer with neither is `forbidden`.
- **One per-post lock, shared with the auto-hide.** This was revised for Terva review 1356 (PR #140, head 2f8d7a1) and aligned with PR #138. Every queue action on a flagged post runs under TKT-01M29CP2PJ's `flagLocks.Lock(thread, ordinal)`, the striped per-post mutex that `EnforceAutoHide`, the auto-hide restore and `ConfirmHide` already hold. The previous revision's `queue_actions` claim table is dropped: two mechanisms serializing one post are two orders to get wrong. Under the lock the action reads the post's open flags and its index entry flags, and chooses what to do from that state. A second moderator waits on the lock, finds no open flags, and gets `conflict` before touching the log, which gives AC4 one outcome.
- **State decides, errors do not** (review 1356, finding 2). Each append carries the state read under the lock as its precondition (`Require`, `Refuse`). An `ErrPostState` from the append means something outside the lock, such as a moderator's delete from the thread page, changed the post after the read. The answer is then `conflict` with the flags left open, never success. A failed log step leaves the flags open: remove and resolve close them only after the append, and the restore path, which dismisses first so the sweep has nothing to act on, reopens exactly those ids when its append fails. So a failed action leaves the item in the queue with the post in whatever state it is really in. Trying again is the same request: it reads the state afresh. The audit row records what this request did (`post: "hidden" | "restored" | "already hidden" | "unchanged"`), not what was asked.
- **A dismissal restores only by closing every open flag** (review 1356, finding 1). The restore of an auto-hidden post is TKT-01M29CP2PJ's path, under the same lock. `DismissOpenFlagsForPost` closes every flag open at that moment, not a list captured earlier. The `Restore` is then appended, and exactly those ids are reopened if the append fails. A flag inserted after the dismissal is an ordinary open flag counted from zero (TKT-01M29CP2PJ's rule), so the post is visible because it is below the threshold, not while still pending review. If new flags reach the threshold, `EnforceAutoHide`, which waits on the same lock, hides the post again.
- **Audit rows.** The records the queue appends (a `Delete` with `by_moderator`, a `Restore`) get their `post.hidden` and `post.restored` rows from the projection writer (TKT-01M29CKRGS step 1 on PR #138), through `appendEvent`'s synchronous projection. The queue writes only the flag row, `flag.resolved` or `flag.dismissed` with `{flags, action, note, post}`, through `internal/audit`, and only once the action has succeeded, because `audit_log` is append-only and a row cannot be withdrawn. Where no reopen can follow, the row is written in the `db.Write` that closes the flags: resolve, a dismissal that restores nothing, and remove, whose flags close after the append. For that, this ticket gives TKT-01M29CP2PJ's `DismissOpenFlagsForPost` and `ResolveOpenFlagsForPost` an optional audit entry. The restore path is the exception, and it follows TKT-01M29CP2PJ as revised on PR #138 (head b75e369). The flags are dismissed before the `Restore`, and they are reopened only if `s.Writer.Append` itself fails. Once the append returns, the Restore is durable and the flags stay dismissed, even if the synchronous projection then fails, because catch-up applies the rest. The durable record of the action is the projection writer's `post.restored` row, written whenever the Restore is projected, together with the flags' own `resolved_by`, `resolved_at` and `resolution_note`. The `flag.dismissed` row is best-effort. It is written after the append succeeds, still under the per-post lock, and never before, so a failed append leaves no row (Terva review on PR #140, head 1c78b85). Recovery (Terva review on PR #140, head 075aff1): each flag gains a nullable `audit_id`, set in the same `db.Write` that writes the row naming it. Every close path sets it with its row, so only this path can leave closed flags with `audit_id` null. If that write fails after a durable Restore, the request answers 200 with the restored post, because the action happened, and logs a warning. The row is then written again on retry: a `ResolvePost` that finds no open flags first checks for closed flags on the post with `audit_id` null. If there are any, it writes their row with the original `resolved_by` and `resolved_at` and `"recovered": true`, and sets `audit_id`, whatever the request asked. It answers 200 only when the request's action is one the completed restore satisfies: `dismiss`, or `resolve` (Keep post), on the now-visible post. Any other action, such as `remove`, gets `conflict` as usual, so a request is never reported as having done what another did (Terva review on PR #140, head bed9b58). The job `jobs.FlagAuditCatchUp` (every minute, in `serve`'s background list) does the same for any such flags older than a minute, so nothing depends on a moderator retrying. Writing the row with the dismissal and then a compensating `flag.reopened` row lost, because the log would read as a dismissal that was undone when nothing happened. A pending-operation table lost to one nullable column, which marks exactly what is owed.
- **Counts ride on `/me`**, as `queue`, because the shell already reads `/me` on every page, and a second request per page would cost more than the one count query it carries.


### Settled at claim (2026-09-30, phase 2 run)

Carried requirements, from the notes and the orchestrator's brief:
- **Terva review 1371 (PR #140, head 4aafbf4), carried as a requirement.** Audit recovery uses an explicit pending marker, `flags.audit_pending`, set only in the queue's restore transaction, the one that dismisses the flags before the `Restore` append. A NULL `audit_id` alone never means a row is owed. `ResolvePost`'s retry and `FlagAuditCatchUp` consider only rows with the marker set. A test proves that a flag closed by any other path, or before the migration, is never swept.
- Dismissing every flag on an auto-hidden post restores it. "Keep post" restores an auto-hidden post, and answers conflict on a manual hide.
- The queue count on /me is for anyone `perm.ModeratesAny` admits.
- Forum-scoped moderators use the moderates gate, and the author link goes to /u/{handle} for them.
- A post-state error is a conflict, with the flags left open.
- A dismissal's audit row is written only after the Restore append succeeds.
- `Flag`, `EnforceAutoHide`, `SweepAutoHide`, a moderator's `Restore` and `ConfirmHide` hold `s.flagLocks` (PR #176). The queue takes the same lock and adds no second mechanism.
- Two PRs. PR 1 is the server half with no dist change; a types-only `dto.ts` or `dto-contract.ts` edit is allowed if the dist is unchanged. PR 2 is the client, opened only when the dist is free, and it closes the ticket.

Revisions to the plan below, made on reading main at 3e1ff35:
- **The queue lives in `internal/service/threads/queue.go`, not a new `moderation` package.** Its actions need the thread service's private flag lock, `access`, `changePostAppended` and `readBack`. The queue methods are therefore the exported wrapper around the lock, and nothing else can reach the lock. `users.Service` gets an optional `QueueCount` hook that `serve` wires to `threads.Service.QueueCount`, so `users` imports nothing new.
- **The pending marker holds the owed action.** `flags.audit_pending` is `NULL`, `'dismiss'` or `'resolve'`. A `CHECK` allows it only on a dismissed flag with no `audit_id`. `ReopenFlags` clears it, and so does the write of the row. The thread page's restore passes `NULL`, so its dismissals are never owed a queue row.
- **Recovery checks that the Restore landed.** A pending flag whose post is visible gets its `flag.dismissed` row with the original `resolved_by`, `resolved_at` and `"recovered": true`. A pending flag whose post is still hidden is left from a restore whose append failed and whose reopen also failed. Recovery reopens those flags instead, because no restore happened and the item belongs back in the queue.
- **Archived threads are not in the queue.** An archived thread is read-only, flags included (Terva review 1506, ConfirmHide). Its flags are left out of the list and the count, and a queue action on one is conflict. Nobody's queue holds an item that no action can clear.
- **One statement for all forums in perm.** `perm.ModeratedForums(v)` lists the forums v may moderate. It evaluates every forum in one statement (`ForumAccessRowsAll`), caches each forum's access and the list, and `ModeratesAny` uses it for a member. /me asks it before it counts. A member with no grant then pays one statement cold, and that statement also warms the read check /me makes next. So the cold budget stays at two, and warm costs nothing extra.
- **Items carry a thread reference** `{id, slug, title}` and a nullable post, which is null when the thread cannot be read now, such as when it is quarantined. `Queue` has no `counts` field: /me's `queue` is the count.
- **Migration 0015** (`0015_flag_audit.sql`). If another lane lands a 0015 first, this PR renumbers.

### Additional tests

- `TestOnlyPendingFlagsAreRecovered` (review 1371): flags closed by `ConfirmHide`, by the thread page's restore, and by a hand-written pre-migration close all have a null `audit_id`, and neither `ResolvePost` nor `FlagAuditCatchUp` writes a row for them or changes them.
- `TestModeratedForums`: the list follows role, grants and hidden forums, costs one statement cold and none warm, and a revoked grant applies on the next call.
- `TestMeBudget`, revised: a member with no grant still runs two handler statements cold (`ForumAccessRowsAll` and `MeCounts`) and one warm. A moderator adds `QueueCount`.
- `TestLateFlagAfterDismiss` inserts its late flag directly, because `Flag` holds the lock from check to insert (PR #176).

### Approach

This is grooming's approach. Where "Settled at claim" above revises it, the revision holds: the service is in `threads`, the marker is `audit_pending`, and the migration is 0015.

1. `internal/store/db/views/queue.go` (hand-written, since the forum list varies): `QueueItems(ctx, tx, forumIDs []int64, applications bool, before Cursor, limit)`. It is a `UNION ALL` of open flags grouped by `(thread_id, ordinal)` and joined to `threads` for `forum_id IN (…)`, taking `max(created_at)`, `count(*)` and `group_concat(id)`, with pending `applications`. The result is ordered by `created_at DESC, kind, key DESC`, and the keyset cursor is `(created_at, kind, key)`. `QueueCounts` uses the same WHERE clauses, so the badge and the list cannot disagree. The `flags_status` and `flags_post` indexes serve it.
2. Migration (next free number): `ALTER TABLE flags ADD COLUMN audit_id INTEGER REFERENCES audit_log (id)`, and the query `FlagsOwedAudit`. docs/04 "flags" gains the column. Otherwise there is no new table or query for actions: the flag closes are TKT-01M29CP2PJ's `DismissOpenFlagsForPost`, `ResolveOpenFlagsForPost` and `ReopenFlags`, and the lock is its `flagLocks`.
3. `internal/service/moderation/queue.go`: `Queue(ctx, v, cursor) (dto.Queue, error)`, `Count(ctx, v) (*int, error)` (nil for a viewer with no queue), and `ResolvePost(ctx, v, by, thread, ordinal, Resolution{Action, Note})` (`resolve`, `dismiss` or `remove`). The post must be in a forum where `v` may `moderate`. Under the post's lock, no open flags is `conflict`, and a request that finds closed flags still owed their audit row writes it first. It answers 200 only if its own action matches the completed restore (the recovery above). Otherwise the action is chosen from the entry flags:
   - `dismiss`. On an auto-hidden post (hidden-by-mod and auto-hidden) it takes TKT-01M29CP2PJ's moderator restore path: dismiss every open flag, append the `Restore`, and reopen those flags on failure. That gives `post: "restored"`, the maintainer's rule. On a visible post it dismisses the flags (`"unchanged"`). On a post a moderator hid by hand it dismisses the flags, and the post stays hidden (`"already hidden"`).
   - `remove`. On an auto-hidden post it takes `ConfirmHide` (TKT-01M29CP2PJ): a `Delete{ByModerator: true}` with `Require: EntryAutoHidden`, then `ResolveOpenFlagsForPost`. On a visible post it appends TKT-01M29CRJ5J's moderator `Delete{ByModerator: true}` with `Refuse: EntryDeleted|EntryHiddenByMod|EntryMoved`, then resolves the flags. Both give `"hidden"`. On a post already hidden or deleted it resolves the flags with `"already hidden"`. If the append succeeds but closing the flags fails, the answer is `internal`. The item stays, showing the post hidden, and the next remove resolves it as `"already hidden"` with no second `Delete`.
   - `resolve` ("Keep post"). On a visible post it resolves the flags, and the post is `"unchanged"`. On an auto-hidden post, keeping it means showing it again, so it takes the same restore path as `dismiss`, with `post: "restored"` (Terva review on PR #140, head 075aff1). The flags end `dismissed`, because TKT-01M29CP2PJ's restore path is the one way to lift an auto-hide without racing the sweep, and the audit row records `action: "resolve"`. On a post a moderator hid by hand, `resolve` is `conflict` with the message "Restore it from the thread page first", because keeping a post a moderator removed is a restore of that moderator's decision (TKT-01M29CRJ5J), not a queue shortcut.
   - An `ErrPostState` from any append is `conflict`, with the flags as they were ("State decides, errors do not" above).
4. `internal/dto`: `Queue{Items []QueueItem; NextCursor string; Counts QueueCounts}` and `QueueItem{Kind string; CreatedAt int64; Flag *QueueFlag; Application *Application}`. `QueueFlag` is `{thread ThreadRef, ordinal, post Post}`, with the post as the moderator view draws it, `hidden bool`, and `flags [{id, reason, note, reporter Author, created_at}]`. `Application` is TKT-01M29CP2MG's DTO, reused. Goldens: `queue.moderator.json` with both kinds, and `me.moderator.json` updated.
5. `/me`: `dto.Me.Queue *int` (`json:"queue"`) is null unless `moderation.Count` says the viewer has a queue. It is computed for every active member for whom `perm.ModeratesAny` is true (TKT-01M3NRYCP5): moderators and admins by role, and members granted `moderate` on a forum through a group (Terva review on PR #140, head 075aff1). `ModeratesAny` answers from the permission cache that `/me` already warms, so a member with no grant pays no extra statement. A viewer who moderates somewhere pays one count statement. Update the GET /me row in docs/07, which counts statements, and the goldens `me.member.json` (null) and `me.moderator.json` (a number).
6. Routes in `internal/web/api/admin_queue.go`, all behind `moderates`: `GET /api/v1/admin/queue?cursor=`, and `POST /api/v1/admin/queue/posts/{thread}/{ordinal}` with `{action, note?}`. The POST is JSON-only, charged to the write limiter, and `no-store`. Applications use TKT-01M29CP2MG's routes.
7. Reader client (budgeted): `state/viewer.ts` `badges` gains `queue`. `TopBar.tsx` shows the existing queue link when `me.queue !== null`, rather than by role, with `<Badge n={badges.queue} what="items to review">`. The phone layout shows the same badge wherever it offers the queue. It is not a sixth tab, because docs/10 item 25 fixes five.
8. Admin client: `screens/admin/QueueScreen.tsx`. Each item shows the post, its flags with reasons, reporters and notes, and three buttons: Remove post, Keep post, which restores the post if it is hidden pending review, and Dismiss flags. An application item shows its summary and the approve and deny controls from TKT-01M29CP2MG. On success the item leaves the list and `refreshViewer()` updates the badge. Each item shows the post's state as it is: visible, hidden pending review, or hidden by a moderator. After a failed action the moderator sees the state that action left. On `conflict` the screen says "Another moderator has already handled this" and refetches. For a viewer whose role is moderator or admin, the author links to `/admin/users?prefix={handle}`, the users list's documented filter, for a suspension (TKT-01M29CP2QK). A forum-scoped moderator cannot open that screen, which is behind `staff`, so for them the author links to the profile `/u/{handle}` instead (Terva review on PR #140, head bed9b58).
9. Docs: docs/07 "Moderation and admin" documents the queue endpoints. docs/08 "Flags" records the one-item-per-post grouping, the shared per-post lock, the state-decided actions, and the maintainer's dismiss rule.
10. Run `go generate ./internal/web/spa`, commit `dist`, and run the budget check. The reader delta is the badge only, expected under 200 B gzip. Record the new total in the PR.

### Tests

- `TestQueueOneListNewestFirst`: flags and applications interleave by time, and paging repeats nothing.
- `TestQueueGroupsFlagsByPost`: three flags on one post make one item with three flags.
- `TestQueueScopesFlagsToModeratedForums`: a `moderators`-group member granted forum A sees A's flags and not B's. `role = moderator` sees every forum they can read, but no flags in a restricted forum they cannot read. A plain member is `forbidden`.
- `TestQueueCountMatchesList`: `/me`'s `queue` equals the items across every page, for three viewers, and is null for a plain member.
- `TestResolvedItemLeavesQueueAndStaysQueryable`: after each action the item is gone, `GetFlag` returns the closed flag with resolver, time and note, and the audit row names the actor.
- `TestConcurrentResolveOneOutcome` (under `-race`): two moderators act on one post at once, with every pair of actions. One gets 200. The other waits on the per-post lock, finds no open flags, and gets 409 before touching the log. There is one flag audit row, and at most one `Delete` in the log.
- `TestReporterLearnsNothing`: after resolution the reporter's `/me` and notifications are unchanged.
- `TestQueueRemoveMovesTheCacheKey`: a thread page cached for a member before a remove shows the tombstone after it, because the append moved the log length (docs/07 "The response cache").
- `TestDismissRestoresOnlyAnAutoHide`: dismissing every flag on an auto-hidden post appends a `Restore`. On a post a moderator hid by hand it appends nothing.
- `TestFailedActionStaysInQueue`: with the moderator `Delete` made to fail, the flags stay open, no flag audit row is written, and the item is listed with the post visible. With `ResolveOpenFlagsForPost` made to fail after the `Delete`, the item is listed with the post hidden, and a second remove resolves it as `"already hidden"` and appends no second `Delete`.
- `TestFailedRestoreWritesNoDismissal`: with the `Restore` append made to fail on an auto-hidden post, the flags are reopened and `audit_log` has no `flag.dismissed` row for them. With the append succeeding, exactly one `flag.dismissed` row is written after it, naming the dismissed ids and `post: "restored"`.
- `TestKeepAutoHiddenPostRestoresIt`: `resolve` on an auto-hidden post appends a `Restore`, the post is visible, and the item leaves the queue. On a post a moderator hid by hand it is `conflict` and appends nothing.
- `TestRestoredButUnauditedIsRecovered`: with the `flag.dismissed` write made to fail after a durable Restore, the answer is 200, the flags stay dismissed with `audit_id` null, and the `post.restored` row exists. A retried `ResolvePost` writes the row with the original resolver, sets `audit_id`, and answers 200. `FlagAuditCatchUp` does the same without a retry, and neither writes a second row. A retried `remove` in that state writes the owed row and still answers `conflict`.
- `QueueScreen.test.tsx` also checks that the author links to `/admin/users?prefix=` for a role moderator and to `/u/{handle}` for a forum-scoped moderator.
- `TestForumScopedMemberGetsQueueCount`: a member granted `moderate` on one forum through a group gets a numeric `/me` `queue` equal to that forum's items. A member with no grant gets null and costs no extra statement.
- `TestStateChangeIsConflictNotSuccess`: a moderator's thread-page delete lands between the queue's read and its append, held at a barrier. The queue answers `conflict`, the flags stay open, and no row claims `"hidden"` for the queue's action.
- `TestLateFlagAfterDismiss`: with the lock held after `DismissOpenFlagsForPost`, a new flag is inserted. The post is restored, and the new flag is open and counts 1. The item stays in the queue showing a visible post. When flags reach the threshold again, `EnforceAutoHide`, waiting on the lock, hides the post.
- `TestQueueSharesTheFlagLock`: a queue dismissal of an auto-hidden post and `AutoHideSweep` run concurrently, and the sweep never hides the post again from flags the dismissal closed.
- `TestForumScopedModeratorUsesQueue`: a member granted `moderate` on one forum through a group reaches `GET /api/v1/admin/queue` through the `moderates` gate, sees that forum's items, and can resolve them. Their `/me` `queue` is not null.
- Client: `QueueScreen.test.tsx` covers resolve, conflict and application items. `Shell.test.tsx` checks the badge appears only when `queue` is not null. `TestTheAdminBundleIsSeparate` stays green.

### Out of scope

- Creating flags and the auto-hide itself (TKT-01M29CP2PJ).
- The application review screen, its decision service and its routes (TKT-01M29CP2MG).
- The moderator delete and restore primitives (TKT-01M29CRJ5J).
- Suspending an author (TKT-01M29CP2QK). The queue only links to it.
- Notifying moderators of new items (TKT-01M29CP2N0).

## Notes

**agent:claude-code/phase2-groom** at 2026-09-29T05:13:04Z

Open decision for the maintainer (grooming, 2026-09-29): does dismissing the flags on an auto-hidden post restore it? (a) yes, when the hide was the auto-hide (a Delete with auto=true), because docs/08 says the post is hidden pending review; a moderator's own hide stays. (b) no, and restoring is a separate action. Recommended: (a). Under (b), a hidden post with no open flags is in nobody's queue. See the plan's Open decision.

**agent:claude-code/phase2-groom** at 2026-09-29T05:41:12Z

Maintainer decision 2026-09-29: dismissing every flag on an auto-hidden post restores it. This supersedes the open decision in the earlier note. The plan also now claims a post, acts on the log, and then completes, with Retry and Abandon for an action that fails part-way (Terva review 1349, PR #140). A forum-scoped moderator reaches the queue through TKT-01M3NRYCP5's moderates gate.

**agent:claude-code/phase2-groom** at 2026-09-29T06:04:50Z

Terva review 1356 (PR #140, head 2f8d7a1): both findings accepted and fixed in the plan. The queue_actions claim table is dropped. Queue actions now hold TKT-01M29CP2PJ's per-post flag lock (PR #138) and use its restore and ConfirmHide paths. Each action is chosen from the state read under the lock, so an ErrPostState is conflict, never success. A dismissal restores only by dismissing every open flag, so a later flag counts from zero. Audit rows for the log records come from the projection writer (TKT-01M29CKRGS).

**agent:claude-code/phase2-groom** at 2026-09-29T06:27:44Z

Terva review on PR #140, head 1c78b85, medium: accepted and fixed. On the restore path, the flag.dismissed row is now written after the Restore append succeeds, still under the per-post lock, so a failed restore that reopens its flags leaves no row (TestFailedRestoreWritesNoDismissal).

**agent:claude-code/phase2-groom** at 2026-09-29T06:39:32Z

Terva review on PR #140, head 075aff1: three medium findings, all accepted and fixed in the plan. (1) Keep post on an auto-hidden post restores it through the dismissal's restore path; on a post a moderator hid by hand it is conflict. (2) The /me queue count runs for anyone perm.ModeratesAny admits, forum-scoped members included. (3) Aligned with PR #138 head b75e369: once the Restore append succeeds, the flags stay dismissed. post.restored is the durable record. The flag.dismissed row is best-effort, tracked by a new flags.audit_id column, and written again on retry or by jobs.FlagAuditCatchUp.

**agent:claude-code/phase2-groom** at 2026-09-29T06:45:43Z

Terva review on PR #140, head bed9b58: two medium findings, both accepted and fixed in the plan. (1) Audit recovery writes the owed flag.dismissed row whatever the request asked, and answers 200 only when the request's own action (dismiss, or resolve/Keep post) matches the completed restore; anything else, remove included, is conflict. (2) The author link goes to /admin/users?prefix={handle} only for a role moderator or admin; a forum-scoped moderator, who cannot pass staff, gets the profile /u/{handle}.

**agent:claude-code/phase1-run-438e26c1** at 2026-09-29T07:04:34Z

Carried from Terva review 1371 on PR #140 (head 4aafbf4), medium, 'Do not treat existing closed flags as audit rows owed by recovery'. The plan's round cap was reached, so this is carried here as a requirement, not by another plan revision. Audit recovery must use an explicit pending marker, for example a flags.audit_pending column set only in the restore transaction that closes the flags. A NULL audit_id alone never means an audit row is owed. ResolvePost's retry and FlagAuditCatchUp consider only rows with the marker set. Add a test that a flag closed by any other path, or before the migration, is never swept.

**agent:claude-code/phase2-run-crj4e** at 2026-09-30T00:49:11Z

Server half built as PR 1, with no change to the built client (go generate ./internal/web/spa leaves internal/web/spa/dist identical). The criteria it earns are ticked. AC3 waits for the top bar badge and Shell.test.tsx, and AC6 for the queue screen, both in PR 2.

Where the build differs from the plan, and why:
- The service is `threads.Service.Queue`, `QueueCount`, `ResolvePost` and `FlagAuditCatchUp` in internal/service/threads/queue.go, not a new moderation package. The actions need the private flag lock and the append helpers, and making the queue the only exported way to take the lock keeps it one mechanism.
- The requirement carried from Terva review 1371 is met with `flags.audit_pending` (migration 0015). It is NULL, 'dismiss' or 'resolve', and set only in the queue's restore transaction. A CHECK allows it only on a dismissed flag with no audit_id. TestOnlyPendingFlagsAreRecovered covers flags closed by ConfirmHide, by the thread page's restore, and by hand before the migration, and none is swept. If a pending flag's Restore never landed (its append failed, and so did the reopen), recovery reopens the flag instead of writing a row. It tells a landed Restore by the post being shown now, or by the projection's post.restored row from the same moderator since the dismissal.
- AC12's "computed through perm.ModeratesAny" is `perm.ModeratedForums`, which ModeratesAny now answers from for members (len > 0), so the predicate is the same. It reads every forum in one statement (ForumAccessRowsAll), and /me asks it first, so that one statement also warms the read checks. A member with no grant runs two handler statements cold and one warm, as before. A moderator runs one QueueCount statement more (TestMeBudget).
- Archived threads are left out of the list and the count, and a queue action on one is conflict. This follows the rule from Terva review 1506 (an archived thread is read-only, flags included), and it keeps an item no action can clear out of the queue. Recorded in docs/08.
- `Me.queue` is optional in dto.ts until PR 2 reads it, so bootstrap.ts's anonymous default, which is bundled source, does not change in PR 1.

**agent:claude-code/phase2-run-crj4e** at 2026-09-30T00:57:01Z

PR #184 replaces PR #183, which main moved under (PR #182, archived forums). The same server half is carried onto main at b41b6db as one commit. ForumAccessRowsAll now selects forums.archived, as ForumAccessRows does, so the decisions perm.ModeratedForums caches keep an archived forum read-only (TestModeratedForumsCachesWhatCheckDecides). PR #183 was closed unmerged, before any review completed: its review run was superseded when main moved.

**agent:claude-code/phase2-run-crj4e** at 2026-09-30T01:12:44Z

Terva review 1522 on PR #184 (review of the server half), with profile code and model gpt-6-sol. It ran as [Actions run #1173](https://git.local.sothr.com/terva-sh/ketju/actions/runs/1173) with request `ready-review-c779429`. It reviewed head c7794292f6b7a772f25f404718341aa8daf41b85 against base b41b6dbe0966ecb9a507d852d3070e8707c7deec. The result was failure, with three medium findings. All three are fixed; none was declined.

### finding-1: Resolve and dismiss can report a stale post state (fixed)

A dismiss or resolve on a shown post, or on a post already down, closed the flags from the state read under the lock. Nothing was appended, so the post state was never checked again.

Fix, in two parts:
- `setDeleted`, the thread page's Delete and Restore of one post, now holds the post's flag lock. The moderator's restore already held it through `restoreDismissingFlags`.
- `closeFlags` now takes the state the action was decided on. It reads the entry again while holding the thread, just before the write, and a changed state answers conflict with the flags left open.

What can still land outside the lock is a suspension's `HidePostsOf` or `RestorePostsOf`. Those walk an author's threads inside `Threads.With`, where the flag lock may not be taken, because the rule is flag lock before thread. The re-read narrows that case to the write itself.

`stateAny` skips the re-read for the close after the queue's own Delete. That Delete is durable, and the row says `hidden`.

Tests:
- `TestStateChangeIsConflictNotSuccess` now covers all three actions. The intervening change is a raw moderator Delete appended to the log, as a suspension writes it. The previous `svc.Delete` in the hook would now wait on the lock the queue holds.
- New `TestThreadPageDeleteWaitsForTheQueue`.

### finding-2: A moderator removing their own flagged post is recorded as an author deletion (fixed)

The queue's remove on a shown post now always writes `ByModerator: true`, as the auto-hidden case already did. The thread page's delete of one's own post is still an author's delete. I updated the `DeleteBody.ByModerator` comment and docs/08 to match.

New test: `TestQueueRemoveOfOwnPostIsAModeratorsHide`.

### finding-3: Recovery can attribute someone else's restore to a failed queue action (fixed)

Recovery no longer takes "the post is shown" as proof, and the post.restored audit-row check is gone. That row was never enough anyway: the projection writes no post.restored for a moderator restoring their own post.

Each queue Restore is now stamped with the dismissal's millisecond (`resolved_at`) and the moderator as its actor. Recovery looks for exactly that record in the post's log with the new `threadlog.Reader.Restored(from, upTo, ordinal, actor, ms)`, which walks the log the way `Removals` does. Recovery is rare, so the walk is acceptable. `AuditExistsSince` is removed.

Recovery now returns the ids it wrote, grouped by moderator. A request gets 200 `restored` only if the recovered restore was its own moderator's; any other request is conflict. The row is still written, crediting the moderator who made the restore.

Tests:
- New `TestRecoveryIgnoresSomebodyElsesRestore`, covering the catch-up and a retry.
- New "another moderator's request" subtest in `TestRestoredButUnauditedIsRecovered`. Its "a retry" subtest now retries as the same moderator.
- New `TestRestored` in threadlog.

### Mutations tried, all caught

- The re-read removed.
- The lock in `setDeleted` removed.
- `ByModerator` back to `author != viewer`.
- Visibility taken as proof of landing.
- The retry answer taken from any moderator's recovered restore.

**agent:claude-code/phase2-run-crj4e** at 2026-09-30T01:15:53Z

Terva review 1524 on PR #184 ran as [Actions run #1180](https://git.local.sothr.com/terva-sh/ketju/actions/runs/1180) with request `ready-review-2baf5fd`. It reviewed head 2baf5fde6384429d4a211b0d0b207bba2e7b2927 against base b41b6dbe0966ecb9a507d852d3070e8707c7deec. The result was failure. It called all three findings from review 1522 resolved and raised one new medium finding.

### finding-1: Require the visible state for a queue removal (fixed)

The model read the visible-post remove's `Refuse: gone` as refusing only deleted or moved posts. That reading was wrong. `gone` (mutate.go:37) is `EntryDeleted | EntryHiddenByMod | EntryMoved`, so any hide already made the append ErrPostState. `TestStateChangeIsConflictNotSuccess/remove` showed this, with a moderator's hide landing between the read and the append.

The definition sits outside the diff, so I spelled the precondition out instead of arguing:
- It is now `EntryDeleted | EntryHiddenByMod | EntryAutoHidden | EntryMoved`, with a comment tying it to the shown state the remove was decided on.
- `TestStateChangeIsConflictNotSuccess` now also covers the system's auto-hide as the intervening change, for every action. It also checks that the only Delete is the hide's.
- Mutation tried: `Refuse: EntryDeleted | EntryMoved`. It was caught.

**agent:claude-code/phase2-run-crj4e** at 2026-09-30T01:25:52Z

PR #184, the server half, merged at 034053c032f67c6252e9a7567d20d6319a55b334 by rebase. It went in as a fast-forward: its base, b41b6dbe0966ecb9a507d852d3070e8707c7deec, was still main's tip, so the commit SHAs are the branch's.

- The clean Terva review was run `f9d48fd9-ad31-4c12-9f2c-c3825cc75358` for `request:ready-review-034053c`, in [Actions run #1182](https://git.local.sothr.com/terva-sh/ketju/actions/runs/1182). It reviewed head 034053c against base b41b6db and found nothing, saying review 1524's finding was resolved.
- CI was `ci / Build and Test (pull_request)` [run 1181](https://git.local.sothr.com/terva-sh/ketju/actions/runs/1181). I read the step log: every step succeeded, including go test (race), the Playwright smoke and "ticket store is consistent".
- I deleted branch `run/crj4e-moderator-queue-2` after `merged` read true.
- `run/crj4e-moderator-queue`, the branch of the closed #183, is left on the remote, because #183 never merged.

The client half follows as PR 2 on `run/crj4e-queue-screen-2`, built directly on 034053c.

**agent:claude-code/phase2-run-crj4e** at 2026-09-30T01:37:46Z

Terva review 1526 on PR #185 (the client half) ran as [Actions run #1189](https://git.local.sothr.com/terva-sh/ketju/actions/runs/1189) with request `ready-review-4410f31`. It reviewed head 4410f31b83957220c716f11bda092f84c232da71 against base 034053c032f67c6252e9a7567d20d6319a55b334. The result was failure, with two medium findings. Both are fixed.

### finding-1: Handle conflicts on moderator-hidden posts as conflicts (fixed)

Every 409 now goes to the handled notice ("Another moderator has already handled this.") and reloads the queue. The old `state !== 'moderator'` exception was meant for Keep's conflict on a hand-hidden post, but the screen never offers Keep there, so the exception only swallowed real conflicts from Dismiss. New test: "says another moderator got there first on a post a moderator hid, too".

### finding-2: Show post attachments in flag items (fixed)

A flag item now renders the post's files with the thread view's `Attachments` component (components/thread/Post.tsx), as links to their blobs. An attachment-only post no longer says it has nothing to show. New test: "shows the files of a post with no words".

The built client is regenerated. The thread-view bundle is 45708 B gzip against a 60000 B budget. All 1433 client tests pass.

**agent:claude-code/phase2-run-crj4e** at 2026-09-30T01:48:40Z

PR #185 passed a clean Terva review: run `2e8e6684-de66-40ab-b752-88c7b66bf17a` for `request:ready-review-e2dd240`, in [Actions run #1199](https://git.local.sothr.com/terva-sh/ketju/actions/runs/1199). It reviewed head e2dd2406e0e36b78ac8cfe944706eb4d0309bdc4 against base 034053c032f67c6252e9a7567d20d6319a55b334, and said both findings from review 1526 were resolved.

CI on that head was `ci / Build and Test (pull_request)` [run 1198](https://git.local.sothr.com/terva-sh/ketju/actions/runs/1198). I read the step log: every step succeeded, including the Playwright smoke, go test (race), "built client is current" and the bundle budget.

Main moved to 2fc352f in the meantime. That commit only changed TKT-01M3M9RR26's draft plans, other tickets' files, so the rebase merge replays this branch without conflict. This note is a ticket-only commit, so it takes a carried review.

## Summary

The moderator queue landed in two PRs.

### PR #184: the server half

It merged at 034053c, after Terva reviews 1522 and 1524. It replaced #183, which was superseded when main moved.

- **Routes.** `GET /api/v1/admin/queue` and `POST /api/v1/admin/queue/posts/{id}/{ordinal}` sit behind the moderates gate. The threads service scopes them by `perm.ModeratedForums`.
- **/me.** `GET /me` gains `queue`, which is null for anyone `perm.ModeratesAny` does not admit.
- **Locking.** Every action holds the post's flag lock. So do the thread page's delete and restore of one post.
- **State checks.** An action decides from the state it read under that lock. A change that lands anyway is conflict with the flags left open:
  - for an append, through the append's precondition;
  - for a close with no append, through a second read just before the write.
- **Dismiss and Keep.** On a post hidden pending review, both restore it through the moderator's restore path. Keeping a post a moderator hid by hand is conflict.
- **Remove.** It is always a moderator's hide.
- **Migration 0015.** It adds `flags.audit_id` and `flags.audit_pending`.
  - The queue's restore marks its dismissed flags pending, and writes the `flag.dismissed` row only after the Restore append succeeds.
  - Recovery (a retry, or the `FlagAuditCatchUp` job) settles only marked flags. It finds the queue's own Restore in the post's log by moderator and millisecond (`threadlog.Reader.Restored`). If that Restore landed, it writes the row marked `recovered`. If not, it reopens the flags.

### PR 2: the client half

- The top bar's queue button and badge appear at both widths whenever `queue` is a number.
- `/admin/queue` lists flag items with Remove post, Keep post and Dismiss flags, and application items with the Decision controls.
- `e2e/admin-queue.spec.ts` covers the screen at 390 px and 1440 px.

All thirteen criteria are ticked.
