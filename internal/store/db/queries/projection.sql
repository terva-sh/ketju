-- name: GetThread :one
SELECT * FROM threads WHERE id = ?;

-- name: ThreadTags :many
SELECT tag FROM thread_tags WHERE thread_id = ? ORDER BY tag;

-- name: GetProjectionState :one
SELECT * FROM projection_state WHERE thread_id = ?;

-- Startup catch-up reads every thread's projected length in one statement
-- and compares it with the log sizes a directory walk finds (TKT-01M29C5Y1S).
-- name: ListProjectionLengths :many
SELECT thread_id, log_length FROM projection_state;

-- The statements ClearProjections runs, one per table in ProjectionTables.
-- name: ClearThreads :exec
DELETE FROM threads;

-- name: ClearThreadTags :exec
DELETE FROM thread_tags;

-- name: ClearPostMeta :exec
DELETE FROM post_meta;

-- name: ClearMentions :exec
DELETE FROM mentions;

-- name: ClearProjectionState :exec
DELETE FROM projection_state;

-- name: ClearForumThreadCounts :exec
DELETE FROM forum_thread_counts;

-- name: ClearPostMoves :exec
DELETE FROM post_moves;

-- The projection writer's statements (TKT-01M29C2WW5). Every one sets an
-- absolute value, never an increment, so replaying a range the database has
-- already seen converges on the same rows.

-- A ThreadCreated resets the whole row: replayed from the start of the log,
-- it must undo whatever later records did before they are applied again.
-- name: UpsertThread :exec
INSERT INTO threads (id, forum_id, slug, title, author_id, created_at, post_count, last_post_at,
                     last_post_author_id, last_post_ordinal, locked, pinned_scope, pinned_until,
                     archived, log_length_projected)
VALUES (?, ?, ?, ?, ?, ?, 0, ?, ?, 0, 0, NULL, NULL, 0, ?)
ON CONFLICT (id) DO UPDATE SET
	forum_id = excluded.forum_id, slug = excluded.slug, title = excluded.title,
	author_id = excluded.author_id, created_at = excluded.created_at, post_count = 0,
	last_post_at = excluded.last_post_at, last_post_author_id = excluded.last_post_author_id,
	last_post_ordinal = 0, locked = 0, pinned_scope = NULL, pinned_until = NULL, archived = 0,
	log_length_projected = excluded.log_length_projected;

-- name: DeleteThreadTags :exec
DELETE FROM thread_tags WHERE thread_id = ?;

-- name: InsertThreadTag :exec
INSERT OR IGNORE INTO thread_tags (thread_id, tag) VALUES (?, ?);

-- name: SetThreadLastPost :execrows
UPDATE threads SET post_count = ?, last_post_at = ?, last_post_author_id = ?, last_post_ordinal = ?
WHERE id = ?;

-- A copy merged into a thread that already held posts raises post_count, the
-- latest ordinal, and leaves last_post_* where they were (TKT-01M29CXJZZ).
-- name: SetThreadPostCount :execrows
UPDATE threads SET post_count = ? WHERE id = ?;

-- The thread's newest post that is shown in its lists' "last post": not
-- moved out, and not a copy merged into a thread that already had posts.
-- Deleted posts count, as applyPost counts them when they are written. A
-- PostsSplitOut recomputes last_post_* from it (TKT-01M29CXJZZ). Newest by
-- time, then by ordinal: a copy keeps its original's time, so a thread a
-- move opened holds copies whose times do not rise with their ordinals,
-- and applyCopy picks its last post by time (Terva review 2014).
-- name: NewestListedPost :one
SELECT p.ordinal, p.author_id, p.created_at FROM post_meta AS p
WHERE p.thread_id = sqlc.arg(thread_id) AND p.moved = 0
  AND NOT EXISTS (SELECT 1 FROM post_moves AS m
                  WHERE m.target_thread_id = p.thread_id AND m.target_ordinal = p.ordinal
                    AND m.opens_thread = 0)
ORDER BY p.created_at DESC, p.ordinal DESC LIMIT 1;

-- name: SetThreadTitle :execrows
UPDATE threads SET title = ?, slug = ? WHERE id = ?;

