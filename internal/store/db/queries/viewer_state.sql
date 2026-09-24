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

-- MeCounts is GET /me's count statement: the viewer's unread notifications,
-- and their unread threads by forum, so the service can drop the forums they
-- cannot read in one batch from perm.
--
-- A thread is unread when its newest post is: past the read position (0 with
-- no row) and written after mark_all_read_at. That is UnreadCount's per-post
-- rule (docs/04 "read_positions") applied to the post with the highest
-- ordinal and the latest time, which is unread exactly when any post is. A
-- test holds the two rules together.
--
-- participated is a thread the viewer has written in; watching is a
-- thread-level watch. A thread the viewer muted counts nowhere. Forum-level
-- watches are not in these counts yet: what the cross-forum lists contain is
-- TKT-01M29CTY7K's to settle, and it should reuse this rule so the badges
-- and the lists agree.
--
-- The shape is for cost, because the shell runs this on every page load.
-- Only one part reads the whole site: counting, per forum, the threads with
-- a post after the mark, all of which are unread to someone who never opened
-- them. Everything that depends on the viewer starts from the viewer's own
-- rows (read positions, watches, posts) and subtracts from that count, so it
-- grows with what they have touched rather than with the site.
--
-- There is always at least one row: with no unread threads, forum_id is null.
-- name: MeCounts :many
WITH me AS (
	SELECT coalesce(mark_all_read_at, -1) AS mark FROM users WHERE id = sqlc.arg(user_id)
),
active AS (
	SELECT t.forum_id, count(*) AS threads
	FROM threads AS t, me
	WHERE t.last_post_at > me.mark AND t.last_post_ordinal > 0
	GROUP BY t.forum_id
),
mine AS (
	SELECT DISTINCT thread_id FROM post_meta WHERE author_id = sqlc.arg(user_id)
),
touched AS (
	SELECT thread_id FROM read_positions WHERE user_id = sqlc.arg(user_id)
	UNION SELECT thread_id FROM watches WHERE user_id = sqlc.arg(user_id) AND thread_id IS NOT NULL
	UNION SELECT thread_id FROM mine
),
state AS (
	SELECT t.forum_id,
	       t.last_post_ordinal > 0 AS active,
	       t.last_post_ordinal > coalesce(rp.last_read_ordinal, 0) AND coalesce(w.level, '') <> 'mute' AS unread,
	       coalesce(w.level, '') = 'watch' AS watched,
	       m.thread_id IS NOT NULL AS participated
	FROM touched
	JOIN threads AS t ON t.id = touched.thread_id
	CROSS JOIN me
	LEFT JOIN read_positions AS rp ON rp.user_id = sqlc.arg(user_id) AND rp.thread_id = t.id
	LEFT JOIN watches AS w ON w.user_id = sqlc.arg(user_id) AND w.thread_id = t.id
	LEFT JOIN mine AS m ON m.thread_id = t.id
	-- Only threads with a post after the mark: the rest are read by the mark,
	-- and are not in active's count to subtract from.
	WHERE t.last_post_at > me.mark
),
adjust AS (
	SELECT forum_id,
	       sum(active AND NOT unread) AS not_unread,
	       sum(unread AND participated) AS participated,
	       sum(unread AND watched) AS watching
	FROM state GROUP BY forum_id
),
by_forum AS (
	SELECT a.forum_id,
	       a.threads - coalesce(j.not_unread, 0) AS threads,
	       CAST(coalesce(j.participated, 0) AS INTEGER) AS participated,
	       CAST(coalesce(j.watching, 0) AS INTEGER) AS watching
	FROM active AS a LEFT JOIN adjust AS j ON j.forum_id = a.forum_id
	WHERE a.threads - coalesce(j.not_unread, 0) > 0
)
SELECT (SELECT count(*) FROM notifications
        WHERE notifications.user_id = sqlc.arg(user_id) AND notifications.read_at IS NULL) AS notifications,
       f.forum_id, f.threads, f.participated, f.watching
FROM (SELECT 1) AS one
LEFT JOIN by_forum AS f ON 1;
