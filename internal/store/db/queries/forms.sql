-- Forms and their versions (docs/04 "forms, form_versions"). Versions are
-- written through internal/service/forms, which parses the schema first and
-- writes the audit row in the same transaction.

-- name: GetFormByKey :one
SELECT * FROM forms WHERE key = ?;

-- name: GetFormByID :one
SELECT * FROM forms WHERE id = ?;

-- name: GetFormVersion :one
SELECT * FROM form_versions WHERE form_id = ? AND version = ?;

-- name: ListFormVersions :many
SELECT version, created_by, created_at FROM form_versions WHERE form_id = ? ORDER BY version DESC;

-- name: MaxFormVersion :one
SELECT CAST(coalesce(max(version), 0) AS INTEGER) FROM form_versions WHERE form_id = ?;

-- name: InsertFormVersion :exec
INSERT INTO form_versions (form_id, version, schema_json, created_by, created_at) VALUES (?, ?, ?, ?, ?);

-- name: SetActiveFormVersion :exec
UPDATE forms SET active_version = ?, updated_at = ? WHERE id = ?;