-- name: SetThreadForum :execrows
UPDATE threads SET forum_id = ? WHERE id = ?;

-- name: SetThreadLocked :execrows
UPDATE threads SET locked = ? WHERE id = ?;

-- name: SetThreadPinned :execrows
UPDATE threads SET pinned_scope = ?, pinned_until = ? WHERE id = ?;

-- Clears the pin only when its scope matches, so removing a forum pin does
-- not silently remove a global one (docs/03). SQLite evaluates every SET
-- expression against the row as it was, so both CASEs see the old scope.
-- name: UnpinThread :execrows
UPDATE threads SET
	pinned_until = CASE WHEN pinned_scope = sqlc.arg('scope') THEN NULL ELSE pinned_until END,
	pinned_scope = CASE WHEN pinned_scope = sqlc.arg('scope') THEN NULL ELSE pinned_scope END
WHERE id = sqlc.arg('id');

-- name: SetThreadArchived :execrows
UPDATE threads SET archived = 1 WHERE id = ?;

-- name: SetThreadProjected :execrows
UPDATE threads SET log_length_projected = ? WHERE id = ?;

-- A Post resets every column, for the same reason UpsertThread does.
-- name: UpsertPostMeta :exec
INSERT INTO post_meta (thread_id, ordinal, author_id, created_at, current_revision, edited_at,
                       reply_to, deleted, moved, log_offset)
VALUES (?, ?, ?, ?, ?, NULL, ?, 0, 0, ?)
ON CONFLICT (thread_id, ordinal) DO UPDATE SET
	author_id = excluded.author_id, created_at = excluded.created_at,
	current_revision = excluded.current_revision, edited_at = NULL, reply_to = excluded.reply_to,
	deleted = 0, moved = 0, log_offset = excluded.log_offset;

-- A null reply_to keeps the row's: an Edit without one keeps the post's
-- reply link, as the index entry does (TKT-01M29CA9BP).
-- name: SetPostRevision :execrows
UPDATE post_meta SET current_revision = sqlc.arg(current_revision), edited_at = sqlc.arg(edited_at),
	reply_to = coalesce(sqlc.narg(reply_to), reply_to), log_offset = sqlc.arg(log_offset)
WHERE thread_id = sqlc.arg(thread_id) AND ordinal = sqlc.arg(ordinal);

-- SetPostRevision for a grace Edit (TKT-01M29CKRHS). It does not mark an
-- unedited post edited, and on a post already edited it moves edited_at to
-- its own time, as the index entry keeps the edited flag and moves to this
-- revision. The two read paths then agree on edited_at.
-- name: SetPostRevisionKeepEdited :execrows
UPDATE post_meta SET current_revision = sqlc.arg(current_revision),
	edited_at = CASE WHEN edited_at IS NULL THEN NULL ELSE CAST(sqlc.arg(edited_at) AS INTEGER) END,
	reply_to = coalesce(sqlc.narg(reply_to), reply_to), log_offset = sqlc.arg(log_offset)
WHERE thread_id = sqlc.arg(thread_id) AND ordinal = sqlc.arg(ordinal);

-- The post's author, for the projection writer to tell an author's own
-- Edit, Delete or Restore from a moderator's (TKT-01M29CKRGS).
-- name: PostAuthor :one
SELECT author_id FROM post_meta WHERE thread_id = ? AND ordinal = ?;

-- log_offset is the current revision's record, as the index entry's Offset
-- is, so a Delete or Restore (which is not a revision) leaves it alone.
-- name: SetPostDeleted :execrows
UPDATE post_meta SET deleted = ? WHERE thread_id = ? AND ordinal = ?;

-- name: DeleteMentions :exec
DELETE FROM mentions WHERE thread_id = ? AND ordinal = ?;

-- name: InsertMention :exec
INSERT OR IGNORE INTO mentions (thread_id, ordinal, mentioned_user_id) VALUES (?, ?, ?);

-- A post's quote backlinks (migration 0025, TKT-01M3YME9AZ): a Post or Edit
-- replaces them from its source, and a Redacted post marker removes them.
-- name: DeletePostQuotes :exec
DELETE FROM post_quotes WHERE thread_id = ? AND ordinal = ?;

