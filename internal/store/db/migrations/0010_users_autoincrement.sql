-- users.id becomes AUTOINCREMENT, so a member id is never handed out twice
-- (TKT-01M3KB7FXC, docs/04 "users").
--
-- Thread logs name members by id, and a plain INTEGER PRIMARY KEY gives a new
-- row max(id)+1. After a database is restored from a backup older than the
-- logs, the next member to join would be given the id of a member the backup
-- does not have, and that member's old posts would show as the newcomer's.
-- With AUTOINCREMENT the next id is above sqlite_sequence's high-water mark,
-- which ketju reindex raises above every id the logs name
-- (db.RaiseMemberIDFloor) and nothing ever lowers.
--
-- SQLite cannot add AUTOINCREMENT in place, so the table is rebuilt as its
-- documentation describes: create the new table, copy every row with its id,
-- drop the old one, rename. The copy seeds sqlite_sequence with max(id). The
-- foreign keys that reference users (id) name it by name and still do after
-- the rename. The runner turns foreign_keys off around every migration and
-- runs foreign_key_check before it commits (migrate.go), because the DROP
-- would otherwise delete every referencing row's parent. The columns and
-- constraints are 0002's exactly; see there for why each is what it is.

CREATE TABLE users_new (
	id               INTEGER PRIMARY KEY AUTOINCREMENT,
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
	mark_all_read_at INTEGER,
	created_at       INTEGER NOT NULL,
	updated_at       INTEGER NOT NULL,
	last_seen_at     INTEGER
);

INSERT INTO users_new (id, handle, display_name, avatar_blob, status, role, kind, bio_source,
	timezone, locale, prefs_json, mark_all_read_at, created_at, updated_at, last_seen_at)
SELECT id, handle, display_name, avatar_blob, status, role, kind, bio_source,
	timezone, locale, prefs_json, mark_all_read_at, created_at, updated_at, last_seen_at
FROM users;

DROP TABLE users;

ALTER TABLE users_new RENAME TO users;
