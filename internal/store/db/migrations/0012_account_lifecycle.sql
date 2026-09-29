-- The account lifecycle: a suspension's reason and end, and the flag an
-- anonymized account carries (docs/04 "users", docs/05 "Account lifecycle",
-- TKT-01M29CKRFS).
--
-- status_reason is the moderator's reason for a suspension, shown to the
-- member at sign-in (docs/08: there is no shadow banning). status_until is
-- when a suspension ends, in Unix ms, and null when no end is set. Every
-- status change writes all three columns (SetUserStatus), so a reason never
-- outlives the suspension it explained.
ALTER TABLE users ADD COLUMN status_reason TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN status_until INTEGER;

-- anonymized is what suppresses attribution at render time: the handle is
-- deleted-user-<id>, and the display name, avatar and bio are gone. Post
-- bodies are not touched, because they are in append-only logs; erasing body
-- text is the offline redaction tool (docs/03 "Hard redaction").
ALTER TABLE users ADD COLUMN anonymized INTEGER NOT NULL DEFAULT 0 CHECK (anonymized IN (0, 1));

-- Registration has always refused a handle starting with deleted-user
-- (docs/04 "users"), so no row can match. The update makes the flag and the
-- prefix, which marked an anonymized account until now, agree by
-- construction rather than by that argument.
UPDATE users
SET anonymized = 1, display_name = '', avatar_blob = NULL, bio_source = '', status = 'deactivated',
    status_reason = '', status_until = NULL
WHERE handle GLOB 'deleted-user*';

-- Anonymizing is irreversible (docs/08 "Primitives"). The trigger makes that
-- a property of the schema rather than of the one query that anonymizes, so
-- no write path, the command line included, can give an anonymized account
-- back its name, a display name, an avatar, a bio or another status.
CREATE TRIGGER users_anonymized_is_final
BEFORE UPDATE ON users
WHEN OLD.anonymized = 1
	AND (NEW.anonymized <> 1 OR NEW.handle <> OLD.handle OR NEW.display_name <> ''
	     OR NEW.avatar_blob IS NOT NULL OR NEW.bio_source <> '' OR NEW.status <> OLD.status)
BEGIN
	SELECT RAISE(ABORT, 'an anonymized account cannot change');
END;

-- An account becomes anonymized under its placeholder handle and no other,
-- so the flag cannot be set on a row that still shows a name.
CREATE TRIGGER users_anonymized_handle
BEFORE UPDATE OF anonymized ON users
WHEN NEW.anonymized = 1 AND OLD.anonymized = 0 AND NEW.handle <> 'deleted-user-' || NEW.id
BEGIN
	SELECT RAISE(ABORT, 'an anonymized account has the handle deleted-user-<id>');
END;

-- An account is anonymized by an update of the account it was, never
-- created anonymized.
CREATE TRIGGER users_insert_anonymized
BEFORE INSERT ON users
WHEN NEW.anonymized <> 0
BEGIN
	SELECT RAISE(ABORT, 'an account is anonymized, never created so');
END;
