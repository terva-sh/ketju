-- Notifications (docs/04 "notifications"). They are written through
-- internal/service/notify, in the transaction of the act that causes them,
-- and nothing here marks one read: read_at records that the recipient read
-- it, and only the recipient does that (TKT-01M29CTY86).

-- NotifyStaffOfApplication writes one new_application row naming the
-- application for every active user with role moderator or admin (docs/05
-- "mode = approval" step 3, TKT-01M29CP2N0). Recipients are chosen by role,
-- the predicate that gates review, not by the moderators group: a group
-- member without the role cannot open the review queue. Agents with the
-- role are staff too, and are notified.
-- name: NotifyStaffOfApplication :execrows
INSERT INTO notifications (user_id, kind, application_id, at)
SELECT id, 'new_application', sqlc.arg(application_id), sqlc.arg(at)
FROM users
WHERE status = 'active' AND role IN ('moderator', 'admin');

-- NotifyApplicationDecided writes the approved applicant's
-- application_decided row, as the user the approval created, with the
-- deciding moderator as actor. A denied applicant has no user, so no row.
-- name: NotifyApplicationDecided :exec
INSERT INTO notifications (user_id, kind, application_id, actor_id, at)
VALUES (sqlc.arg(user_id), 'application_decided', sqlc.arg(application_id), sqlc.arg(actor_id), sqlc.arg(at));

-- The post kinds are written by the notification generator
-- (internal/events/notifier, TKT-01M29CRJ2T), which keeps its progress per
-- thread in notification_progress (migration 0024).

-- name: GetNotificationProgress :one
SELECT log_length FROM notification_progress WHERE thread_id = ?;

-- name: SetNotificationProgress :exec
INSERT INTO notification_progress (thread_id, log_length, updated_at)
VALUES (sqlc.arg(thread_id), sqlc.arg(log_length), sqlc.arg(updated_at))
ON CONFLICT (thread_id) DO UPDATE SET
	log_length = excluded.log_length,
	updated_at = excluded.updated_at;

-- name: ListNotificationProgress :many
SELECT thread_id, log_length FROM notification_progress;

-- NotificationsSince is the install cutoff: when the generator's migration
-- was applied, found by its name so a renumber does not move it. A record
-- written at or before it generates nothing, so installing the generator
-- alerts nobody to history (docs/04 "notifications", review 1693).
-- name: NotificationsSince :one
SELECT created_at FROM schema_migrations WHERE name = 'notification_generator';

-- UpsertPostNotification writes one member's notification for one post, or
-- moves the row the member already has for that post to kind when kind is
-- the stronger: mention, then reply_to_you, then quoted, then
-- thread_you_watch (docs/04, run decision 2026-10-01). Only an unread row
-- moves: one the member has read stays as they read it, so an edit's
-- mention does not rewrite history they have seen (Terva review 1781). It
-- never touches read_at or at, so a replay leaves a read row read, and it reads only
-- excluded and the row itself: sqlc v1.31.1 binds no parameter inside DO
-- UPDATE on SQLite.
-- name: UpsertPostNotification :exec
INSERT INTO notifications (user_id, kind, thread_id, ordinal, actor_id, at)
VALUES (sqlc.arg(user_id), sqlc.arg(kind), sqlc.arg(thread_id), sqlc.arg(ordinal), sqlc.arg(actor_id), sqlc.arg(at))
ON CONFLICT (user_id, thread_id, ordinal) WHERE thread_id IS NOT NULL DO UPDATE SET
	kind = excluded.kind,
	actor_id = excluded.actor_id
WHERE notifications.read_at IS NULL
  AND (CASE excluded.kind WHEN 'mention' THEN 4 WHEN 'reply_to_you' THEN 3 WHEN 'quoted' THEN 2 ELSE 1 END)
    > (CASE notifications.kind WHEN 'mention' THEN 4 WHEN 'reply_to_you' THEN 3 WHEN 'quoted' THEN 2 ELSE 1 END);

