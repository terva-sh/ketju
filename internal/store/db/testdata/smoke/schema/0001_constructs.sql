-- Not a real schema. Every SQL construct the Phase 1 schema tickets plan to
-- use, in one place, so a sqlc upgrade that stops parsing one of them fails
-- `go generate` here rather than in whichever ticket first needs it.

-- CHECK constraints and a WITHOUT ROWID table.
CREATE TABLE smoke_members (
	forum_id INTEGER NOT NULL,
	user_id  INTEGER NOT NULL,
	role     TEXT    NOT NULL CHECK (role IN ('member', 'moderator')),
	handle   TEXT    NOT NULL,
	deleted  INTEGER NOT NULL DEFAULT 0 CHECK (deleted IN (0, 1)),
	PRIMARY KEY (forum_id, user_id)
) WITHOUT ROWID;

-- An expression index and a partial index.
CREATE UNIQUE INDEX smoke_members_handle ON smoke_members (lower(handle));
CREATE INDEX smoke_members_live ON smoke_members (forum_id) WHERE deleted = 0;

-- A trigger that refuses a change with RAISE.
CREATE TRIGGER smoke_members_no_role_downgrade
BEFORE UPDATE OF role ON smoke_members
WHEN OLD.role = 'moderator' AND NEW.role = 'member'
BEGIN
	SELECT RAISE(ABORT, 'demote through the audit path');
END;

-- An FTS5 table over the columns docs/04 names for search.
CREATE VIRTUAL TABLE smoke_search USING fts5(
	thread_id UNINDEXED, ordinal UNINDEXED, revision UNINDEXED,
	title, source_plain
);
