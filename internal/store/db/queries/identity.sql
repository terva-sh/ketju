-- name: CreateUser :one
INSERT INTO users (handle, display_name, status, role, kind, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
RETURNING id;

-- name: GetUser :one
SELECT * FROM users WHERE id = ?;

-- name: GetUserByHandle :one
SELECT * FROM users WHERE handle = ?;

-- name: CreateIdentity :one
INSERT INTO identities (user_id, provider_key, subject, email, email_verified, issuer, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
RETURNING id;

-- name: GetIdentityBySubject :one
SELECT * FROM identities WHERE provider_key = ? AND subject = ?;

-- Sessions are addressed only by the hash of their id. There is deliberately
-- no query that takes the plaintext: hashing belongs to the session store.

-- name: CreateSession :exec
INSERT INTO sessions (id_hash, user_id, identity_id, kind, created_at, expires_at, last_used_at,
                      ip, user_agent, oidc_provider_key)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetSession :one
SELECT * FROM sessions WHERE id_hash = ?;

-- name: TouchSession :exec
UPDATE sessions SET last_used_at = ? WHERE id_hash = ?;

-- name: DeleteSession :exec
DELETE FROM sessions WHERE id_hash = ?;

-- name: DeleteUserSessions :exec
DELETE FROM sessions WHERE user_id = ?;

-- name: SetUserRole :exec
UPDATE users SET role = ?, updated_at = ? WHERE id = ?;

-- The authors on one page of a thread, in one query rather than one per post.
-- name: UsersByIDs :many
SELECT * FROM users WHERE id IN (sqlc.slice('ids')) ORDER BY id;
