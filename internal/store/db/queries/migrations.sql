-- name: ListMigrations :many
SELECT version, name, checksum, created_at
FROM schema_migrations
ORDER BY version;

-- name: InsertMigration :exec
INSERT INTO schema_migrations (version, name, checksum, created_at)
VALUES (?, ?, ?, ?);
