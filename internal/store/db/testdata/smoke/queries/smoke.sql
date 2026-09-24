-- name: LiveMembers :many
SELECT forum_id, user_id, role, handle
FROM smoke_members
WHERE forum_id = ? AND deleted = 0;

-- name: MemberByHandle :one
SELECT forum_id, user_id FROM smoke_members WHERE lower(handle) = lower(?);

-- sqlc (v1.31.1) accepts MATCH against a named column, and ORDER BY rank.
-- It rejects the whole-table form `WHERE smoke_search MATCH ?` ("column does
-- not exist") and the table-valued form `FROM smoke_search(?)`. A search
-- across every column goes in views/, or names the columns in the MATCH
-- string with FTS5's {title source_plain}: filter.
-- name: SearchSmoke :many
SELECT thread_id, ordinal FROM smoke_search WHERE smoke_search.source_plain MATCH ? ORDER BY rank;
