-- Approval-mode registration: forms and their versions, applications, the
-- notes moderators leave on them, the block on an identity, and the
-- application a notification is about (docs/04 "applications (approval
-- mode)", "forms, form_versions"; docs/05 "mode = approval", "Forms";
-- TKT-01M29CKRDQ).
--
-- Every table here is AUTHORITATIVE: the only copy, so it must be backed up.
-- Foreign keys follow 0002's NO ACTION convention, with one exception:
-- applications.identity_id is ON DELETE SET NULL, explained there.

-- A form, named by key, and the version an applicant is shown. A form and its
-- first version are inserted in one transaction, so the foreign key from
-- active_version is deferred to commit. active_version is NOT NULL, so a form
-- committed without a version, or naming one that does not exist, is refused
-- at commit; there is no trigger fallback (review 1358).
CREATE TABLE forms (
	id             INTEGER PRIMARY KEY,
	key            TEXT    NOT NULL UNIQUE
		CHECK (key GLOB '[a-z]*' AND key NOT GLOB '*[^a-z0-9_]*'),
	title          TEXT    NOT NULL,
	active_version INTEGER NOT NULL,
	created_at     INTEGER NOT NULL,
	updated_at     INTEGER NOT NULL,
	FOREIGN KEY (id, active_version) REFERENCES form_versions (form_id, version)
		DEFERRABLE INITIALLY DEFERRED
);

-- One version of a form's declarative schema (docs/05 "Forms"). A version is
-- never changed once written: an application names the version it answered,
-- and editing that version would silently change the meaning of every answer
-- already given. An admin's edit writes the next version instead. created_by
-- is null for the version this migration seeds.
CREATE TABLE form_versions (
	form_id     INTEGER NOT NULL REFERENCES forms (id),
	version     INTEGER NOT NULL CHECK (version >= 1),
	schema_json TEXT    NOT NULL CHECK (json_valid(schema_json)),
	created_by  INTEGER REFERENCES users (id),
	created_at  INTEGER NOT NULL,
	PRIMARY KEY (form_id, version)
);

-- Immutable by trigger, as audit_log is in 0006. A convention would not stop
-- INSERT OR REPLACE, whose implicit delete fires no DELETE trigger unless
-- recursive_triggers is on, so a replace is refused at the insert.
CREATE TRIGGER form_versions_no_update
BEFORE UPDATE ON form_versions
BEGIN
	SELECT RAISE(ABORT, 'form_versions are immutable; write a new version');
END;

CREATE TRIGGER form_versions_no_delete
BEFORE DELETE ON form_versions
BEGIN
	SELECT RAISE(ABORT, 'form_versions are immutable; write a new version');
END;

CREATE TRIGGER form_versions_no_replace
BEFORE INSERT ON form_versions
WHEN EXISTS (SELECT 1 FROM form_versions WHERE form_id = NEW.form_id AND version = NEW.version)
BEGIN
	SELECT RAISE(ABORT, 'form_versions are immutable; write a new version');
END;

-- An application to join, answered against one form version.
--
-- identity_id is ON DELETE SET NULL. Account linking removes identities
-- (docs/07 DELETE /settings/identities/{id}) and so does anonymization
-- (docs/05). Under NO ACTION an identity that had ever applied could never be
-- removed. NO ACTION exists because users are never deleted; identities are,
-- and the application outlives the identity with a null identity_id.
--
-- user_id is the member an approval created. It is set on approval, so the
-- application still names its member after the identity is removed.
--
-- A CHECK runs when each statement runs, not at commit. An approval creates
-- the user first, then sets status and user_id in one UPDATE (TKT-01M29CP2MG,
-- review 1344). A withdrawal records when (decided_at) but has no deciding
-- moderator and no note.
CREATE TABLE applications (
	id            INTEGER PRIMARY KEY,
	identity_id   INTEGER REFERENCES identities (id) ON DELETE SET NULL,
	form_id       INTEGER NOT NULL,
	form_version  INTEGER NOT NULL,
	answers_json  TEXT    NOT NULL CHECK (json_valid(answers_json)),
	status        TEXT    NOT NULL DEFAULT 'pending'
		CHECK (status IN ('pending', 'approved', 'denied', 'withdrawn')),
	user_id       INTEGER REFERENCES users (id),
	decided_by    INTEGER REFERENCES users (id),
	decided_at    INTEGER,
	decision_note TEXT,
	created_at    INTEGER NOT NULL,
	FOREIGN KEY (form_id, form_version) REFERENCES form_versions (form_id, version),
	CHECK ((status = 'pending') = (decided_at IS NULL)),
	CHECK ((status = 'approved') = (user_id IS NOT NULL)),
	CHECK (decided_by IS NULL OR status IN ('approved', 'denied')),
	CHECK (decision_note IS NULL OR status IN ('approved', 'denied'))
);

