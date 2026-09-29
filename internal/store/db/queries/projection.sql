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

-- log_offset is the current revision's record, as the index entry's Offset
-- is, so a Delete or Restore (which is not a revision) leaves it alone.
-- name: SetPostDeleted :execrows
UPDATE post_meta SET deleted = ? WHERE thread_id = ? AND ordinal = ?;

-- name: DeleteMentions :exec
DELETE FROM mentions WHERE thread_id = ? AND ordinal = ?;

-- name: InsertMention :exec
INSERT OR IGNORE INTO mentions (thread_id, ordinal, mentioned_user_id) VALUES (?, ?, ?);

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
