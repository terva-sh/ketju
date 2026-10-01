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

-- name: ForumWatchLevel :one
SELECT level FROM watches WHERE user_id = ? AND forum_id = ?;

-- Watches and mutes (docs/04 "watches", TKT-01M29CRJ29): one row per user
-- per thread and per user per forum. Setting a level replaces the row's
-- level and keeps its created_at; clearing it deletes the row, since no
-- row is what "follow the forum" and "nothing" mean. The conflict target
-- names the partial index's predicate, which SQLite requires to match it,
-- and the update takes its values from excluded only, because sqlc does
-- not bind a parameter inside ON CONFLICT ... DO UPDATE on SQLite (v1.31.1).
-- name: SetThreadWatch :exec
INSERT INTO watches (user_id, thread_id, level, created_at, updated_at)
VALUES (sqlc.arg(user_id), sqlc.arg(thread_id), sqlc.arg(level), sqlc.arg(now), sqlc.arg(now))
ON CONFLICT (user_id, thread_id) WHERE thread_id IS NOT NULL DO UPDATE SET
	level = excluded.level,
	updated_at = excluded.updated_at;

-- name: ClearThreadWatch :exec
DELETE FROM watches WHERE user_id = ? AND thread_id = ?;

-- name: SetForumWatch :exec
INSERT INTO watches (user_id, forum_id, level, created_at, updated_at)
VALUES (sqlc.arg(user_id), sqlc.arg(forum_id), sqlc.arg(level), sqlc.arg(now), sqlc.arg(now))
ON CONFLICT (user_id, forum_id) WHERE forum_id IS NOT NULL DO UPDATE SET
	level = excluded.level,
	updated_at = excluded.updated_at;

-- name: ClearForumWatch :exec
DELETE FROM watches WHERE user_id = ? AND forum_id = ?;

-- ReadBoundary is the viewer's place in a thread as the thread page, its
-- unread lens and the read routes' answers use it (docs/06 "Read tracking",
-- TKT-01M29CTY7K): every post after the boundary is unread and none at or
-- before it is. It is the stored position, moved forward past the posts
-- users.mark_all_read_at has made read: to just before the first post past
-- the position that was written after the mark, or to the last post when
-- there is none. With no mark it is the stored position, 0 with no row.
--
-- Posts are appended in time order, so every post after that first one is
-- after the mark too, and the posts past the boundary are exactly the ones
-- UnreadCount counts. rowColumns in package views computes the same column
-- for a thread list's rows, and TestUnreadMatchesUnreadCount holds the
-- two together.
--
-- last_read_at is the row's, null with none; mark_all_read_at is the
-- user's, so the caller can say which of the two set the boundary.
-- name: ReadBoundary :one
SELECT CAST(coalesce(rp.last_read_ordinal, 0) AS INTEGER) AS last_read_ordinal,
       rp.last_read_at,
       u.mark_all_read_at,
       CAST(CASE
         WHEN u.mark_all_read_at IS NULL OR t.last_post_ordinal <= coalesce(rp.last_read_ordinal, 0)
           THEN coalesce(rp.last_read_ordinal, 0)
         WHEN t.last_post_at <= u.mark_all_read_at THEN t.last_post_ordinal
         ELSE coalesce((SELECT p.ordinal - 1 FROM post_meta AS p
                        WHERE p.thread_id = t.id AND p.ordinal > coalesce(rp.last_read_ordinal, 0)
                          AND p.created_at > u.mark_all_read_at
                        ORDER BY p.ordinal LIMIT 1), t.last_post_ordinal)
       END AS INTEGER) AS boundary
FROM threads AS t
JOIN users AS u ON u.id = sqlc.arg(user_id)
LEFT JOIN read_positions AS rp ON rp.user_id = u.id AND rp.thread_id = t.id
WHERE t.id = sqlc.arg(thread_id);

-- SetMarkAllRead is the site-level "mark all read" (PUT /me/read,
-- TKT-01M29CTY7K): everything written up to now is read. The mark only
-- moves forward, so a request from a device whose clock is behind, or one
-- that arrives late, leaves a later mark where it is. The answer is the mark
-- as it now stands.
-- name: SetMarkAllRead :one
UPDATE users SET mark_all_read_at = max(coalesce(mark_all_read_at, 0), CAST(sqlc.arg(now) AS INTEGER))
WHERE id = sqlc.arg(user_id)
RETURNING CAST(mark_all_read_at AS INTEGER) AS mark_all_read_at;

