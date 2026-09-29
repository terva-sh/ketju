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
updated_at: 2026-09-29T05:13:04Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase2-groom
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
- [ ] Two moderators acting on one item at once produce one closure, one audit row and at most one Delete; the second gets conflict and the screen says another moderator handled it (TestConcurrentResolveOneOutcome)
- [ ] A reporter learns nothing from a resolution: no notification and no change to what their flag request answered (TestReporterLearnsNothing)

## Implementation plan

### Settled in grooming

- `/admin/queue` shows open flags and pending applications in one list, newest first, with counts in the moderator navigation (docs/08 "Flags").
- Auto-hide: a post with at least N open flags (default 3, `flags.auto_hide_threshold`) is hidden pending review, as `hidden-by-mod`, through a `Delete` with `auto=true`. Its author and moderators still see it, marked "hidden pending review". Who flagged it and how many did is visible only to moderators. Decided 2026-09-28, TKT-01M29CP2PJ, docs/08 "Flags".
- A moderator's delete of someone else's post is `hidden-by-mod`, and only a moderator restores it. Decided 2026-09-28, TKT-01M29CRJ5J, docs/08 "Restoring a moderator's delete".
- Reporters see "thanks, a moderator will look" and nothing else (TKT-01M29CP2PJ AC3). Resolving an item notifies no reporter.
- A flag is resolved or dismissed in place and never deleted. A trigger refuses a delete (docs/04 "flags"). `CloseFlag` closes only an open flag, so a second click finds nothing to change (`queries/moderation.sql`).
- Application decisions are TKT-01M29CP2MG's service and routes. The queue calls them and adds no second approval path. Approval never creates a session here: the applicant's next sign-in goes through `session.IssueUserWith` (docs/05).
- The gate, the audit helper and the admin scaffold come from TKT-01M3NRYCP5. The screen is in the lazy admin chunk. The badge is in the shell, which is reader code and counts against the budget. The built client is committed: run `go generate ./internal/web/spa`.

Engineering choices made here:
- **One item per flagged post**, holding all its open flags, rather than one item per flag. Three reports of one spam post are one decision. Per-flag rows would make a moderator clear the same post three times, and the badge would count reports rather than work. An item's time is its newest open flag.
- **Scope follows `moderate`.** Flags appear for forums where the viewer has `moderate` (`perm.CanEach`), so a member of the `moderators` group granted one forum sees that forum's flags, and `role = moderator` sees every forum they can read (docs/04 "permissions"). Applications appear when TKT-01M29CP2MG's authorisation lets the viewer review them. A viewer with neither is `forbidden`.
- **Claim first, then act.** An action closes the post's open flags in one `UPDATE … WHERE status = 'open'` before anything touches the log. Zero rows means another moderator got there, and the answer is `conflict` with nothing appended. That gives AC4 one outcome without a lock across the database and the log.
- **Counts ride on `/me`**, as `queue`, because the shell already reads `/me` on every page, and a second request per page would cost more than the one count query it carries.

### Approach

1. `internal/store/db/views/queue.go` (hand-written, since the forum list varies): `QueueItems(ctx, tx, forumIDs []int64, applications bool, before Cursor, limit)`. It is a `UNION ALL` of open flags grouped by `(thread_id, ordinal)` and joined to `threads` for `forum_id IN (…)`, taking `max(created_at)`, `count(*)` and `group_concat(id)`, with pending `applications`. The result is ordered by `created_at DESC, kind, key DESC`, and the keyset cursor is `(created_at, kind, key)`. `QueueCounts` uses the same WHERE clauses, so the badge and the list cannot disagree. The `flags_status` and `flags_post` indexes serve it.
2. Query `CloseOpenPostFlags :execrows` in `queries/moderation.sql`: `UPDATE flags SET status = ?, resolved_by = ?, resolved_at = ?, resolution_note = ? WHERE thread_id = ? AND ordinal = ? AND status = 'open'`.
3. `internal/service/moderation/queue.go`: `Queue(ctx, v, cursor) (dto.Queue, error)`, `Count(ctx, v) (*int, error)` (nil for a viewer with no queue), and `ResolvePost(ctx, v, by, thread, ordinal, Resolution{Action, Note})`, where `Action` is `resolve`, `dismiss` or `remove`:
   - All three first close the post's open flags in one `db.Write`: `resolved` for resolve and remove, `dismissed` for dismiss. They append one `audit` row, `flag.resolved` or `flag.dismissed`, with the flag ids, the action and the note. Zero rows is `conflict`.
   - `remove` then appends a moderator `Delete` through the threads service's moderator path (TKT-01M29CRJ5J). An `ErrPostState` there, because the post is already hidden or deleted, is success. Any other append error is logged and returned as `internal`: the flags stay closed, and the moderator can remove the post from the thread page.
   - `dismiss` and an auto-hidden post: see the open decision. The recommended build appends a moderator `Restore` when the post's `hidden-by-mod` came from a `Delete` with `auto=true`, and leaves it alone otherwise.
