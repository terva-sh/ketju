-- A statement before the first named query is still read.
INSERT INTO sessions (id_hash, kind) VALUES (?, 'user');

-- name: Harmless :one
SELECT count(*) FROM sessions;
