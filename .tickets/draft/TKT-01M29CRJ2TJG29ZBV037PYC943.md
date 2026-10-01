---
schema: 3
id: TKT-01M29CRJ2TJG29ZBV037PYC943
title: Build the notification generator
type: task
status: draft
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
claim: null
archive: null
created_at: 2026-09-11T23:27:36Z
updated_at: 2026-10-01T09:39:35Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase3-groom-unread
  name: ""
extensions: {}
---

## Description

The event bus subscriber that writes `notifications` rows: reply_to_you, mention, quoted, and thread_you_watch.

One post can trigger several kinds for the same person. A reply that also quotes and also mentions you should not arrive as three separate alerts, so collapse by post and viewer, keeping the strongest kind.

Notifying yourself for your own post is the other obvious case, and it needs suppressing at generation rather than filtering at display.

## Acceptance criteria

- [ ] One post produces at most one notification per recipient, keeping the strongest kind (TestOnePostOneNotificationPerRecipient, TestStrongestKindWins)
- [ ] A user is never notified about their own post (TestNoSelfNotification)
- [ ] A muted thread or forum produces no notifications, and a watched thread in a muted forum still does (TestMuteSuppressesEveryKind)
- [ ] Generation is idempotent under event replay, so catch-up does not duplicate alerts (TestReplayIsIdempotent)
- [ ] After a crash at any point, startup catch-up generates each missing notification once, and the first start alerts nobody to history (TestCatchUpAfterCrash, TestFirstStartAlertsNoHistory, TestReindexWritesNoNotifications)
- [ ] No notification is listed or counted in /me for a post that is deleted, hidden, hidden pending review, redacted or moved, in a quarantined thread, in a thread moved to a forum the viewer cannot read, or in a forum the viewer lost access to; moderators by role or group grant included (TestNotificationsNeverWidenAccess, TestNotificationsTrailingProjection)
- [ ] GET /notifications and POST /notifications/read are served, refuse non-members, and are documented in docs/07 with a form-post row (TestNotificationList, TestMarkNotificationsRead, TestNotificationsRefusals, TestEveryViewerStateRouteHasAFormPost)
- [ ] The /me notifications count equals the unread rows GET /notifications serves (TestBadgeMatchesNotificationList); TestMeCountsScansNoTable and TestMeBudget still pass
- [ ] The generator opens thread logs read-only and decides each recipient's access through perm as their own session would (TestGeneratorOpensLogsReadOnly, TestRecipientViewerMatchesSession)
- [ ] A thread the viewer has muted since its notification was written, at thread or forum level, has that notification neither listed nor counted until unmuted, and a decided application's new_application row is listed as read and not counted (TestNotificationsNeverWidenAccess case l, TestBadgeMatchesNotificationList)

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

1. **Migration**, at the next free number (0020 or later, checked at implementation):
   - `notification_progress (thread_id INTEGER PRIMARY KEY, log_length INTEGER NOT NULL, updated_at INTEGER NOT NULL)`. It is authoritative and carries no PROJECTION marker, so `db.ProjectionTables` and reindex leave it alone. The migration seeds it with `INSERT ... SELECT thread_id, log_length, updated_at FROM projection_state`. That only saves the first catch-up a scan. The projection may be behind when the migration runs, so the seed cannot be what keeps history quiet. The **install cutoff** does that: the generator reads its own migration's `schema_migrations.created_at` once at start, and a record whose frame timestamp is at or before it generates nothing. A post written before the generator existed is history whatever the projection had reached (review 1693).
   - `CREATE UNIQUE INDEX notifications_post ON notifications (user_id, thread_id, ordinal) WHERE thread_id IS NOT NULL`.
   - `CREATE UNIQUE INDEX notifications_watch_unread ON notifications (user_id, thread_id) WHERE kind = 'thread_you_watch' AND read_at IS NULL`.
   - `CREATE INDEX notifications_recent ON notifications (user_id)`. It ends in the rowid, which is the list's order.
   - `CREATE INDEX watches_by_thread ON watches (thread_id) WHERE thread_id IS NOT NULL` and `watches_by_forum ON watches (forum_id) WHERE forum_id IS NOT NULL`, so the generator finds a post's watchers without scanning.
   - No code has written a post kind yet (docs/04), so the unique indexes meet no duplicates.
