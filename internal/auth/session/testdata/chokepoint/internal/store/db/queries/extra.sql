-- name: CreateSession :exec
INSERT INTO sessions (id_hash, kind) VALUES (?, ?);

-- A second insert, under another name.
-- name: CreateSessionAgain :exec
INSERT INTO sessions (id_hash, kind) VALUES (?, 'user');

-- name: AdoptSession :exec
UPDATE sessions SET last_used_at = ?, user_id = ? WHERE id_hash = ?;

-- name: TouchAgain :exec
UPDATE OR REPLACE sessions SET "kind" = 'user' WHERE id_hash = ?;

-- name: Touch :exec
-- UPDATE sessions SET user_id = 1 in a comment is not a statement.
UPDATE sessions SET last_used_at = ? WHERE id_hash = ?;