-- MarkForumRead is the forum-level "mark all read" (PUT /forums/{slug}/read,
-- docs/06 "Read tracking", TKT-01M29CTY7K): the viewer's position in each of
-- the forum's threads with a post moves to its last post, in one statement.
-- It never moves a position back, and a row it does not move keeps its
-- last_read_at, so the divider's time stays the time the place was last
-- set. A thread whose last post is at or before the viewer's site-level
-- mark is read already and gets no row. The forum's threads are read
-- through threads_forum_active from the mark on, so the statement reads
-- only the threads it may write.
--
-- The WHERE on the SELECT is what lets SQLite parse the ON CONFLICT clause
-- as the upsert's rather than as a join constraint.
-- name: MarkForumRead :execrows
INSERT INTO read_positions (user_id, thread_id, last_read_ordinal, last_read_at)
SELECT sqlc.arg(user_id), t.id, t.last_post_ordinal, sqlc.arg(now)
FROM threads AS t INDEXED BY threads_forum_active
WHERE t.forum_id = sqlc.arg(forum_id)
  AND t.last_post_at > coalesce((SELECT u.mark_all_read_at FROM users AS u WHERE u.id = sqlc.arg(user_id)), -1)
  AND t.last_post_ordinal > 0
ON CONFLICT (user_id, thread_id) DO UPDATE SET
	last_read_ordinal = excluded.last_read_ordinal,
	last_read_at = excluded.last_read_at
WHERE excluded.last_read_ordinal > read_positions.last_read_ordinal;

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
-- A thread whose effective level is mute counts nowhere (docs/04
-- "watches", TKT-01M29CRJ29). The effective level is the thread's own row,
-- then its forum's, coalesce(w.level, fw.level, ''), as
-- domain.EffectiveWatch says in Go. participated is a thread the viewer has
-- a post in that was not moved out; watching is a thread-level watch
-- (docs/06 "Thread list views": a forum-level watch drives notifications
-- only). Both count only unread threads, so a muted one is in neither.
--
-- new is the New list's rule (docs/06 "Thread list views", TKT-01M29CTY7K):
-- a thread with a post, started after the mark, that the viewer has no read
-- position in and has not muted. It starts, per forum, from the threads
-- with a post started after the mark: forum_thread_counts with no mark, or
-- a count over threads_forum_fresh after it. From that it takes the
-- touched ones, and adds back those of them that are new, as threads does.
-- The lists in package views apply the same rules row by row, and
-- TestBadgesMatchLists holds every count to its list.
--
-- The shape is for cost, because the shell runs this on every page load.
-- It starts from, per forum, the threads with a post after the mark, all of
-- which are unread to someone who never opened them. With no mark that is
-- forum_thread_counts, which triggers keep; with one it is a count per forum
-- over threads_forum_active, which reads only the index entries after the
-- mark (migration 0007). Neither reads every thread. Everything that depends
-- on the viewer starts from the viewer's own rows (read positions, watches,
-- posts), so it grows with what they have touched rather than with the
-- site: a forum's count is its threads the viewer has not touched, which
-- are unread unless the forum is muted, plus the touched ones that are
-- unread by their effective level. The forum's own row is one search on
-- watches_forum per forum.
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
	       END AS threads,
	       CASE WHEN me.mark < 0 THEN c.threads
	            ELSE (SELECT count(*) FROM threads AS t INDEXED BY threads_forum_fresh
	                  WHERE t.forum_id = c.forum_id AND t.created_at > me.mark AND t.last_post_ordinal > 0)
	       END AS fresh
	FROM forum_thread_counts AS c, me
),
mine AS (
	SELECT DISTINCT thread_id FROM post_meta WHERE author_id = sqlc.arg(user_id) AND moved = 0
),
touched AS (
	SELECT thread_id FROM read_positions WHERE user_id = sqlc.arg(user_id)
	UNION SELECT thread_id FROM watches WHERE user_id = sqlc.arg(user_id) AND thread_id IS NOT NULL
	UNION SELECT thread_id FROM mine
),
state AS (
	SELECT t.forum_id,
	       t.last_post_ordinal > 0 AS active,
	       t.last_post_ordinal > coalesce(rp.last_read_ordinal, 0)
	         AND coalesce(w.level, fw.level, '') <> 'mute' AS unread,
	       coalesce(w.level, '') = 'watch' AS watched,
	       m.thread_id IS NOT NULL AS participated,
	       t.last_post_at > me.mark AS recent,
	       t.last_post_ordinal > 0 AND t.created_at > me.mark AS fresh,
	       t.last_post_ordinal > 0 AND t.created_at > me.mark AND rp.user_id IS NULL
	         AND coalesce(w.level, fw.level, '') <> 'mute' AS new
	FROM touched
	JOIN threads AS t ON t.id = touched.thread_id
	CROSS JOIN me
	LEFT JOIN read_positions AS rp ON rp.user_id = sqlc.arg(user_id) AND rp.thread_id = t.id
	LEFT JOIN watches AS w ON w.user_id = sqlc.arg(user_id) AND w.thread_id = t.id
	LEFT JOIN watches AS fw ON fw.user_id = sqlc.arg(user_id) AND fw.forum_id = t.forum_id
	LEFT JOIN mine AS m ON m.thread_id = t.id
	-- Only threads with a post after the mark, or started after it: the rest
	-- are read by the mark and too old to be new, and are in neither of
	-- active's counts.
	WHERE t.last_post_at > me.mark OR t.created_at > me.mark
),
adjust AS (
	SELECT forum_id,
	       sum(active AND recent) AS touched,
	       sum(unread AND recent) AS unread,
	       sum(unread AND recent AND participated) AS participated,
	       sum(unread AND recent AND watched) AS watching,
	       sum(fresh) AS touched_fresh,
	       sum(new) AS new
	FROM state GROUP BY forum_id
),
by_forum AS (
	SELECT a.forum_id,
	       CAST(CASE WHEN fw.level = 'mute' THEN 0 ELSE a.threads - coalesce(j.touched, 0) END
	            + coalesce(j.unread, 0) AS INTEGER) AS threads,
	       CAST(coalesce(j.participated, 0) AS INTEGER) AS participated,
	       CAST(coalesce(j.watching, 0) AS INTEGER) AS watching,
	       CAST(CASE WHEN fw.level = 'mute' THEN 0 ELSE a.fresh - coalesce(j.touched_fresh, 0) END
	            + coalesce(j.new, 0) AS INTEGER) AS new
	FROM active AS a
	LEFT JOIN adjust AS j ON j.forum_id = a.forum_id
	LEFT JOIN watches AS fw ON fw.user_id = sqlc.arg(user_id) AND fw.forum_id = a.forum_id
)
SELECT (SELECT count(*) FROM notifications AS n
        LEFT JOIN applications AS na ON na.id = n.application_id
        WHERE n.user_id = sqlc.arg(user_id) AND n.read_at IS NULL
          AND (n.kind <> 'new_application' OR na.status = 'pending')) AS notifications,
       f.forum_id, f.threads, f.participated, f.watching,
       f.new