-- name: InsertPostQuote :exec
INSERT OR IGNORE INTO post_quotes (thread_id, ordinal, quoted_ordinal) VALUES (?, ?, ?);

-- name: ClearPostQuotes :exec
DELETE FROM post_quotes;

-- QuotedBy is who quotes these posts, for a thread page's quoted_by: each
-- quoting post whose post_meta row is neither deleted nor moved. The caller
-- also drops a quoting post its index does not show as visible, since the
-- index is ahead of post_meta and knows hidden posts (docs/06 "Post
-- anatomy").
-- name: QuotedBy :many
SELECT pq.quoted_ordinal, pq.ordinal FROM post_quotes AS pq
JOIN post_meta AS pm ON pm.thread_id = pq.thread_id AND pm.ordinal = pq.ordinal
WHERE pq.thread_id = sqlc.arg(thread_id) AND pq.quoted_ordinal IN (sqlc.slice('quoted'))
  AND pm.deleted = 0 AND pm.moved = 0
ORDER BY pq.quoted_ordinal, pq.ordinal;

-- name: SetProjectionState :exec
INSERT INTO projection_state (thread_id, log_length, updated_at) VALUES (?, ?, ?)
ON CONFLICT (thread_id) DO UPDATE SET log_length = excluded.log_length, updated_at = excluded.updated_at;

-- A post's direct replies when its thread is not open (post_meta_reply_to),
-- in ordinal order as the index would give them. The replies endpoint uses it
-- only once the projection has reached the length it serves.
-- name: RepliesTo :many
SELECT ordinal FROM post_meta WHERE thread_id = ? AND reply_to = ? ORDER BY ordinal;

-- The tags of a page of threads, for a thread list.
-- name: TagsForThreads :many
SELECT thread_id, tag FROM thread_tags WHERE thread_id IN (sqlc.slice('thread_ids')) ORDER BY thread_id, tag;

-- Moved posts (migration 0026, TKT-01M29CXJZZ). The source and the target of
-- a move are projected independently and in either order, so each side's
-- statement inserts the row when it is missing and otherwise sets only the
-- columns that side owns.

-- The source's PostsSplitOut, for one ordinal: source_revision is the moved
-- post's revision as the source projected it, which a moved post keeps. No
-- row is written for a post the source's projection lacks.
-- name: UpsertSplitMove :execrows
INSERT INTO post_moves (source_thread_id, source_ordinal, source_revision, target_thread_id, split_offset)
SELECT p.thread_id, p.ordinal, p.current_revision, sqlc.arg(target_thread_id), CAST(sqlc.arg(split_offset) AS INTEGER)
FROM post_meta AS p
WHERE p.thread_id = sqlc.arg(source_thread_id) AND p.ordinal = sqlc.arg(source_ordinal)
ON CONFLICT (source_thread_id, source_ordinal) DO UPDATE SET
	source_revision = excluded.source_revision, target_thread_id = excluded.target_thread_id,
	split_offset = excluded.split_offset;

-- name: SetPostMoved :execrows
UPDATE post_meta SET moved = 1 WHERE thread_id = ? AND ordinal = ?;

-- The target's PostsMergedIn, for one source ordinal.
-- name: UpsertMergedMove :exec
INSERT INTO post_moves (source_thread_id, source_ordinal, target_thread_id, merged_offset, opens_thread)
VALUES (sqlc.arg(source_thread_id), sqlc.arg(source_ordinal), sqlc.arg(target_thread_id),
        CAST(sqlc.arg(merged_offset) AS INTEGER), sqlc.arg(opens_thread))
ON CONFLICT (source_thread_id, source_ordinal) DO UPDATE SET
	target_thread_id = excluded.target_thread_id, merged_offset = excluded.merged_offset,
	opens_thread = excluded.opens_thread;

-- The target's copied Post, a Post whose origin names the source post. It
-- is the move's copy only when the row's PostsMergedIn is in this thread,
-- ahead of it in the log, and it is the first such Post: a move writes its
-- copies right after its PostsMergedIn. Any other Post with an origin, such
-- as one written by hand, is no move's and touches no row (Terva review
-- 2011). Updates only; the PostsMergedIn wrote the row.
-- name: SetCopyMove :execrows
UPDATE post_moves SET target_ordinal = CAST(sqlc.arg(target_ordinal) AS INTEGER)
WHERE source_thread_id = sqlc.arg(source_thread_id) AND source_ordinal = sqlc.arg(source_ordinal)
  AND target_thread_id = sqlc.arg(target_thread_id)
  AND merged_offset IS NOT NULL AND merged_offset < CAST(sqlc.arg(copy_offset) AS INTEGER)
  AND (target_ordinal IS NULL OR target_ordinal >= CAST(sqlc.arg(target_ordinal) AS INTEGER));

