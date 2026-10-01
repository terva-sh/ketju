---
schema: 3
id: TKT-01M29CRJ2TJG29ZBV037PYC943
title: Build the notification generator
type: task
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area:views
  - effort:M
assignees: []
milestone: phase-3
parent: TKT-01M29BN36D34YVKVRPNQD3PHDV
origin: null
dependencies:
  - TKT-01M29C0WF9GFGVG7QSAYRAKAA4
  - TKT-01M29BYYHQ7PTYNYA3VBBD4PDB
  - TKT-01M29CRJ29H6TZPN0X42SEW5RN
blocks_on: none
references:
  - ref: design:data-model-viewer
    path: docs/04-data-model.md
  - ref: pr:294
    path: null
  - ref: pr:323
    path: null
claim: null
archive: null
created_at: 2026-09-11T23:27:36Z
updated_at: 2026-10-01T15:57:54Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase3-crj2t
  name: ""
extensions: {}
---

## Description

The event bus subscriber that writes `notifications` rows: reply_to_you, mention, quoted, and thread_you_watch.

One post can trigger several kinds for the same person. A reply that also quotes and also mentions you should not arrive as three separate alerts, so collapse by post and viewer, keeping the strongest kind.

Notifying yourself for your own post is the other obvious case, and it needs suppressing at generation rather than filtering at display.

## Acceptance criteria

- [x] One post produces at most one notification per recipient, keeping the strongest kind (TestOnePostOneNotificationPerRecipient, TestStrongestKindWins)
- [x] A user is never notified about their own post (TestNoSelfNotification)
- [x] A muted thread or forum produces no notifications, and a watched thread in a muted forum still does (TestMuteSuppressesEveryKind)
- [x] Generation is idempotent under event replay, so catch-up does not duplicate alerts (TestReplayIsIdempotent)
- [x] After a crash at any point, startup catch-up generates each missing notification once, and the first start alerts nobody to history (TestCatchUpAfterCrash, TestFirstStartAlertsNoHistory, TestReindexWritesNoNotifications)
- [x] No notification is listed or counted in /me for a post that is deleted, hidden, hidden pending review, redacted or moved, in a quarantined thread, in a thread moved to a forum the viewer cannot read, or in a forum the viewer lost access to; moderators by role or group grant included (TestNotificationsNeverWidenAccess, TestNotificationsTrailingProjection)
- [x] GET /notifications and POST /notifications/read are served, refuse non-members, and are documented in docs/07 with a form-post row (TestNotificationList, TestMarkNotificationsRead, TestNotificationsRefusals, TestEveryViewerStateRouteHasAFormPost)
- [x] The /me notifications count equals the unread rows GET /notifications serves (TestBadgeMatchesNotificationList); TestMeCountsScansNoTable and TestMeBudget still pass
- [x] The generator opens thread logs read-only and decides each recipient's access through perm as their own session would (TestGeneratorOpensLogsReadOnly, TestRecipientViewerMatchesSession)
- [x] A thread the viewer has muted since its notification was written, at thread or forum level, has that notification neither listed nor counted until unmuted, and a decided application's new_application row is listed as read and not counted (TestNotificationsNeverWidenAccess case l, TestBadgeMatchesNotificationList)

## Implementation plan

### Settled in grooming

