-- Identity: users, the IdP identities that sign them in, optional local
-- passwords, and server-side sessions (docs/04, docs/05).
--
-- Every table here is AUTHORITATIVE: the only copy, so it must be backed up.
-- Foreign keys carry no ON DELETE clause, which is SQLite's NO ACTION: users
-- are deactivated, never removed (docs/04, "Conventions").

CREATE TABLE users (
	id               INTEGER PRIMARY KEY,
	-- 3 to 30 of a-z 0-9 _ -, starting with a letter or digit, stored
	-- lowercased (docs/04). It must be TEXT: a BLOB bound here is stored as
	-- a BLOB, whose length counts every byte while GLOB stops at a NUL, and
	-- which NOCASE does not fold. GLOB is case-sensitive, so the character class
	-- also refuses uppercase. length() and GLOB both stop at a NUL, so the
	-- byte length must equal the character length: that refuses an embedded
	-- NUL and anything after it, and any multi-byte character. NOCASE on the
	-- UNIQUE index is exact because a handle is ASCII, and it stands behind
	-- the CHECK. domain.ValidateHandle is the same rule in Go, and a test
	-- holds the two together.
	handle           TEXT    NOT NULL COLLATE NOCASE UNIQUE
		CHECK (typeof(handle) = 'text'
		       AND length(handle) BETWEEN 3 AND 30
		       AND length(CAST(handle AS BLOB)) = length(handle)
		       AND handle NOT GLOB '*[^a-z0-9_-]*'
		       AND substr(handle, 1, 1) GLOB '[a-z0-9]'),
	display_name     TEXT    NOT NULL,
	avatar_blob      TEXT,
	status           TEXT    NOT NULL DEFAULT 'active'
		CHECK (status IN ('active', 'pending', 'suspended', 'deactivated')),
	role             TEXT    NOT NULL DEFAULT 'member'
		CHECK (role IN ('member', 'moderator', 'admin')),
	kind             TEXT    NOT NULL DEFAULT 'human'
		CHECK (kind IN ('human', 'agent')),
	bio_source       TEXT    NOT NULL DEFAULT '',
	timezone         TEXT,
	locale           TEXT,
	prefs_json       TEXT    NOT NULL DEFAULT '{}',
	-- "Mark all read": posts older than this count as read everywhere
	-- (docs/06). Null until the user first uses it.
	mark_all_read_at INTEGER,
	created_at       INTEGER NOT NULL,
	updated_at       INTEGER NOT NULL,
	last_seen_at     INTEGER
);

-- One user, many identities. user_id is null for an approval-mode applicant
-- who has signed in with an IdP but has no account yet (docs/05).
-- provider_key names a block in config.toml, not a row, so removing a
-- provider leaves its identities inert rather than orphaned.
CREATE TABLE identities (
	id              INTEGER PRIMARY KEY,
	user_id         INTEGER REFERENCES users (id),
	provider_key    TEXT    NOT NULL,
	subject         TEXT    NOT NULL,
	email           TEXT,
	email_verified  INTEGER NOT NULL DEFAULT 0 CHECK (email_verified IN (0, 1)),
	issuer          TEXT    NOT NULL,
	last_login_at   INTEGER,
	raw_claims_json TEXT,
	created_at      INTEGER NOT NULL,
	updated_at      INTEGER NOT NULL,
	UNIQUE (provider_key, subject)
);
CREATE INDEX identities_user ON identities (user_id);

-- Off by default; exists so an operator can enable it without a migration.
CREATE TABLE local_credentials (
	user_id       INTEGER PRIMARY KEY REFERENCES users (id),
	password_hash TEXT    NOT NULL,
	created_at    INTEGER NOT NULL,
	updated_at    INTEGER NOT NULL
);

-- Server-side sessions. The cookie carries a random 256-bit id; this table
-- holds only its 32-byte hash, and no column could hold the plaintext.
-- A 'user' session has a user. A 'pre_user' session is an approval-mode
-- applicant: an identity and no user (docs/05). last_used_at is the only
-- column that changes, so it stands in for updated_at.
CREATE TABLE sessions (
	id_hash                BLOB    PRIMARY KEY
		CHECK (typeof(id_hash) = 'blob' AND length(id_hash) = 32),
	user_id                INTEGER REFERENCES users (id),
	identity_id            INTEGER REFERENCES identities (id),
	kind                   TEXT    NOT NULL CHECK (kind IN ('user', 'pre_user')),
	created_at             INTEGER NOT NULL,
	expires_at             INTEGER NOT NULL,
	last_used_at           INTEGER NOT NULL,
	ip                     TEXT,
	user_agent             TEXT,
	oidc_provider_key      TEXT,
	oidc_refresh_token_enc BLOB,
	CHECK ((kind = 'user' AND user_id IS NOT NULL)
	    OR (kind = 'pre_user' AND user_id IS NULL AND identity_id IS NOT NULL))
);
CREATE INDEX sessions_user ON sessions (user_id);
CREATE INDEX sessions_expiry ON sessions (expires_at);
