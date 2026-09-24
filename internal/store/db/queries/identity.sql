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

-- IdentityHolder is what a sign-in decides a session on: the user an
-- identity belongs to, if any, and that user's status. internal/auth/session
-- reads it in the transaction that inserts the session, so nothing can
-- change it between the check and the row.
-- name: IdentityHolder :one
SELECT i.user_id, u.status
FROM identities i
LEFT JOIN users u ON u.id = i.user_id
WHERE i.id = ?;

-- Sessions are addressed only by the hash of their id. There is deliberately
-- no query that takes the plaintext: hashing belongs to the session store.

-- name: CreateSession :exec
INSERT INTO sessions (id_hash, user_id, identity_id, kind, created_at, expires_at, last_used_at,
                      ip, user_agent, oidc_provider_key)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetSession :one
SELECT * FROM sessions WHERE id_hash = ?;

-- LookupSession is the whole of session resolution in one statement: the
-- session, alive by both expiry rules, and the user it belongs to (none for a
-- pre_user session). internal/auth/session binds the cutoffs from one
-- function, so the sweeper and this cannot disagree about what is expired.
-- The user fields are the ones GET /me needs, so resolving a request and
-- answering /me share this query.
-- name: LookupSession :one
SELECT s.id_hash, s.user_id, s.identity_id, s.kind, s.created_at, s.expires_at, s.last_used_at,
       s.oidc_provider_key,
       u.handle, u.display_name, u.avatar_blob, u.status, u.role, u.kind AS user_kind,
       u.timezone, u.locale, u.prefs_json, u.mark_all_read_at
FROM sessions s
LEFT JOIN users u ON u.id = s.user_id
WHERE s.id_hash = sqlc.arg(id_hash)
  AND s.expires_at > sqlc.arg(expires_after)
  AND s.last_used_at > sqlc.arg(last_used_after);

-- TouchSession slides last_used_at only when the stored value is due, so the
-- once-per-five-minutes rule holds in the statement itself: two requests that
-- both read a due value write once between them. It also requires the row to
-- be alive by the same cutoffs as LookupSession, at the time of the write, so
-- a touch that lands just after the idle boundary cannot revive the session.
-- name: TouchSession :execrows
UPDATE sessions SET last_used_at = sqlc.arg(now)
WHERE id_hash = sqlc.arg(id_hash)
  AND last_used_at <= sqlc.arg(due_before)
  AND expires_at > sqlc.arg(expires_after)
  AND last_used_at > sqlc.arg(last_used_after);

-- name: ListUserSessions :many
SELECT id_hash, kind, created_at, expires_at, last_used_at, ip, user_agent
FROM sessions WHERE user_id = ? ORDER BY last_used_at DESC;

-- name: DeleteSession :exec
DELETE FROM sessions WHERE id_hash = ?;

-- name: DeleteUserSessions :exec
DELETE FROM sessions WHERE user_id = ?;

-- name: SetUserRole :exec
UPDATE users SET role = ?, updated_at = ? WHERE id = ?;

-- The authors on one page of a thread, in one query rather than one per post.
-- name: UsersByIDs :many
SELECT * FROM users WHERE id IN (sqlc.slice('ids')) ORDER BY id;
