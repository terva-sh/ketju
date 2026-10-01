-- The search index's plain statements (docs/04 "search (FTS)",
-- TKT-01M29CRJ3B). The FTS5 commands that keep the virtual table `search` in
-- step with search_docs, and every MATCH, are hand-written: in
-- internal/store/db/search.go and internal/store/db/views/search.go. The
-- writes below are called from search.go alone, which
-- TestOnlySearchGoWritesTheIndex holds.

-- name: GetSearchDoc :one
SELECT id, revision, author_id, created_at, title, source_plain
FROM search_docs WHERE thread_id = ? AND ordinal = ?;

-- name: InsertSearchDoc :one
INSERT INTO search_docs (thread_id, ordinal, revision, author_id, created_at, title, source_plain)
VALUES (?, ?, ?, ?, ?, ?, ?)
RETURNING id;

-- name: UpdateSearchDoc :exec
UPDATE search_docs
SET revision = ?, author_id = ?, created_at = ?, title = ?, source_plain = ?
WHERE id = ?;

-- name: DeleteSearchDoc :exec
DELETE FROM search_docs WHERE id = ?;

-- name: ListSearchDocOrdinals :many
SELECT ordinal FROM search_docs WHERE thread_id = ? ORDER BY ordinal;

-- name: GetSearchState :one
SELECT log_length FROM search_state WHERE thread_id = ?;

-- Catch-up reads every thread's indexed length in one statement and
-- compares it with the logs, as the projection's does.
-- name: ListSearchLengths :many
SELECT thread_id, log_length FROM search_state;

-- name: SetSearchState :exec
INSERT INTO search_state (thread_id, log_length, updated_at) VALUES (?, ?, ?)
ON CONFLICT (thread_id) DO UPDATE SET log_length = excluded.log_length, updated_at = excluded.updated_at;

-- name: DeleteSearchState :exec
DELETE FROM search_state WHERE thread_id = ?;

-- name: GetSearchVersion :one
SELECT version FROM search_meta WHERE id = 1;

-- name: SetSearchVersion :exec
INSERT INTO search_meta (id, version) VALUES (1, ?)
ON CONFLICT (id) DO UPDATE SET version = excluded.version;

-- The statements ClearSearch runs after FTS5's delete-all, one per table in
-- SearchTables.
-- name: ClearSearchDocs :exec
DELETE FROM search_docs;

-- name: ClearSearchState :exec
DELETE FROM search_state;

-- name: ClearSearchMeta :exec
DELETE FROM search_meta;
