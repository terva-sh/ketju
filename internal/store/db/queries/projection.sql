-- name: GetThread :one
SELECT * FROM threads WHERE id = ?;

-- name: ThreadTags :many
SELECT tag FROM thread_tags WHERE thread_id = ? ORDER BY tag;

-- name: GetProjectionState :one
SELECT * FROM projection_state WHERE thread_id = ?;

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

-- name: SetThreadProjected :exec
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

-- name: SetPostRevision :execrows
UPDATE post_meta SET current_revision = ?, edited_at = ?, reply_to = ?, log_offset = ?
WHERE thread_id = ? AND ordinal = ?;

-- name: SetPostDeleted :execrows
UPDATE post_meta SET deleted = ?, log_offset = ? WHERE thread_id = ? AND ordinal = ?;

-- name: DeleteMentions :exec
DELETE FROM mentions WHERE thread_id = ? AND ordinal = ?;

-- name: InsertMention :exec
INSERT OR IGNORE INTO mentions (thread_id, ordinal, mentioned_user_id) VALUES (?, ?, ?);

-- name: SetProjectionState :exec
INSERT INTO projection_state (thread_id, log_length, updated_at) VALUES (?, ?, ?)
ON CONFLICT (thread_id) DO UPDATE SET log_length = excluded.log_length, updated_at = excluded.updated_at;