- **One notification per post per recipient.** It keeps the strongest kind, ranked `mention`, then `reply_to_you`, then `quoted`, then `thread_you_watch` (docs/04 `notifications`, run decision 2026-10-01).
- **`thread_you_watch` collapses.** A member has at most one unread row of it per thread. That row names the first new post, and later posts add nothing until the member reads it (docs/04, run decision 2026-10-01).
- **Edits.** An `Edit` with a `mentions` key notifies the members it names, as `mention`, and nothing else. A member already notified about the post gets no new row, though their unread row may move to a stronger kind (docs/04, run decision 2026-10-01).
- **Serving.** A post notification is listed and counted only while three things hold at that moment, moderators included: the post is visible to every reader, the viewer may read its forum, and the thread's effective level for the viewer is not `mute`. Rows are never deleted for this, so a restore or an unmute brings one back. `new_application` is served only to a viewer whose role is moderator or admin at that moment. Once its application is no longer pending, it is served as read, so it agrees with the badge. No notification carries post text (docs/04, run decision 2026-10-01). This is the docs/14 "never widen access" invariant, and any finding against it is a security finding.
- **Generation skips** the post's author, a recipient who is not `active`, a recipient who cannot read the forum when the post is processed, a recipient whose effective level is `mute`, and a record more than seven days older than its processing (docs/04 `notifications`).
- **Precedence and mute** are TKT-01M29CRJ29's, through `domain.EffectiveWatch` (docs/04 `watches`).
- **Mentions** come from the record's `mentions` key, which the renderer filled at write time. A mention inside a quotation is not in it (docs/06 "Composer", TKT-01M29C0WEQ). Quotes are not in the log, so the generator renders the source to find them (`render.Result.Quotes`).
- **The generator is a subscriber on the event bus** (docs/02 "Background work", `internal/events` package comment). The bus is a hint, not the record: a subscriber recovers from durable state after a drop or a restart, as the projection does.
- **In-app only** (docs/10 item 12).
- **Moved here from TKT-01M29CTY86:** `GET /notifications` and `POST /notifications/read`, and the `/me` count's filter. The serve-time access rule and its security tests are server code, and they belong in the PR that writes the rows. TKT-01M29CTY86 keeps the client.
- **The application kinds stay as built** in `internal/service/notify` (TKT-01M29CP2N0).


### Approach

