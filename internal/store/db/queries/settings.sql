-- Writes go through internal/settings.Set, which refuses an unknown key or
-- an ill-typed value and writes the audit row in the same transaction.
-- name: PutSetting :exec
INSERT INTO settings (key, value_json, updated_at, updated_by) VALUES (?, ?, ?, ?)
ON CONFLICT (key) DO UPDATE SET
	value_json = excluded.value_json,
	updated_at = excluded.updated_at,
	updated_by = excluded.updated_by;

-- name: GetSetting :one
SELECT * FROM settings WHERE key = ?;

-- name: ListSettings :many
SELECT * FROM settings ORDER BY key;