-- The source ordinals of source_thread_id's move rows whose source
-- revision is not known yet: the target was projected first.
-- name: MovesWithoutSourceRevision :many
SELECT source_ordinal FROM post_moves
WHERE source_thread_id = ? AND source_revision IS NULL ORDER BY source_ordinal;

-- Sets a row's source revision, read from the source's log, when the
-- target is projected ahead of the source's PostsSplitOut. It is the value
-- that split would write: a moved post accepts no later record.
-- name: SetMoveSourceRevision :exec
UPDATE post_moves SET source_revision = ?
WHERE source_thread_id = ? AND source_ordinal = ? AND source_revision IS NULL;

-- The move row of a copy, by where it landed.
-- name: MoveOfCopy :one
SELECT source_thread_id, source_ordinal, source_revision FROM post_moves
WHERE target_thread_id = ? AND target_ordinal = ?;

-- name: GetPostMove :one
SELECT * FROM post_moves WHERE source_thread_id = ? AND source_ordinal = ?;

-- Whether an Edit at revision of the post at (target_thread_id,
-- target_ordinal) is one a move carried rather than a new one: the post is
-- a copy, and the revision is at most the source post's, or the source's
-- PostsSplitOut is not projected yet so its revision is unknown. A carried
-- Edit writes no audit row and notifies nobody, as a copied Post does.
-- name: IsCarriedEdit :one
SELECT EXISTS (
	SELECT 1 FROM post_moves
	WHERE target_thread_id = sqlc.arg(target_thread_id)
	  AND target_ordinal = CAST(sqlc.arg(target_ordinal) AS INTEGER)
	  AND (source_revision IS NULL OR CAST(sqlc.arg(revision) AS INTEGER) <= source_revision)
);

-- Every move row that is not finished: its copy is not written, the
-- source's PostsSplitOut is not projected, or the copy has not reached the
-- source post's revision. copy_revision is null when the copy's post_meta
-- row is missing. In source order, which is the order the finisher writes.
-- name: UnfinishedMoves :many
SELECT m.source_thread_id, m.source_ordinal, m.source_revision, m.target_thread_id, m.target_ordinal,
       m.split_offset, m.merged_offset, m.opens_thread, c.current_revision AS copy_revision
FROM post_moves AS m
LEFT JOIN post_meta AS c ON c.thread_id = m.target_thread_id AND c.ordinal = m.target_ordinal
WHERE m.target_ordinal IS NULL OR m.source_revision IS NULL OR c.current_revision IS NULL
   OR c.current_revision < m.source_revision
ORDER BY m.source_thread_id, m.source_ordinal;


-- UnfinishedMoves for the moves into or out of one thread.
-- name: UnfinishedMovesForThread :many
SELECT m.source_thread_id, m.source_ordinal, m.source_revision, m.target_thread_id, m.target_ordinal,
       m.split_offset, m.merged_offset, m.opens_thread, c.current_revision AS copy_revision
FROM post_moves AS m
LEFT JOIN post_meta AS c ON c.thread_id = m.target_thread_id AND c.ordinal = m.target_ordinal
WHERE (m.source_thread_id = sqlc.arg(thread_id) OR m.target_thread_id = sqlc.arg(thread_id))
  AND (m.target_ordinal IS NULL OR m.source_revision IS NULL OR c.current_revision IS NULL
       OR c.current_revision < m.source_revision)
ORDER BY m.source_thread_id, m.source_ordinal;