2. **Queries** in `internal/store/db/queries/notifications.sql`:
   - `GetNotificationProgress` and `SetNotificationProgress`.
   - `UpsertPostNotification` uses `ON CONFLICT (user_id, thread_id, ordinal) WHERE thread_id IS NOT NULL DO UPDATE`. It moves `kind` and `actor_id` to the stronger kind by a `CASE` rank, reads only `excluded.*` (sqlc v1.31.1 binds no parameter there on SQLite), and never touches `read_at` or `at`.
   - `InsertWatchNotification` uses `INSERT ... ON CONFLICT DO NOTHING` with no target, so it yields to an unread watch row and to any row for the post.
   - `PostWatchers(thread_id, forum_id)` returns each user's effective level by the `coalesce(tw.level, fw.level, '')` rule.
   - `NotificationRecipients(ids)` returns each user's `status` and `role`.
   - `ListNotifications`, keyset on `id` descending: the order the rows were written, which is the order the alerts arrived. `at` is the record's own time and is only shown. Catch-up can write a notification for an older post after a newer one, so ordering by `at` would let `up_to` disagree with the list (review 1709).
   - `MarkNotificationsRead(user, ids)` and `MarkNotificationsReadUpTo(user, id)`, which marks every unread row of the user with `id <= up_to`.
