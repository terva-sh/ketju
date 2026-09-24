-- Borrowing the door's name does not make a migration the door.
-- name: CreateSession :exec
INSERT INTO sessions (id_hash, kind) VALUES (x'01', 'user');
