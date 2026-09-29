---
schema: 3
id: TKT-01M29CRJ4E4AZ079KEHVGJ01DS
title: Build the moderator queue
type: task
status: draft
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
claim: null
archive: null
created_at: 2026-09-11T23:27:36Z
updated_at: 2026-09-29T07:04:34Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase1-run-438e26c1
  name: ""
extensions: {}
---

## Description

`/admin/queue`: open flags and pending applications in one list, newest first, with counts in the moderator navigation.

One list rather than two is the point. A moderator checking in wants a single place that is either empty or not, and splitting the queue means one half gets ignored.

Actions resolve in place, and a resolved item leaves the queue while staying in the record.

## Acceptance criteria

- [ ] GET /api/v1/admin/queue lists open flags, one item per flagged post, and pending applications in one list by newest time first, scoped to forums where the viewer has moderate (TestQueueOneListNewestFirst, TestQueueGroupsFlagsByPost, TestQueueScopesFlagsToModeratedForums)
- [ ] Resolving, dismissing or removing an item closes its flags in place with resolver, time and note, drops it from the queue, and GetFlag and audit_log still return it (TestResolvedItemLeavesQueueAndStaysQueryable)
- [ ] GET /me's queue equals the number of items the queue lists for the same viewer and is null for a viewer with no queue, and the top bar shows it as a badge (TestQueueCountMatchesList, Shell.test.tsx)
- [ ] A reporter learns nothing from a resolution: no notification and no change to what their flag request answered (TestReporterLearnsNothing)
- [ ] Dismissing every flag on an auto-hidden post restores it, and a post a moderator hid by hand stays hidden (TestDismissRestoresOnlyAnAutoHide)
- [ ] A member granted moderate on one forum through a group can open and work the queue for that forum (TestForumScopedModeratorUsesQueue)
- [ ] Queue actions hold TKT-01M29CP2PJ's per-post flag lock; two moderators acting on one item produce one closure, one flag audit row and at most one Delete, and the second gets conflict before touching the log (TestConcurrentResolveOneOutcome, TestQueueSharesTheFlagLock)
- [ ] A dismissal restores an auto-hidden post only by dismissing every flag open under the lock, and a flag reported afterwards counts from zero and hides the post again only at the threshold (TestLateFlagAfterDismiss)
- [ ] A failed log step leaves the post's flags open (the restore path reopens those it dismissed), an ErrPostState is conflict and never success, and the audit row records what the request did, including already hidden (TestFailedActionStaysInQueue, TestStateChangeIsConflictNotSuccess)
- [ ] A dismissal's flag.dismissed row is written only after its Restore append succeeds, so a failed restore leaves reopened flags and no dismissal row (TestFailedRestoreWritesNoDismissal)
- [ ] Keeping an auto-hidden post restores it through the same restore path as a dismissal, and keeping a post a moderator hid by hand is conflict (TestKeepAutoHiddenPostRestoresIt)
- [ ] A forum-scoped moderator's /me queue is a number, computed through perm.ModeratesAny, and a member with no grant gets null at no extra statement (TestForumScopedMemberGetsQueueCount)
- [ ] A Restore that is durable but whose flag.dismissed write failed answers 200, keeps its flags dismissed as PR #138 specifies, and has its row written on a retry or by FlagAuditCatchUp, exactly once (TestRestoredButUnauditedIsRecovered)

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

### Approach

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
