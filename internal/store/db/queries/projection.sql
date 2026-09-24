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
