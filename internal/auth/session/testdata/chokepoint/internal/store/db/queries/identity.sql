-- The door's own file, with the door's name used twice and a qualified
-- insert. The first CreateSession is the door, and it smuggles a second
-- insert in beside the one it may hold; the second borrows its name.

-- name: CreateSession :exec
INSERT INTO sessions (id_hash, kind) VALUES (?, ?);
INSERT INTO sessions (id_hash, kind) VALUES (?, 'user');

-- name: CreateSession :exec
INSERT INTO sessions (id_hash, kind) VALUES (?, 'user');

-- name: Qualified :exec
INSERT INTO [main].[sessions] (id_hash, kind) VALUES (?, 'user');
