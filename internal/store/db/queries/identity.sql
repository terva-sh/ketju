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

-- RecordIdentityLogin notes a sign-in on the identity: when, and the claims
-- the IdP asserted this time, which may have changed since the last.
-- name: RecordIdentityLogin :exec
UPDATE identities
SET last_login_at = ?, raw_claims_json = ?, email = ?, email_verified = ?, updated_at = ?
WHERE id = ?;

-- IdentityHolder is what a sign-in decides a session on: the user an
-- identity belongs to, if any, and that user's status. internal/auth/session
-- reads it in the transaction that inserts the session, so nothing can
-- change it between the check and the row.
-- The reason and end of a suspension come with the status, for the
-- "account unavailable" page, and updated_at, which the Reactivate button's
-- cookie is bound to (docs/05 "Login flow").
-- name: IdentityHolder :one
SELECT i.user_id, u.status, u.status_reason, u.status_until, u.updated_at
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
--
-- A pre_user session resolves only while its identity exists and still has
-- no user (TKT-01M29CKRG9). The moment approval links the identity to a
-- user, the applicant's cookie stops resolving, even if the code that
-- approved forgot to delete it: a pre-user session never becomes a member
-- session in place, and the applicant signs in again, through
-- session.IssueUser's status check.
-- name: LookupSession :one
SELECT s.id_hash, s.user_id, s.identity_id, s.kind, s.created_at, s.expires_at, s.last_used_at,
       s.oidc_provider_key,
       u.handle, u.display_name, u.avatar_blob, u.status, u.role, u.kind AS user_kind,
       u.timezone, u.locale, u.prefs_json, u.mark_all_read_at
FROM sessions s
LEFT JOIN users u ON u.id = s.user_id
LEFT JOIN identities i ON i.id = s.identity_id
WHERE s.id_hash = sqlc.arg(id_hash)
  AND s.expires_at > sqlc.arg(expires_after)
  AND s.last_used_at > sqlc.arg(last_used_after)
  AND (s.kind = 'user' OR (s.kind = 'pre_user' AND i.id IS NOT NULL AND i.user_id IS NULL));

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

-- DeletePreUserSessions ends every applicant session bound to an identity:
-- what approving an application does in its own transaction (TKT-01M29CP2MG).
-- LookupSession already refuses them once the identity has a user; this
-- removes the rows.
-- name: DeletePreUserSessions :exec
DELETE FROM sessions WHERE kind = 'pre_user' AND identity_id = ?;

-- SweepSessions deletes every session LookupSession would refuse: the exact
-- complement of its liveness test. internal/auth/session binds both cutoffs
-- from session.ExpiredBefore, so the sweeper deletes nothing a lookup at the
-- same moment would still accept (TKT-01M29C5Y2S).
-- name: SweepSessions :execrows
DELETE FROM sessions
WHERE expires_at <= sqlc.arg(expires_after)
   OR last_used_at <= sqlc.arg(last_used_after);

-- name: SetUserRole :exec
UPDATE users SET role = ?, updated_at = ? WHERE id = ?;

-- SetUserStatus is every status change but anonymizing (internal/perm,
-- lifecycle.go). It writes the reason and end with the status, so a
-- suspension's reason never outlives it.
-- name: SetUserStatus :exec
UPDATE users SET status = ?, status_reason = ?, status_until = ?, updated_at = ? WHERE id = ?;

-- AnonymizeUser is docs/05's anonymization of the account's row: the
-- placeholder handle, and nothing left that names the person. The account
-- becomes deactivated, so it drops out of every list of active accounts and
-- is served as a deactivated account is. The schema's triggers keep it so.
-- name: AnonymizeUser :exec
UPDATE users
SET handle = 'deleted-user-' || id, display_name = '', avatar_blob = NULL, bio_source = '', anonymized = 1,
    status = 'deactivated', status_reason = '', status_until = NULL, updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id);

-- DeleteUserIdentities removes every identity of an anonymized account
-- (docs/05). An application's identity_id is set to null by its foreign
-- key. The sessions naming the identities go first (DeleteIdentitySessions).
-- name: DeleteUserIdentities :exec
DELETE FROM identities WHERE user_id = ?;

-- DeleteIdentitySessions ends every session bound to one of the user's
-- identities, a pre-user session from before an approval included, whose
-- user_id is null and which DeleteUserSessions would miss.
-- name: DeleteIdentitySessions :exec
DELETE FROM sessions WHERE identity_id IN (SELECT i.id FROM identities i WHERE i.user_id = sqlc.arg(user_id));

-- The authors on one page of a thread, in one query rather than one per post.
-- name: UsersByIDs :many
SELECT * FROM users WHERE id IN (sqlc.slice('ids')) ORDER BY id;

-- name: UsersByHandlePrefix :many
-- Mention autocomplete (GET /api/v1/users?prefix=). lo is the prefix as
-- typed and hi is lo with '{' appended, which sorts after every handle
-- character. Both compare under the column's NOCASE collation, which folds
-- the case, so this is a range search on the handle index. A handle ending in '-' is left out: the mention parser
-- reads a trailing '-' as punctuation, so such a handle cannot be mentioned
-- and suggesting it would insert a mention of somebody else.
SELECT handle, display_name, avatar_blob, role, kind FROM users
WHERE handle >= sqlc.arg(lo) AND handle < sqlc.arg(hi)
  AND status = 'active'
  AND substr(handle, -1) <> '-'
ORDER BY handle
LIMIT sqlc.arg(lim);