Revised at claim (2026-10-01) against main at 04224e4, after watches and mutes (#318), the search indexer (#310) and the index write-back (#312) landed. What changed from the groomed plan is marked **changed** with the reason.

1. **Migration**, the next free number at push time (0023 at claim), named `notification_generator`:
   - `notification_progress (thread_id INTEGER PRIMARY KEY, log_length INTEGER NOT NULL, updated_at INTEGER NOT NULL)`, authoritative, no PROJECTION marker, seeded from `projection_state` to save the first catch-up a scan.
   - The **install cutoff** is this migration's `schema_migrations.created_at`, found by name, so a renumber on rebase does not move it. A record whose frame timestamp is at or before it generates nothing (review 1693).
   - `notifications_post` unique on `(user_id, thread_id, ordinal) WHERE thread_id IS NOT NULL`; `notifications_watch_unread` unique on `(user_id, thread_id) WHERE kind = 'thread_you_watch' AND read_at IS NULL`; `notifications_recent` on `(user_id)` for the keyset; `watches_by_thread` and `watches_by_forum` partial indexes so a post's watchers are found without a scan.
2. **Queries** in `notifications.sql`: progress get, set and list; `NotificationsSince`; `UpsertPostNotification` (moves to a stronger kind only, reads `excluded.*` and the row, never touches `read_at` or `at`); `InsertWatchNotification` (`ON CONFLICT DO NOTHING`, no target); `ThreadWatchers(thread, forum)` returning each row's level, combined in Go through `domain.EffectiveWatch`; `PostAuthors(thread, ordinals)`; `ListNotifications`; `MarkNotificationsRead` by ids and `MarkNotificationsReadUpTo`.
3. **The generator**, `internal/events/notifier` (`Generator`, `Name() == "notifications"`), an `events.Subscriber`. Options: `DB`, `Root`, `Perm`, `Project` (`projection.Writer.ProjectThread`), `Sweep` (the projection's sweep), `Quarantined`, `Logger`, `Now`, and `Since` (a test seam for the cutoff; zero reads the migration's time).
   - `Deliver`: with `Dropped > 0`, `CatchUp` first, and its error is the one returned. Then each thread in the batch, and each thread already behind, at its longest known length.
   - Each thread: `Project(id, target)`, then read `projection_state.log_length` as P and process up to `min(target, P)`. **Changed:** processing stops at what the projection reached rather than failing when it is short, so the generator never reads `post_meta` or `threads.forum_id` for a range the projection has not applied, and a thread the projection cannot reach stays in the behind set at its target.
   - `process` is one `db.Write` transaction: read progress (no row is the file header's end), scan frames from it to the end with `threadlog.ScanFramesFrom` on a log opened `O_RDONLY`, decide recipients, write rows and progress, commit. A damaged frame stops the scan and the thread is held.
   - Candidates: a `Post` with no `origin` gives the `reply_to` post's author (`reply_to_you`), each `mentions` id (`mention`), the authors of the posts its source quotes (`quoted`; `render.RenderWith` with no resolver, then kept only for an earlier ordinal that `post_meta` holds and is neither deleted nor moved), and each user whose effective level is `watch` (`thread_you_watch`). An `Edit` with a non-nil `mentions` gives those ids as `mention`. Nothing else generates.
   - Collapse to the strongest kind per recipient, then drop the record's actor, the post's author, a user with no row or not `active`, effective level `mute`, and anyone `perm.Can(read)` refuses for the thread's forum. The recipient's viewer is `RecipientViewer(user)`: user id, role only when active, `Active`, `SessionKey: "notifier"` (perm's cache key carries user, role and status, so it never shares an entry with a session).
   - Records at or before the cutoff, or more than seven days older than `Now`, generate nothing. An actor id the database lacks is written as a null actor, so a foreign key cannot hold a thread back.
   - **Changed (review 1743, carried as a requirement):** `CatchUp` runs `Sweep` first, then walks the logs (`threadid.WalkLogs`) and compares each size with `notification_progress`; every thread behind is projected and processed as above, and one the projection cannot reach stays held. A quarantined thread is skipped. `Run` sweeps while a sweep is owed (from `New`, and after one fails) and otherwise retries the behind set at each log's current size, backing off from one second to one minute. `Lag()` reports the behind set.
4. **Serve.** **Changed:** the generator is subscribed after the search indexer and gets its own step, `notifications`, after `search`: its `Run` goroutine starts there, so its startup sweep runs beside the listener, as the indexer's does, rather than before `bus.Start`. A failed sweep then retries instead of stopping the start. Shutdown stops it after the bus closes (`stop notifications`), as for the indexer.
5. **Serving**, in `internal/service/threads/notifications.go`:
   - `Notifications(ctx, v, cursor)`: `member.Require`, a page of 25 by `id` descending, keyset on `id`. The SQL drops a post row whose thread has no projection row, whose `post_meta` row is deleted, moved or missing, or whose effective level for the viewer is `mute` (`coalesce(tw.level, fw.level, '')`), and a `new_application` row unless the viewer's `users` row is an active moderator or admin. **Changed:** the mute and liveness filters are in SQL in both the list and `/me`, with the same predicates, so a page is rarely short; the Go decision below is still the one function both call.
   - `VisibleNotifications(ctx, v, threads)`: forum read through `perm.CanEach`, the quarantine, and for a thread whose projection trails its log (`LogSize != log_length_projected`) each ordinal's index entry must be `Visible()`. Both the list and `/me` call it.
   - A `new_application` whose application is not `pending` is served `read: true` and not counted; no `read_at` is written.
   - `MarkNotificationsRead(ctx, v, {ids} | {up_to})` touches only the viewer's own unread rows, answers `{marked}`.
   - DTOs: `NotificationList {notifications, next_cursor}`, `Notification {id, kind, thread|null, forum|null, ordinal|null, actor|null, at, read}`, `NotificationsRead {marked}`. No post text.
6. **`/me`.** **Changed:** `MeCounts` keeps its shape. Its `notifications` column counts the application kinds only (with the staff and pending rules), and one new column, `notified_posts`, carries the viewer's unread live unmuted post notifications as JSON grouped by thread (thread, forum, projected length, ordinals), from CTEs added after `by_forum`. That is a smaller, separated edit than a `UNION ALL` row kind, because TKT-01M29CTY7K also edits `MeCounts`. `users.Service` gains a `VisibleNotifications` hook, wired in `serve.go` as `QueueCount` is; nil counts no post notification (fails closed). The statement count does not change.
7. **Routes:** `GET /api/v1/notifications?cursor=` and `POST /api/v1/notifications/read`, both `no-store`; the POST joins `unchargedPaths`. Anonymous `unauthenticated`, pre-user `forbidden`.
8. **Docs:** docs/07 resource rows and a form-post row; docs/02 "Background work" row; docs/04 indexes and progress. No audit action is added.

### Tests

Generation (`internal/events/notifier`): TestOnePostOneNotificationPerRecipient, TestStrongestKindWins, TestNoSelfNotification, TestMuteSuppressesEveryKind, TestReplayIsIdempotent, TestCatchUpAfterCrash, TestDroppedEventOnIdleThreadIsGenerated (review 1743), TestFirstStartAlertsNoHistory, TestHorizonSkipsOldRecords, TestWatchCollapsesToFirstUnread, TestEditNotifiesNewMentionsOnly, TestGeneratorSkipsInactiveAndUnreadable, TestRecipientViewerMatchesSession, TestGeneratorOpensLogsReadOnly. TestReindexWritesNoNotifications in `cmd/ketju`.

Serving (`internal/web/api`), each case through both `GET /notifications` and `/me`: TestNotificationsNeverWidenAccess cases (a) to (l) as groomed, generated through the real generator; TestNotificationsTrailingProjection; TestNotificationList with a golden; TestMarkNotificationsRead; TestBadgeMatchesNotificationList; TestNotificationsRefusals; TestEveryViewerStateRouteHasAFormPost with the POST; TestMeCountsScansNoTable and TestMeBudget unchanged in what they hold.

### Out of scope

- The notifications screen, badges and Alerts tab: TKT-01M29CTY86.
- Live badge updates over SSE: TKT-01M29CRJ3X and TKT-01M29CTYA.
- Email and push: docs/10 items 12 and 27.
- Retention of old read notifications: not planned for v1. The rows are small, and nothing deletes them.
- Notifications for split or merge copies, which have an `origin`: Phase 4, TKT-01M29CXJZZ.

## Notes

**agent:claude-code/phase3-groom-unread** at 2026-10-01T07:54:14Z

run decision: pending maintainer review. Four decisions bind this ticket, recorded in docs/04 'notifications' on 2026-10-01. (1) One row per post per recipient, ranked mention > reply_to_you > quoted > thread_you_watch. (2) At most one unread thread_you_watch per thread, naming the first new post. (3) An Edit notifies the members its mentions key names, as mention, and nothing else. (4) A post notification is served and counted only while the post is visible to every reader and its forum is readable by the viewer at that moment, moderators included. new_application is served only to current staff, and no row carries post text. Scope change: GET /notifications, POST /notifications/read and the /me count's filter moved here from TKT-01M29CTY86 (Build the notifications UI and alerts tab), so the serve-time access rule and its security tests ship in the same PR as the rows they guard. This ticket now depends on TKT-01M29CRJ29 (Implement watches and mutes) for the precedence.

**agent:claude-code/phase3-groom-unread** at 2026-10-01T08:27:24Z

Review 1693 (head 2b18c6f, base a599b48) fixed in the plan. High, 'Apply trailing-projection visibility checks to the notification badge': the list and /me now share one function, threads.Service.VisibleNotifications. It checks forum access, quarantine and the index for a trailing projection. MeCounts returns per-thread ordinals in the same single statement so /me can call it. Medium, 'Do not seed progress from a projection that may still be behind': history is now cut off by the generator migration's schema_migrations.created_at, and records at or before it generate nothing. The projection_state seed only saves a scan. docs/04 says the same.

**agent:claude-code/phase3-groom-unread** at 2026-10-01T09:39:28Z

Review 1718 (head f93170e, base f3538e6) fixed in the plan and in docs/04. High, 'Filter existing notifications when a member mutes a thread': VisibleNotifications now drops rows whose thread is muted for the viewer when served, for the list and /me alike; case (l) is added to TestNotificationsNeverWidenAccess. Medium, 'Make decided-application rows follow the same list and badge rule': a new_application whose application is no longer pending is served as read, with no read_at written, so the list and the badge agree. The work is carried onto groom/p3-unread-notifications-2 because main moved; that is round 4 of 5.

**agent:claude-code/phase3-groom-unread** at 2026-10-01T10:55:36Z

Requirement carried from review 1743 (2026-10-01; PR #285, head fb1c38d, base ccb234d). Finding (high, not security): 'Catch up threads missed by both bus subscribers'. The plan's notifier CatchUp compares projection_state with notification_progress. A dropped event that also left the projection behind can therefore leave an idle thread unprocessed: the notifier sweeps before the projection catches up, and no later event or Run retries it. What the implementation must do: the notifier's sweep must find behind threads from the logs and not from projection_state alone. On a Dropped batch, and at startup, run the projection's sweep first (proj.CatchUp with the serve.go reopen). Then compare each thread's log length (the thread store's LogSize, or the projection's walk) with notification_progress, and for every thread behind call Project(ctx, id, logLength) and then process. A thread the projection cannot reach stays in the notifier's behind set, so Run keeps retrying it. Name a test TestDroppedEventOnIdleThreadIsGenerated: drop an event for a thread whose projection is also behind, send no later event, and assert the notification arrives through the sweep or Run.

**agent:claude-code/phase3-crj2t** at 2026-10-01T15:53:16Z

Review 1781 (head e1311dc, base ba98d3a, Actions run 2149): both findings are fixed in 97278f7e. This is round 1 of 5.

### High: a trailing projection authorized a stale forum after a move
This is a security finding, and it is fixed. A ThreadMoved in the log that the projection has not reached left `threads.forum_id` naming the old forum. `VisibleNotifications` checked read against that forum, so a reader of the old forum could see the notification and the badge count until the projection caught up.

`VisibleNotifications` now asks the tail between the projected length and the snapshot's length whether it holds a ThreadMoved, through the new header-only walk `threadlog.Reader.Moved`. If it does, the thread's rows are withheld from the list and the badge until the projection reaches the move. A projected length past the snapshot is withheld the same way.

Alternatives considered:
- **Decode the last ThreadMoved's forum and authorize that forum instead.** It lost because the list would still render the projection's stale forum and title for the row, and it needs a second perm batch for forums met mid-loop. Withholding is the one rule that cannot widen access. The window is short, because `Move` projects synchronously after its append, so a reader loses at most a moment.
- **Project the thread from the read path.** It lost because a GET then writes to the database.

Tests: `TestNotificationsTrailingMove` checks the list and `/me` through a move into a restricted forum and back, each before and after projecting. `TestMoved` covers the walk.

### Medium: the upsert strengthened a read row
This is fixed. docs/04's run decision lets only an unread row move to a stronger kind. `UpsertPostNotification`'s conflict update now requires `read_at IS NULL`. `TestEditLeavesReadNotificationAlone` covers it.

### Mutations
Each fix was reverted in turn to check that its test fails:
- dropping the moved check: caught by `TestNotificationsTrailingMove`;
- dropping the `read_at` condition: caught by `TestEditLeavesReadNotificationAlone`.

## Summary

Landed as the notifier package, internal/events/notifier, subscribed in serve after the indexer with its own "notifications" step. It writes notification rows from posts and mention edits. Migration 0024 adds notification_progress, seeded from projection_state, and the unique and partial indexes the upserts and the list need.

### Generation
- A thread is processed only up to projection_state.log_length, in the same transaction as its progress row. A thread whose projection trails is held and retried by Run with backoff.
- After a dropped batch, catch-up runs the projection's sweep first (CatchUp with reopen), then walks the logs to find behind threads. This is the review 1743 requirement. TestDroppedEventOnIdleThreadIsGenerated covers it, including a torn tail that only the sweep's reopen cuts.
- Recipients come from domain.EffectiveWatch and the coalesce(tw.level, fw.level) rule, not re-derived.
- These are skipped: the author, the actor, inactive members, muted members, members perm refuses read (RecipientViewer is the recipient's own session view), records at or before the install cutoff, and records older than the seven-day horizon.
- Kinds rank mention > reply_to_you > quoted > thread_you_watch. A thread has at most one unread watch row per member.

### Serving
- GET /api/v1/notifications is keyset on id, 25 to a page, with an opaque cursor. POST /api/v1/notifications/read takes ids (at most 200) or up_to, and is uncharged.
- A post notification is served only while the post is live, the forum readable, the thread not quarantined and not muted for the viewer. This is decided at serve time, moderators included. A trailing projection is decided by the index.
- No post text is carried. new_application goes to active staff only, and is served as read once decided.
- /me counts the same rows through the same function (VisibleNotifications) with no new statement. MeCounts carries a notified_posts JSON column.

### Client and checks
The client change is type-only (dto.ts and dto-contract.ts), and the dist is byte-identical. The mutations tried are listed in the commit body; all eleven were caught.