-- InsertWatchNotification writes a thread_you_watch row, unless the member
-- already has an unread one for the thread (notifications_watch_unread) or
-- any row for the post (notifications_post): DO NOTHING with no target
-- yields to either.
-- name: InsertWatchNotification :exec
INSERT INTO notifications (user_id, kind, thread_id, ordinal, actor_id, at)
VALUES (sqlc.arg(user_id), 'thread_you_watch', sqlc.arg(thread_id), sqlc.arg(ordinal), sqlc.arg(actor_id), sqlc.arg(at))
ON CONFLICT DO NOTHING;

-- ThreadWatchers is every watches row that bears on a thread: its own rows,
-- and the rows for the forum it is in now. The generator combines a
-- member's two through domain.EffectiveWatch (docs/04 "watches").
-- name: ThreadWatchers :many
SELECT tw.user_id, tw.level, 1 AS on_thread FROM watches AS tw WHERE tw.thread_id = sqlc.arg(thread_id)
UNION ALL
SELECT fw.user_id, fw.level, 0 AS on_thread FROM watches AS fw WHERE fw.forum_id = sqlc.arg(forum_id);

-- PostAuthors is the author and liveness of each post a record replies to
-- or quotes.
-- name: PostAuthors :many
SELECT ordinal, author_id, deleted, moved FROM post_meta
WHERE thread_id = sqlc.arg(thread_id) AND ordinal IN (sqlc.slice('ordinals'));

-- ListNotifications is GET /notifications's candidates (TKT-01M29CRJ2T): a
-- page of the member's rows, the newest written first, keyset on id. id is
-- the order the rows were written, which is the order the alerts arrived;
-- catch-up can write a row for an older post after a newer one, so ordering
-- by at would let up_to disagree with the list (review 1709).
--
-- A post row is a candidate only while its thread has a projection row, its
-- post_meta row is neither deleted nor moved, and the thread's effective
-- level for the member, coalesce(tw.level, fw.level, ''), is not mute
-- (docs/04, run decision 2026-10-01). A new_application row is one only
-- while the member is an active moderator or admin. MeCounts states the
-- same rules; TestBadgeMatchesNotificationList holds the two together. The
-- rest of the rule (forum read, quarantine, a projection trailing its log)
-- is threads.Service.VisibleNotifications's.
-- name: ListNotifications :many
SELECT n.id, n.kind, n.thread_id, n.ordinal, n.actor_id, n.at, n.read_at,
       t.forum_id, t.slug AS thread_slug, t.title AS thread_title, t.log_length_projected,
       f.slug AS forum_slug, f.name AS forum_name, a.status AS application_status
FROM notifications AS n INDEXED BY notifications_recent
LEFT JOIN threads AS t ON t.id = n.thread_id
LEFT JOIN forums AS f ON f.id = t.forum_id
LEFT JOIN post_meta AS pm ON pm.thread_id = n.thread_id AND pm.ordinal = n.ordinal
LEFT JOIN applications AS a ON a.id = n.application_id
LEFT JOIN watches AS tw ON tw.user_id = n.user_id AND tw.thread_id = n.thread_id
LEFT JOIN watches AS fw ON fw.user_id = n.user_id AND fw.forum_id = t.forum_id
WHERE n.user_id = sqlc.arg(user_id) AND n.id < sqlc.arg(before)
  AND CASE WHEN n.thread_id IS NULL THEN
        n.kind <> 'new_application'
        OR EXISTS (SELECT 1 FROM users AS u WHERE u.id = n.user_id AND u.status = 'active'
                   AND u.role IN ('moderator', 'admin'))
      ELSE t.id IS NOT NULL AND pm.deleted = 0 AND pm.moved = 0
        AND coalesce(tw.level, fw.level, '') <> 'mute'
      END
ORDER BY n.id DESC
LIMIT sqlc.arg(lim);

-- MarkNotificationsRead marks the member's own unread rows among ids read.
-- name: MarkNotificationsRead :execrows
UPDATE notifications SET read_at = sqlc.arg(at)
WHERE user_id = sqlc.arg(user_id) AND read_at IS NULL AND id IN (sqlc.slice('ids'));

-- MarkNotificationsReadUpTo marks every unread row of the member's with an
-- id at or below up_to read: the list's first row's id marks the list.
-- name: MarkNotificationsReadUpTo :execrows
UPDATE notifications SET read_at = sqlc.arg(at)
WHERE user_id = sqlc.arg(user_id) AND read_at IS NULL AND id <= sqlc.arg(up_to);
