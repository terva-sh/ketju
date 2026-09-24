-- Flags, the audit log, blobs and runtime settings (docs/04 "flags",
-- "audit_log", "blobs", "settings"; docs/08 "Flags (reports)"). All
-- authoritative. A thread id is a plain INTEGER, as in 0005: threads is a
-- projection and has no rows a foreign key could rely on.

-- A report on a post, feeding the moderator queue. A flag is resolved or
-- dismissed in place and never deleted: the queue's history is what a
-- moderator reads to see whether a member has been reported before.
CREATE TABLE flags (
	id              INTEGER PRIMARY KEY,
	thread_id       INTEGER NOT NULL,
	ordinal         INTEGER NOT NULL CHECK (ordinal > 0),
	reporter_id     INTEGER NOT NULL REFERENCES users (id),
	reason          TEXT    NOT NULL CHECK (reason IN ('spam', 'abuse', 'off-topic', 'other')),
	note            TEXT    NOT NULL DEFAULT '',
	created_at      INTEGER NOT NULL,
	status          TEXT    NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'resolved', 'dismissed')),
	resolved_by     INTEGER REFERENCES users (id),
	resolved_at     INTEGER,
	resolution_note TEXT    NOT NULL DEFAULT '',
	-- "other" is a reason only with text saying what it is (docs/08).
	CHECK (reason <> 'other' OR note <> ''),
	-- An open flag has no resolution, and a closed one says who and when.
	CHECK ((status = 'open') = (resolved_by IS NULL AND resolved_at IS NULL))
);

-- The queue: open flags newest first, and a post's open flags for the
-- auto-hide threshold.
CREATE INDEX flags_status ON flags (status, created_at);
CREATE INDEX flags_post ON flags (thread_id, ordinal, status);

CREATE TRIGGER flags_never_deleted
BEFORE DELETE ON flags
BEGIN
	SELECT RAISE(ABORT, 'flags are resolved or dismissed, never deleted');
END;

-- INSERT OR REPLACE removes the conflicting row without firing DELETE
-- triggers unless recursive_triggers is on, so a replace is refused here, at
-- the insert, where it fires whatever the connection's pragmas.
CREATE TRIGGER flags_never_replaced
BEFORE INSERT ON flags
WHEN EXISTS (SELECT 1 FROM flags WHERE id = NEW.id)
BEGIN
	SELECT RAISE(ABORT, 'flags are resolved or dismissed, never replaced');
END;

-- Every moderation and administration action. docs/04 says append-only "by
-- convention"; these triggers make it a property of the schema, so a
-- mistaken UPDATE fails where it is written rather than in an audit.
CREATE TABLE audit_log (
	id           INTEGER PRIMARY KEY,
	at           INTEGER NOT NULL,
	actor_id     INTEGER REFERENCES users (id),
	action       TEXT    NOT NULL CHECK (action <> ''),
	target_type  TEXT    NOT NULL DEFAULT '',
	target_id    TEXT    NOT NULL DEFAULT '',
	details_json TEXT    NOT NULL DEFAULT '{}' CHECK (json_valid(details_json)),
	ip           TEXT    NOT NULL DEFAULT ''
);

CREATE INDEX audit_log_at ON audit_log (at);
CREATE INDEX audit_log_target ON audit_log (target_type, target_id, at);

CREATE TRIGGER audit_log_no_update
BEFORE UPDATE ON audit_log
BEGIN
	SELECT RAISE(ABORT, 'audit_log is append-only');
END;

CREATE TRIGGER audit_log_no_delete
BEFORE DELETE ON audit_log
BEGIN
	SELECT RAISE(ABORT, 'audit_log is append-only');
END;

-- REPLACE, as for flags: the implicit delete fires no DELETE trigger unless
-- recursive_triggers is on (review 395).
CREATE TRIGGER audit_log_no_replace
BEFORE INSERT ON audit_log
WHEN EXISTS (SELECT 1 FROM audit_log WHERE id = NEW.id)
BEGIN
	SELECT RAISE(ABORT, 'audit_log is append-only');
END;

-- An uploaded file, stored at blobs/sha256/xx/<hash> (docs/03 "Attachments")
-- and named here by its hash. refcount counts the posts referring to it.
-- unreferenced_since is set when refcount reaches zero and cleared when it
-- rises, so blob GC can wait "seven days at zero" rather than seven days from
-- upload (TKT-01M29C5Y2S). The CHECK makes the two agree in both directions:
-- a blob at zero without the timestamp would never be collected. A
-- quarantined blob is never served: BlobForServing
-- is the one lookup the serve path may use, and it asks for status 'ok'.
CREATE TABLE blobs (
	hash               TEXT    PRIMARY KEY
		CHECK (length(hash) = 64 AND hash NOT GLOB '*[^0-9a-f]*'),
	size               INTEGER NOT NULL CHECK (size >= 0),
	mime               TEXT    NOT NULL,
	original_name      TEXT    NOT NULL DEFAULT '',
	uploaded_by        INTEGER REFERENCES users (id),
	uploaded_at        INTEGER NOT NULL,
	refcount           INTEGER NOT NULL DEFAULT 0 CHECK (refcount >= 0),
	status             TEXT    NOT NULL DEFAULT 'ok' CHECK (status IN ('ok', 'quarantined')),
	unreferenced_since INTEGER,
	CHECK ((refcount = 0) = (unreferenced_since IS NOT NULL))
) WITHOUT ROWID;

CREATE INDEX blobs_unreferenced ON blobs (unreferenced_since) WHERE unreferenced_since IS NOT NULL;

-- Runtime settings: what an admin changes without a restart. The key list
-- and each key's type live in Go (internal/settings.Keys), which refuses an
-- unknown key or an ill-typed value before anything reaches this table; the
-- table only insists on valid JSON. A missing row means the key's default.
CREATE TABLE settings (
	key        TEXT    PRIMARY KEY CHECK (key <> ''),
	value_json TEXT    NOT NULL CHECK (json_valid(value_json)),
	updated_at INTEGER NOT NULL,
	updated_by INTEGER REFERENCES users (id)
) WITHOUT ROWID;
