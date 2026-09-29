-- Agent accounts and their credentials (docs/04 "agents", docs/05 "Agent
-- accounts", TKT-01M29P9RSW).
--
-- An agent is a users row with kind = 'agent'. This table holds what only an
-- agent has: its rate-limit overrides, and who provisioned it. A null
-- override means the site default (settings agents.write_burst and
-- agents.write_refill_seconds).
--
-- AUTHORITATIVE.
CREATE TABLE agents (
	user_id               INTEGER PRIMARY KEY REFERENCES users (id),
	bucket_burst          INTEGER CHECK (bucket_burst >= 1),
	-- At most a week, settings.AgentRefillMaxSeconds: a refill becomes a
	-- time.Duration, and a larger count would overflow it.
	bucket_refill_seconds INTEGER CHECK (bucket_refill_seconds BETWEEN 1 AND 604800),
	created_by            INTEGER REFERENCES users (id),
	created_at            INTEGER NOT NULL
);

-- An agent's bearer credential. The token is never stored: token_hash is
-- the SHA-256 of its random bytes, as sessions.id_hash is, and the token is
-- shown once, in the response that issues it. It has no expiry; revoking
-- it is the only way it ends, and a revoked row is kept as the record of
-- who revoked it and when.
--
-- It is not a session. A credential lives here, not in sessions, so it has
-- no cookie, no idle or hard expiry for the sweeper to apply, and no way
-- through the session chokepoint (internal/auth/session).
--
-- AUTHORITATIVE.
CREATE TABLE agent_credentials (
	id           INTEGER PRIMARY KEY,
	user_id      INTEGER NOT NULL REFERENCES users (id),
	token_hash   BLOB    NOT NULL UNIQUE CHECK (length(token_hash) = 32),
	created_by   INTEGER REFERENCES users (id),
	created_at   INTEGER NOT NULL,
	last_used_at INTEGER,
	revoked_at   INTEGER,
	revoked_by   INTEGER REFERENCES users (id)
);

-- One live credential per agent. Issuing a new one revokes the old in the
-- same transaction, which is how a leaked token is rotated.
CREATE UNIQUE INDEX agent_credentials_live ON agent_credentials (user_id) WHERE revoked_at IS NULL;