-- At most one pending application per identity. A null identity_id is not
-- a value, so applications whose identity was removed never collide.
CREATE UNIQUE INDEX applications_one_pending ON applications (identity_id) WHERE status = 'pending';
-- Prior applications from the same identity, shown beside the one reviewed.
CREATE INDEX applications_identity ON applications (identity_id, created_at);
-- The review list and the queue.
CREATE INDEX applications_status ON applications (status, created_at, id);

-- Free-text notes moderators leave on an application (docs/05 step 5).
-- docs/04's one decision_note is the decision's reason, not a discussion, so
-- the notes are rows: appended without rewriting the application, and each
-- names its author.
CREATE TABLE application_notes (
	id             INTEGER PRIMARY KEY,
	application_id INTEGER NOT NULL REFERENCES applications (id),
	author_id      INTEGER NOT NULL REFERENCES users (id),
	body           TEXT    NOT NULL CHECK (length(body) BETWEEN 1 AND 2000),
	created_at     INTEGER NOT NULL
);
CREATE INDEX application_notes_application ON application_notes (application_id, created_at);

-- A block keeps the identity with user_id NULL and refuses its
-- re-applications (docs/05 step 5). Two columns rather than a table: a block
-- has one state per identity, and the form's check would pay a join for
-- nothing. A blocked identity never has a user, so a block cannot be undone
-- by linking the identity to an account, and a member cannot be blocked
-- this way; the three triggers below hold that however the row is written.
-- Clearing both columns unblocks and passes them.
ALTER TABLE identities ADD COLUMN blocked_at INTEGER;
ALTER TABLE identities ADD COLUMN blocked_by INTEGER REFERENCES users (id);

CREATE TRIGGER identities_blocked_has_no_user
BEFORE UPDATE OF user_id ON identities
WHEN NEW.blocked_at IS NOT NULL AND NEW.user_id IS NOT NULL
BEGIN
	SELECT RAISE(ABORT, 'a blocked identity cannot have a user');
END;

CREATE TRIGGER identities_block_needs_no_user
BEFORE UPDATE OF blocked_at ON identities
WHEN NEW.blocked_at IS NOT NULL AND NEW.user_id IS NOT NULL
BEGIN
	SELECT RAISE(ABORT, 'an identity with a user cannot be blocked');
END;

CREATE TRIGGER identities_insert_blocked
BEFORE INSERT ON identities
WHEN NEW.blocked_at IS NOT NULL AND NEW.user_id IS NOT NULL
BEGIN
	SELECT RAISE(ABORT, 'a blocked identity cannot have a user');
END;

-- The application a new_application or application_decided notification is
-- about. thread_id and ordinal name posts, so neither kind could say which
-- application it meant. It is required for exactly those two kinds, on
-- insert and on any update of the kind or the column.
ALTER TABLE notifications ADD COLUMN application_id INTEGER REFERENCES applications (id);
CREATE INDEX notifications_application ON notifications (application_id);

CREATE TRIGGER notifications_application_kind
BEFORE INSERT ON notifications
WHEN (NEW.kind IN ('new_application', 'application_decided')) <> (NEW.application_id IS NOT NULL)
BEGIN
	SELECT RAISE(ABORT, 'application_id is required for the application kinds and only for them');
END;

CREATE TRIGGER notifications_application_kind_update
BEFORE UPDATE OF kind, application_id ON notifications
WHEN (NEW.kind IN ('new_application', 'application_decided')) <> (NEW.application_id IS NOT NULL)
BEGIN
	SELECT RAISE(ABORT, 'application_id is required for the application kinds and only for them');
END;

-- Registration defaults to approval (docs/05 "Provider configuration"), so a
-- fresh install needs a registration form from its first start. Version 1 is
-- docs/05's example without the illustrative interests select. An admin edits
-- it by writing version 2 (TKT-01M29CP2P1). The runner checks foreign keys
-- before it commits (migrate.go), which covers the deferred one on forms.
INSERT INTO forms (id, key, title, active_version, created_at, updated_at)
VALUES (1, 'registration', 'Registration', 1, 0, 0);

INSERT INTO form_versions (form_id, version, schema_json, created_by, created_at)
VALUES (1, 1, '{"title":"Apply to join","intro_markdown":"Tell us a bit about yourself…","fields":['
	|| '{"key":"name","type":"text","label":"Name","from_claim":"name","required":true},'
	|| '{"key":"email","type":"email","label":"Email","from_claim":"email","locked":true},'
	|| '{"key":"who","type":"textarea","label":"Who invited you or how did you find us?","required":true,"max":1000},'
	|| '{"key":"rules","type":"checkbox","label":"I have read the guidelines","required":true}'
	|| ']}', NULL, 0);
