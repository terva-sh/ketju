-- name: GetReadPosition :one
SELECT * FROM read_positions WHERE user_id = ? AND thread_id = ?;

-- Reading never moves backwards: opening an older page of a thread must not
-- mark the newer posts unread again, and a beacon that arrives late, out of
-- order or from a second device must not either (PUT /threads/{id}/read,
-- TKT-01M29CA9CE). The rule lives in the statement rather than in a read
-- followed by a write, so two beacons racing each other cannot interleave.
-- name: UpsertReadPosition :exec
INSERT INTO read_positions (user_id, thread_id, last_read_ordinal, last_read_at)
VALUES (?, ?, ?, ?)
ON CONFLICT (user_id, thread_id) DO UPDATE SET
	last_read_ordinal = max(last_read_ordinal, excluded.last_read_ordinal),
	last_read_at = excluded.last_read_at;

-- LowerReadPosition is mark-unread (DELETE /threads/{id}/read), the only
-- statement that moves a position back. It never moves one forward: marking
-- posts unread must not mark others read. It updates a row, so the caller
-- runs EnsureReadPosition first in the same write transaction: with no row
-- the position is 0, and the row EnsureReadPosition writes says so. A row
-- is written rather than deleted, so marking a thread unread and then reading
-- it forward starts from a stored position.
--
-- Two statements rather than one upsert, because sqlc does not bind a
-- parameter inside ON CONFLICT ... DO UPDATE on SQLite (v1.31.1); the single write
-- connection's BEGIN IMMEDIATE keeps the pair atomic against every writer.
-- name: EnsureReadPosition :exec
INSERT INTO read_positions (user_id, thread_id, last_read_ordinal, last_read_at)
VALUES (?, ?, 0, ?)
ON CONFLICT (user_id, thread_id) DO NOTHING;

-- name: LowerReadPosition :one
UPDATE read_positions
SET last_read_ordinal = CASE WHEN last_read_ordinal > @ordinal THEN @ordinal ELSE last_read_ordinal END,
    last_read_at = @last_read_at
WHERE user_id = @user_id AND thread_id = @thread_id
RETURNING last_read_ordinal;

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
-- A new_application notification counts only while its application is
-- pending (TKT-01M29CP2N0). Closing an application, by approval, denial or
-- withdrawal, never writes read_at, because nobody read anything; the badge
-- drops because the application stopped waiting, and the row stays unread
-- for the notification list. Every other kind counts while unread.
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
-- It starts from, per forum, the threads with a post after the mark, all of
-- which are unread to someone who never opened them. With no mark that is
-- forum_thread_counts, which triggers keep; with one it is a count per forum
-- over threads_forum_active, which reads only the index entries after the
-- mark (migration 0007). Neither reads every thread. Everything that depends
-- on the viewer starts from the viewer's own rows (read positions, watches,
-- posts) and subtracts from that count, so it grows with what they have
-- touched rather than with the site.
--
-- There is always at least one row: with no unread threads, forum_id is null.
-- name: MeCounts :many
WITH me AS (
	SELECT coalesce(mark_all_read_at, -1) AS mark FROM users WHERE id = sqlc.arg(user_id)
),
active AS (
	SELECT c.forum_id,
	       CASE WHEN me.mark < 0 THEN c.threads
	            ELSE (SELECT count(*) FROM threads AS t INDEXED BY threads_forum_active
	                  WHERE t.forum_id = c.forum_id AND t.last_post_at > me.mark AND t.last_post_ordinal > 0)
	       END AS threads
	FROM forum_thread_counts AS c, me
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
SELECT (SELECT count(*) FROM notifications AS n
        LEFT JOIN applications AS na ON na.id = n.application_id
        WHERE n.user_id = sqlc.arg(user_id) AND n.read_at IS NULL
          AND (n.kind <> 'new_application' OR na.status = 'pending')) AS notifications,
       f.forum_id, f.threads, f.participated, f.watching
FROM (SELECT 1) AS one
LEFT JOIN by_forum AS f ON 1;

-- ForumUnread is the forum index's unread column (GET /forums,
-- TKT-01M29CA9A5): per forum, the viewer's threads with an unread post. It
-- is MeCounts's threads count with participation and notifications left
-- out, by the same rule and in the same shape, so the index and the /me
-- badge agree; a test holds the two together. It reads threads,
-- forum_thread_counts, users, read_positions and watches, and no per-post
-- row: read state is a position per thread (docs/04 "read_positions").
--
-- Only forums with an unread thread have a row.
-- name: ForumUnread :many
WITH me AS (
	SELECT coalesce(mark_all_read_at, -1) AS mark FROM users AS u WHERE u.id = sqlc.arg(user_id)
),
active AS (
	SELECT c.forum_id,
	       CASE WHEN me.mark < 0 THEN c.threads
	            ELSE (SELECT count(*) FROM threads AS t INDEXED BY threads_forum_active
	                  WHERE t.forum_id = c.forum_id AND t.last_post_at > me.mark AND t.last_post_ordinal > 0)
	       END AS threads
	FROM forum_thread_counts AS c, me
),
touched AS (
	SELECT thread_id FROM read_positions WHERE user_id = sqlc.arg(user_id)
	UNION SELECT thread_id FROM watches WHERE user_id = sqlc.arg(user_id) AND thread_id IS NOT NULL
),
not_unread AS (
	SELECT t.forum_id, count(*) AS threads
	FROM touched
	JOIN threads AS t ON t.id = touched.thread_id
	CROSS JOIN me
	LEFT JOIN read_positions AS rp ON rp.user_id = sqlc.arg(user_id) AND rp.thread_id = t.id
	LEFT JOIN watches AS w ON w.user_id = sqlc.arg(user_id) AND w.thread_id = t.id
	WHERE t.last_post_at > me.mark AND t.last_post_ordinal > 0
	  AND NOT (t.last_post_ordinal > coalesce(rp.last_read_ordinal, 0) AND coalesce(w.level, '') <> 'mute')
	GROUP BY t.forum_id
)
SELECT a.forum_id, CAST(a.threads - coalesce(n.threads, 0) AS INTEGER) AS threads
FROM active AS a LEFT JOIN not_unread AS n ON n.forum_id = a.forum_id
WHERE a.threads - coalesce(n.threads, 0) > 0;

