-- name: GetReadPosition :one
SELECT * FROM read_positions WHERE user_id = ? AND thread_id = ?;

-- Reading never moves backwards: opening an older page of a thread must not
-- mark the newer posts unread again.
-- name: UpsertReadPosition :exec
INSERT INTO read_positions (user_id, thread_id, last_read_ordinal, last_read_at)
VALUES (?, ?, ?, ?)
ON CONFLICT (user_id, thread_id) DO UPDATE SET
	last_read_ordinal = max(last_read_ordinal, excluded.last_read_ordinal),
	last_read_at = excluded.last_read_at;

-- name: ThreadWatchLevel :one
SELECT level FROM watches WHERE user_id = ? AND thread_id = ?;

-- A thread's unread posts for one user: those past the user's read position
-- (0 when they never opened the thread, so no row) and written after
-- users.mark_all_read_at, which means "everything before this moment is read"
-- (docs/04 "read_positions"). created_at is revision 1's, so an edit does not
-- make an old post unread. Deleted posts count, as post_count counts them; a
-- reader decides whether a tombstone is worth a badge.
-- name: UnreadCount :one
SELECT count(*) FROM post_meta AS p
JOIN users AS u ON u.id = sqlc.arg('user_id')
LEFT JOIN read_positions AS rp ON rp.user_id = u.id AND rp.thread_id = p.thread_id
WHERE p.thread_id = sqlc.arg('thread_id')
  AND p.ordinal > coalesce(rp.last_read_ordinal, 0)
  AND (u.mark_all_read_at IS NULL OR p.created_at > u.mark_all_read_at);

-- MeCounts is GET /me's second and last statement (the first is the session
-- lookup): the viewer's unread notifications, and their unread threads by
-- forum, so the service can drop the forums they cannot read from perm's
-- cache without another query.
--
-- A thread is unread when its newest post is: past the read position (0 with
-- no row) and written after mark_all_read_at. That is UnreadCount's per-post
-- rule (docs/04 "read_positions") applied to the post with the highest
-- ordinal and the latest time, which is unread exactly when any post is, and
-- it reads threads rather than scanning post_meta. A test holds the two
-- rules together.
--
-- participated is a thread the viewer has written in, found through
-- post_meta's author index; watching is a thread-level watch. A thread the
-- viewer muted counts nowhere. Forum-level watches are not in these counts
-- yet: what the cross-forum lists contain is TKT-01M29CTY7K's to settle, and
-- it should reuse this rule so the badges and the lists agree.
--
-- There is always at least one row: with no unread threads, forum_id is null.
-- name: MeCounts :many
WITH unread AS (
	SELECT t.forum_id,
	       t.id IN (SELECT thread_id FROM post_meta WHERE author_id = sqlc.arg(user_id)) AS participated,
	       EXISTS (SELECT 1 FROM watches w
	               WHERE w.user_id = sqlc.arg(user_id) AND w.thread_id = t.id AND w.level = 'watch') AS watching
	FROM threads AS t
	JOIN users AS u ON u.id = sqlc.arg(user_id)
	LEFT JOIN read_positions AS rp ON rp.user_id = u.id AND rp.thread_id = t.id
	WHERE t.last_post_ordinal > coalesce(rp.last_read_ordinal, 0)
	  AND (u.mark_all_read_at IS NULL OR t.last_post_at > u.mark_all_read_at)
	  AND NOT EXISTS (SELECT 1 FROM watches m
	                  WHERE m.user_id = u.id AND m.thread_id = t.id AND m.level = 'mute')
),
by_forum AS (
	SELECT forum_id, count(*) AS threads, CAST(sum(participated) AS INTEGER) AS participated,
	       CAST(sum(watching) AS INTEGER) AS watching
	FROM unread GROUP BY forum_id
)
SELECT (SELECT count(*) FROM notifications
        WHERE notifications.user_id = sqlc.arg(user_id) AND notifications.read_at IS NULL) AS notifications,
       f.forum_id, f.threads, f.participated, f.watching
FROM (SELECT 1) AS one
LEFT JOIN by_forum AS f ON 1;