FROM (SELECT 1) AS one
LEFT JOIN by_forum AS f ON f.threads > 0 OR f.new > 0;

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
touched_state AS (
	SELECT t.forum_id, count(*) AS touched,
	       sum(t.last_post_ordinal > coalesce(rp.last_read_ordinal, 0)
	           AND coalesce(w.level, fw.level, '') <> 'mute') AS unread
	FROM touched
	JOIN threads AS t ON t.id = touched.thread_id
	CROSS JOIN me
	LEFT JOIN read_positions AS rp ON rp.user_id = sqlc.arg(user_id) AND rp.thread_id = t.id
	LEFT JOIN watches AS w ON w.user_id = sqlc.arg(user_id) AND w.thread_id = t.id
	LEFT JOIN watches AS fw ON fw.user_id = sqlc.arg(user_id) AND fw.forum_id = t.forum_id
	WHERE t.last_post_at > me.mark AND t.last_post_ordinal > 0
	GROUP BY t.forum_id
),
counted AS (
	SELECT a.forum_id,
	       CAST(CASE WHEN fw.level = 'mute' THEN 0 ELSE a.threads - coalesce(n.touched, 0) END
	            + coalesce(n.unread, 0) AS INTEGER) AS threads
	FROM active AS a
	LEFT JOIN touched_state AS n ON n.forum_id = a.forum_id
	LEFT JOIN watches AS fw ON fw.user_id = sqlc.arg(user_id) AND fw.forum_id = a.forum_id
)
SELECT forum_id, threads FROM counted WHERE threads > 0;

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

-- MentionedOrdinals is the posts in a thread whose current revision
-- mentions a user, for the mentions lens (TKT-01M29CTY6E). The mentions
-- projection replaces a post's rows on every edit, so a mention an edit
-- removed is gone here once projected. A scan of the thread's prefix of
-- the primary key.
-- name: MentionedOrdinals :many
SELECT ordinal FROM mentions WHERE thread_id = ? AND mentioned_user_id = ? ORDER BY ordinal;
