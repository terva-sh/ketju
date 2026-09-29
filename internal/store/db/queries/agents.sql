-- Agent accounts and their credentials (TKT-01M29P9RSW). Writes go through
-- internal/service/users (provisioning, limits) and internal/auth/agentcred
-- (issuing and revoking), each with its audit row in the same transaction.

-- name: CreateAgent :exec
INSERT INTO agents (user_id, created_by, created_at) VALUES (?, ?, ?);

-- name: GetAgent :one
SELECT * FROM agents WHERE user_id = ?;

-- A null override means the site default.
-- name: SetAgentLimits :execrows
UPDATE agents SET bucket_burst = sqlc.narg(burst), bucket_refill_seconds = sqlc.narg(refill_seconds)
WHERE user_id = sqlc.arg(user_id);

-- name: InsertAgentCredential :exec
INSERT INTO agent_credentials (user_id, token_hash, created_by, created_at) VALUES (?, ?, ?, ?);

-- RevokeAgentCredentials ends the account's live credential, if it has one,
-- and reports how many it ended: 0 or 1, by agent_credentials_live.
-- name: RevokeAgentCredentials :execrows
UPDATE agent_credentials SET revoked_at = CAST(sqlc.arg(now) AS INTEGER), revoked_by = sqlc.narg(revoked_by)
WHERE user_id = sqlc.arg(user_id) AND revoked_at IS NULL;

-- LookupAgentCredential resolves a token's hash to a live credential: not
-- revoked, on an agent account that is active. Anything else is no row, so
-- a credential stops working the moment its account leaves active, whatever
-- revoked it or failed to.
-- name: LookupAgentCredential :one
SELECT c.id, c.user_id, c.last_used_at, u.role, a.bucket_burst, a.bucket_refill_seconds
FROM agent_credentials c
JOIN users u ON u.id = c.user_id
LEFT JOIN agents a ON a.user_id = c.user_id
WHERE c.token_hash = sqlc.arg(token_hash)
  AND c.revoked_at IS NULL
  AND u.kind = 'agent'
  AND u.status = 'active'
  AND u.anonymized = 0;

-- TouchAgentCredential records a use, at most once per interval: the
-- statement repeats the check, so concurrent requests write once.
-- name: TouchAgentCredential :execrows
UPDATE agent_credentials SET last_used_at = CAST(sqlc.arg(now) AS INTEGER)
WHERE id = sqlc.arg(id) AND revoked_at IS NULL
  AND (last_used_at IS NULL OR last_used_at <= CAST(sqlc.arg(due_before) AS INTEGER));

-- name: LiveAgentCredential :one
SELECT id, created_at, last_used_at FROM agent_credentials
WHERE user_id = ? AND revoked_at IS NULL;