-- Drafts (docs/07 "Drafts", TKT-01M3K9XBJA): a reply draft, one per user per
-- thread (drafts_thread), and a new-thread draft, one per user per forum
-- (drafts_new_thread). Each key's row is kept once it exists: a delete turns
-- it into a tombstone, deleted = 1, so its revision survives and only grows.
-- Every revision is computed here from the stored value, never taken from Go,
-- so no clock reaches it. The reads return tombstones too, so the service can
-- answer a tombstone's revision.

-- name: GetThreadDraft :one
SELECT * FROM drafts WHERE user_id = ? AND thread_id = ?;

-- PUT /drafts/{thread_id}: the one row for (user, thread) takes the new
-- source, reply target and attachments, keeping its created_at, and a
-- tombstone comes back to life. A new row starts at revision 1 and each save
-- adds one. The conflict target names the partial index's predicate, which
-- SQLite requires to match it. attachments is a JSON array of the hashes the
-- attach check answered (TKT-01M3V73DP6); saving one adds no reference. An
-- empty parameter, a caller's zero value, is stored as '[]', as the column's
-- default would be, rather than refused by its CHECK (Terva review 1754).
-- name: UpsertThreadDraft :one
INSERT INTO drafts (user_id, thread_id, source, reply_to, attachments, created_at, updated_at, revision)
VALUES (sqlc.arg(user_id), sqlc.arg(thread_id), sqlc.arg(source), sqlc.arg(reply_to), coalesce(nullif(CAST(sqlc.arg(attachments) AS TEXT), ''), '[]'), sqlc.arg(now), sqlc.arg(now), 1)
ON CONFLICT (user_id, thread_id) WHERE thread_id IS NOT NULL DO UPDATE SET
	source = excluded.source,
	reply_to = excluded.reply_to,
	attachments = excluded.attachments,
	updated_at = excluded.updated_at,
	deleted = 0,
	revision = drafts.revision + 1
RETURNING *;

-- DELETE /drafts/{thread_id}: a live draft becomes a tombstone one revision
-- on, holding no text and no attachments. A tombstone, or a key with no row,
-- is left as it is.
-- name: DeleteThreadDraft :execrows
UPDATE drafts SET deleted = 1, source = '', reply_to = NULL, title = NULL, attachments = '[]',
	revision = revision + 1, updated_at = sqlc.arg(now)
WHERE user_id = sqlc.arg(user_id) AND thread_id = sqlc.arg(thread_id) AND deleted = 0;

-- name: GetForumDraft :one
SELECT * FROM drafts WHERE user_id = ? AND forum_id = ? AND thread_id IS NULL;

-- PUT /forums/{slug}/draft: as UpsertThreadDraft, for the one new-thread
-- draft per (user, forum). The title and attachments are replaced as a
-- whole, so a save without them clears them.
-- name: UpsertForumDraft :one
INSERT INTO drafts (user_id, forum_id, title, source, attachments, created_at, updated_at, revision)
VALUES (sqlc.arg(user_id), sqlc.arg(forum_id), sqlc.arg(title), sqlc.arg(source), coalesce(nullif(CAST(sqlc.arg(attachments) AS TEXT), ''), '[]'), sqlc.arg(now), sqlc.arg(now), 1)
ON CONFLICT (user_id, forum_id) WHERE thread_id IS NULL DO UPDATE SET
	title = excluded.title,
	source = excluded.source,
	attachments = excluded.attachments,
	updated_at = excluded.updated_at,
	deleted = 0,
	revision = drafts.revision + 1
RETURNING *;

-- DELETE /forums/{slug}/draft: as DeleteThreadDraft.
-- name: DeleteForumDraft :execrows
UPDATE drafts SET deleted = 1, source = '', reply_to = NULL, title = NULL, attachments = '[]',
	revision = revision + 1, updated_at = sqlc.arg(now)
WHERE user_id = sqlc.arg(user_id) AND forum_id = sqlc.arg(forum_id) AND thread_id IS NULL AND deleted = 0;