4. `internal/dto`: `Queue{Items []QueueItem; NextCursor string; Counts QueueCounts}` and `QueueItem{Kind string; CreatedAt int64; Flag *QueueFlag; Application *Application}`. `QueueFlag` is `{thread ThreadRef, ordinal, post Post}`, with the post as the moderator view draws it, `hidden bool`, and `flags [{id, reason, note, reporter Author, created_at}]`. `Application` is TKT-01M29CP2MG's DTO, reused. Goldens: `queue.moderator.json` with both kinds, and `me.moderator.json` updated.
5. `/me`: `dto.Me.Queue *int` (`json:"queue"`) is null unless `moderation.Count` says the viewer has a queue. It adds one statement, and a perm read while the cache is cold, for staff only. Update the GET /me row in docs/07, which counts statements, and the goldens `me.member.json` (null) and `me.moderator.json` (a number).
6. Routes in `internal/web/api/admin_queue.go`: `GET /api/v1/admin/queue?cursor=`, and `POST /api/v1/admin/queue/posts/{thread}/{ordinal}` with `{action, note?}`. The second is JSON-only, charged to the write limiter, and `no-store`. Applications use TKT-01M29CP2MG's routes.
7. Reader client (budgeted): `state/viewer.ts` `badges` gains `queue`. `TopBar.tsx` shows the existing queue link when `me.queue !== null`, rather than by role, with `<Badge n={badges.queue} what="items to review">`. The phone layout shows the same badge wherever it offers the queue. It is not a sixth tab, because docs/10 item 25 fixes five.
8. Admin client: `screens/admin/QueueScreen.tsx`. Each item shows the post, its flags with reasons, reporters and notes, and three buttons: Remove post, Resolve (keep post) and Dismiss flags. An application item shows its summary and the approve and deny controls from TKT-01M29CP2MG. On success the item leaves the list and `refreshViewer()` updates the badge. On `conflict` the screen says "Another moderator has already handled this" and refetches. The author links to `/admin/users?q={handle}` for a suspension (TKT-01M29CP2QK).
9. Docs: docs/07 "Moderation and admin" documents the queue endpoints. docs/08 "Flags" records the one-item-per-post grouping and the dismiss rule, once decided.
10. Run `go generate ./internal/web/spa`, commit `dist`, and run the budget check. The reader delta is the badge only, expected under 200 B gzip. Record the new total in the PR.

### Tests

- `TestQueueOneListNewestFirst`: flags and applications interleave by time, and paging repeats nothing.
- `TestQueueGroupsFlagsByPost`: three flags on one post make one item with three flags.
- `TestQueueScopesFlagsToModeratedForums`: a `moderators`-group member granted forum A sees A's flags and not B's. `role = moderator` sees every forum they can read, but no flags in a restricted forum they cannot read. A plain member is `forbidden`.
- `TestQueueCountMatchesList`: `/me`'s `queue` equals the items across every page, for three viewers, and is null for a plain member.
- `TestResolvedItemLeavesQueueAndStaysQueryable`: after each action the item is gone, `GetFlag` returns the closed flag with resolver, time and note, and the audit row names the actor.
- `TestConcurrentResolveOneOutcome` (under `-race`): two moderators act on one post at once. One gets 200 and one gets 409. There is one audit row, and at most one `Delete` in the log.
- `TestReporterLearnsNothing`: after resolution the reporter's `/me` and notifications are unchanged.
- `TestQueueRemoveMovesTheCacheKey`: a thread page cached for a member before a remove shows the tombstone after it, because the append moved the log length (docs/07 "The response cache").
- `TestDismissRestoresOnlyAnAutoHide`, if the decision below is taken as recommended.
- Client: `QueueScreen.test.tsx` covers resolve, conflict and application items. `Shell.test.tsx` checks the badge appears only when `queue` is not null. `TestTheAdminBundleIsSeparate` stays green.

### Out of scope

- Creating flags and the auto-hide itself (TKT-01M29CP2PJ).
- The application review screen, its decision service and its routes (TKT-01M29CP2MG).
- The moderator delete and restore primitives (TKT-01M29CRJ5J).
- Suspending an author (TKT-01M29CP2QK). The queue only links to it.
- Notifying moderators of new items (TKT-01M29CP2N0).

### Open decision

Does dismissing an auto-hidden post's flags restore the post? Recorded in a note of the same date.
- (a) Yes, when the hide was the auto-hide. docs/08 says the post is hidden "pending review", and a review that dismisses the flags has found nothing to hide. A post a moderator hid by hand stays hidden.
- (b) No. Dismissing only closes the flags, and restoring is a separate moderator action. That leaves a hidden post with no open flags, which is in nobody's queue.
Recommendation: (a). Under (b), an auto-hidden post whose flags are dismissed stays hidden, and no queue item points a moderator back to it.

## Notes

**agent:claude-code/phase2-groom** at 2026-09-29T05:13:04Z

Open decision for the maintainer (grooming, 2026-09-29): does dismissing the flags on an auto-hidden post restore it? (a) yes, when the hide was the auto-hide (a Delete with auto=true), because docs/08 says the post is hidden pending review; a moderator's own hide stays. (b) no, and restoring is a separate action. Recommended: (a). Under (b), a hidden post with no open flags is in nobody's queue. See the plan's Open decision.