3. **The generator**, `internal/events/notifier` (`Generator`, `Name() == "notifications"`). It is an `events.Subscriber` registered after the projection in `cmd/ketju/serve.go`. Its options are:
   - `DB`
   - `Root`
   - `Project`, which is `projection.Writer.ProjectThread` (safe for concurrent use, and a no-op when already there)
   - `Index`, the thread's index through the thread cache, for reply and quote authors when `post_meta` lacks them
   - `Perm`
   - `Quarantined`
   - `Logger`
   - `Now`

   `Deliver(batch)`:
   - with `Dropped > 0` it runs `CatchUp` first;
   - for each thread at its longest reported length it calls `Project(ctx, id, target)` first. So `post_meta`, `mentions` and `threads.forum_id` describe the range before the generator reads them, and the generator never waits on the projection's goroutine;
   - it then runs `process(id, target)`;
   - a failure holds the thread in a behind set and is not returned to the bus (the same reasoning as `projection.Writer`'s package comment). Only a failed sweep is returned.

   `process` is one `db.Write` transaction:
   1. Read the progress (no row means `threadlog.FileHeaderSize`). Return if it is at or past the target.
   2. Open the log read-only and run `threadlog.ScanFramesFrom(f, progress, target)`.
   3. For each frame:
      - **`Post`** with no `origin`: the candidates are the `reply_to` post's author, every `mentions` id, the authors of the quotes `render.RenderWith(source, Context{Posts: ...})` reports (a quote of a removed post is skipped), and every user whose effective level on the thread is `watch`.
      - **`Edit`** with a non-nil `mentions`: those ids, as `mention`.
      - **Anything else:** nothing.
      - **A record older than `now − 7 days`:** nothing.
   4. Collapse to one strongest kind per recipient. Drop the actor, recipients not `active`, effective level `mute`, and anyone `perm.Can(recipient, forum, read)` refuses. The recipient's `domain.Viewer` is built from the `users` row (`UserID`, `Role`, `Active`, and `SessionKey: "notifier"`). perm's cache key carries the user and role, so it never shares an entry with a session.
   5. Write the rows and `SetNotificationProgress(target)`, then commit.

   A damaged frame stops the scan. Progress stays before it, and the thread is held. A quarantined thread is skipped.
   `Run(ctx)` retries the behind set with the projection's backoff of one second to one minute. `Lag()` reports it.
4. **Crashes and catch-up.**
   - Rows and progress commit in one transaction per thread, so a crash leaves both or neither.
   - At startup, `serve.go` runs `proj.CatchUp`, then `gen.CatchUp(ctx)`, both before `bus.Start`. `gen.CatchUp` reads `projection_state` against `notification_progress`, as `SELECT p.thread_id, p.log_length FROM projection_state p LEFT JOIN notification_progress n USING (thread_id) WHERE n.log_length IS NULL OR n.log_length < p.log_length`, and processes each thread. So whatever the projection holds and the generator does not is generated once.
   - A thread whose projection is itself behind is caught when its next event or `Run` projects it.
   - An event for a range already processed is a no-op by progress. Processing a range again with its progress reset converges anyway, because of the unique indexes and the upsert that only ever strengthens a kind.
   - `ketju reindex` leaves `notifications` and `notification_progress` alone and does not move offsets.
   - Hard redaction keeps every frame's length (docs/03), so progress stays valid.
   - The install cutoff keeps the first start, and any range the projection had not reached at install, from alerting on history. The seven-day rule covers a database restored without progress rows.
   - The generator's goroutine and closer follow the projection runner's (stop before the store closes, `busDrain`).
5. **Serving**, in `internal/service/threads/notifications.go`. It is beside `userposts.go` because it uses the same quarantine, `LogSize` and index checks.
   - `Notifications(ctx, v, cursor)` requires a member (`member.Require`). It reads a page of 25, highest `id` first. The SQL joins `threads` (forum, slug, title, `log_length_projected`) and `post_meta`, and drops a row whose post is `deleted` or `moved`.
   - Then `s.Perm.CanEach(v, forums, PermRead)` drops unreadable forums, and quarantined threads are dropped.
   - For a thread whose projection trails its log (`Threads.LogSize(id) != log_length_projected`), the index entry must be `Flags.Visible()`, as in `userPostCount`.
   - `new_application` is kept only when `v.Staff()`, and is served with `read: true` once its application is not `pending`. No `read_at` is written. `application_decided` is always kept.
   - The answer is `dto.NotificationList{notifications, next_cursor}`. Each `dto.Notification` is `{id, kind, thread {id, slug, title} | null, forum {slug, name} | null, ordinal, actor Author | null, at, read}`. The actor goes through `member.Author`, so an anonymized one is the placeholder.
   - A page may be short after filtering, and `next_cursor` is empty only at the end.
   - `MarkNotificationsRead(ctx, v, req)` takes `{ids}` or `{up_to}` and touches only the viewer's own unread rows. It answers `dto.NotificationsRead{marked}`.
   - **One visibility decision for both surfaces** (review 1693). `threads.Service.VisibleNotifications(ctx, v, refs)` takes candidate rows, each with thread, ordinal, forum, `log_length_projected` and the `post_meta` live flag. Each candidate row also carries the thread's effective level for the viewer (`coalesce(tw.level, fw.level, '')`, joined from the viewer's own `watches` rows in the same SQL). The function drops a row whose level is `mute` (review 1718), then applies the forum check (`CanEach`), the quarantine, and the trailing-projection index check, and returns the rows that may be served. The list calls it on its page.
   - `/me` calls the same function. `MeCounts`'s notifications part becomes per-thread rows in the same one statement, through a `UNION ALL` with a row-kind column: `forum_id`, `thread_id`, `log_length_projected`, and `json_group_array(ordinal)` of the viewer's unread post notifications whose `post_meta` row is live, each thread with its effective level, plus one row counting the unread application kinds, with `new_application` only while pending and only for a staff role. `users.Service` gains a `VisibleNotifications` hook, wired in `serve.go` as `QueueCount` is. So the badge and the list cannot disagree, even while the projection trails the log.
   - The statement count does not change. The index is consulted only for a thread whose projection trails its log, which is a thread just appended to and so already open in the cache.
6. **Routes:** `GET /api/v1/notifications?cursor=` and `POST /api/v1/notifications/read`, both `no-store`. The POST joins `unchargedPaths`. Anonymous is `unauthenticated` and a pre-user is `forbidden`.
7. **Docs.**
   - docs/07: resources rows with the rules above, plus a form-post row `POST /api/v1/notifications/read` to `POST /notifications/read`, `ids` or `up_to`, `MarkNotificationsRead`.
   - docs/02 "Background work": the generator's row says "per-thread progress in `notification_progress`; catch-up at startup and after a drop".
   - docs/04 already carries the rules.

### Tests

Generation (`internal/events/notifier`):
- `TestOnePostOneNotificationPerRecipient`: one post replies to, quotes and mentions a member and they watch the thread. One `mention` row results. `TestStrongestKindWins` checks the rank table.
- `TestNoSelfNotification`: an author replying to, quoting and mentioning themselves in a thread they watch gets nothing.
- `TestMuteSuppressesEveryKind`: a thread mute and a forum mute each stop all four kinds, and a thread watch inside a muted forum still alerts.
- `TestReplayIsIdempotent`: process, deliver the same events again, reset progress to the header and process again, and deliver a `Dropped` batch. The rows are the same and `read_at` is untouched.
- `TestCatchUpAfterCrash`:
  - a projected range with no generation is generated once by `CatchUp`;
  - an error injected after the inserts and before the commit leaves no rows, and the retry writes them once;
  - a held thread is retried by `Run`.
- `TestFirstStartAlertsNoHistory`: records written before the migration get no rows. That includes a range the projection had not reached when the migration ran and that startup catch-up projects afterwards.
- `TestHorizonSkipsOldRecords`.
- `TestWatchCollapsesToFirstUnread`: five posts give one unread row naming the first. After it is read, the next post adds a row.
- `TestEditNotifiesNewMentionsOnly`.
- `TestGeneratorSkipsInactiveAndUnreadable`: a suspended or deactivated recipient, and one without read on a restricted forum, get nothing.
- `TestRecipientViewerMatchesSession`: the generator's viewer gets the same `perm` decision as the member's own session, including a moderator by group grant. This is the "role changes go through perm" invariant.
- `TestGeneratorOpensLogsReadOnly`, in the manner of `TestProjectionKeepsTheLogReadOnly` (the frozen log format invariant).
- `TestReindexWritesNoNotifications` (`cmd/ketju`): reindex leaves both tables unchanged.

Serving and access (`internal/web/api`). Each case asserts both `GET /notifications` and `/me`'s `notifications`:
- `TestNotificationsNeverWidenAccess`, a table:
  - (a) deleted by its author;
  - (b) hidden by a moderator;
  - (c) hidden pending review (auto-hide);
  - (d) hard-redacted;
  - (e) `post_meta.moved` set;
  - (f) the thread moved into a forum the viewer cannot read;
  - (g) the viewer removed from the group that granted read of a restricted forum;
  - (h) a moderator by group grant (docs/08 "Decided 2026-09-30"), who sees neither a hidden post's notification nor any notification once the grant is removed;
  - (i) a suspended author whose posts were hidden, gone and then back after the restore;
  - (j) a quarantined thread;
  - (k) `new_application` after the viewer is demoted;
  - (l) a thread muted after its notification was written, gone, then back after unmuting, whether muted at thread level or through its forum (review 1718);
  - a restore that brings a row back.
- `TestNotificationsTrailingProjection`: a `Delete` appended while the projection is held hides the row through the index check, from the list and from `/me`'s count alike.
- `TestNotificationList`: the golden `notifications.member.json`, paging by cursor, and a malformed cursor `bad-request`.
- `TestMarkNotificationsRead`: by ids and by `up_to`. Another user's ids are ignored, and `/me` drops. A row written by catch-up for an older post, and so with a higher `id` and an earlier `at`, sits above the older rows in the list and is cleared by an `up_to` of the first row's id.
- `TestBadgeMatchesNotificationList`: `/me` equals the unread rows the list serves, through a muted thread and through a `new_application` whose application is decided. That application's row is listed as read and is not counted (review 1718).
- `TestNotificationsRefusals`: anonymous `unauthenticated` and pre-user `forbidden`. Both answers are `no-store`.
- `TestEveryViewerStateRouteHasAFormPost` with the POST added. `TestMeCountsScansNoTable` and `TestMeBudget` still pass. `TestProjectionTablesAreMarked` still passes, because progress is not a projection.

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
