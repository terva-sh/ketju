-- Forums, groups, and forum-level permissions (docs/04, docs/05). All
-- authoritative. Deliberately phpBB-shaped and small.

-- Forums nest at most one level: a forum may have a parent, and a forum with
-- a parent may not have children. The triggers below enforce it on every
-- write path, not just the admin screen.
CREATE TABLE forums (
	id          INTEGER PRIMARY KEY,
	parent_id   INTEGER REFERENCES forums (id),
	slug        TEXT    NOT NULL UNIQUE,
	name        TEXT    NOT NULL,
	description TEXT    NOT NULL DEFAULT '',
	position    INTEGER NOT NULL DEFAULT 0,
	visibility  TEXT    NOT NULL DEFAULT 'members'
		CHECK (visibility IN ('public', 'members', 'restricted')),
	archived    INTEGER NOT NULL DEFAULT 0 CHECK (archived IN (0, 1)),
	created_at  INTEGER NOT NULL,
	updated_at  INTEGER NOT NULL,
	CHECK (parent_id IS NULL OR parent_id <> id)
);
CREATE INDEX forums_parent ON forums (parent_id, position);

CREATE TRIGGER forums_depth_on_insert
BEFORE INSERT ON forums
WHEN NEW.parent_id IS NOT NULL
 AND (SELECT parent_id FROM forums WHERE id = NEW.parent_id) IS NOT NULL
BEGIN
	SELECT RAISE(ABORT, 'forums nest one level deep: the parent already has a parent');
END;

CREATE TRIGGER forums_depth_on_update
BEFORE UPDATE OF parent_id ON forums
WHEN NEW.parent_id IS NOT NULL
BEGIN
	SELECT RAISE(ABORT, 'a forum cannot be its own parent')
	WHERE NEW.parent_id = NEW.id;
	SELECT RAISE(ABORT, 'forums nest one level deep: the parent already has a parent')
	WHERE (SELECT parent_id FROM forums WHERE id = NEW.parent_id) IS NOT NULL;
	SELECT RAISE(ABORT, 'forums nest one level deep: this forum has children')
	WHERE EXISTS (SELECT 1 FROM forums WHERE parent_id = NEW.id);
END;

CREATE TABLE groups (
	id          INTEGER PRIMARY KEY,
	name        TEXT    NOT NULL,
	slug        TEXT    NOT NULL UNIQUE,
	description TEXT    NOT NULL DEFAULT '',
	is_system   INTEGER NOT NULL DEFAULT 0 CHECK (is_system IN (0, 1)),
	created_at  INTEGER NOT NULL,
	updated_at  INTEGER NOT NULL
);

-- The four system groups, with fixed ids. Go finds them by slug
-- (domain.GroupEveryone and the rest). Seeded before the triggers that
-- refuse any other system row.
INSERT INTO groups (id, name, slug, description, is_system, created_at, updated_at) VALUES
	(1, 'Everyone',   'everyone',   'Every viewer, signed in or not. Implicit.', 1, 0, 0),
	(2, 'Members',    'members',    'Every active user. Implicit.',              1, 0, 0),
	(3, 'Moderators', 'moderators', 'Moderate the forums they are granted.',     1, 0, 0),
	(4, 'Admins',     'admins',     'Administer the forum.',                     1, 0, 0);

-- Exactly those four are system groups, for good: none is deleted, none loses
-- its flag, slug or id, and no other group gains the flag.
CREATE TRIGGER groups_system_no_delete
BEFORE DELETE ON groups
WHEN OLD.is_system = 1
BEGIN
	SELECT RAISE(ABORT, 'system groups cannot be deleted');
END;

CREATE TRIGGER groups_system_no_change
BEFORE UPDATE OF id, is_system, slug ON groups
WHEN NEW.is_system IS NOT OLD.is_system
  OR (OLD.is_system = 1 AND (NEW.slug IS NOT OLD.slug OR NEW.id IS NOT OLD.id))
BEGIN
	SELECT RAISE(ABORT, 'system groups keep their id, slug and flag, and no group becomes one');
END;

CREATE TRIGGER groups_system_no_insert
BEFORE INSERT ON groups
WHEN NEW.is_system = 1
BEGIN
	SELECT RAISE(ABORT, 'the four system groups are seeded by migration only');
END;

-- source says where a membership came from: an admin set it by hand, or it
-- was mapped from an IdP group claim at login (docs/05). Re-evaluation at
-- login replaces only 'idp' rows, so an admin's choice is never clobbered.
-- An IdP membership has no human who added it.
CREATE TABLE group_members (
	group_id INTEGER NOT NULL REFERENCES groups (id),
	user_id  INTEGER NOT NULL REFERENCES users (id),
	source   TEXT    NOT NULL CHECK (source IN ('admin', 'idp')),
	added_by INTEGER REFERENCES users (id),
	added_at INTEGER NOT NULL,
	PRIMARY KEY (group_id, user_id),
	CHECK (source = 'admin' OR added_by IS NULL)
) WITHOUT ROWID;
CREATE INDEX group_members_user ON group_members (user_id);

-- everyone and members are implicit (docs/04), so a row for either would be
-- a second, contradictory source of truth.
CREATE TRIGGER group_members_not_implicit
BEFORE INSERT ON group_members
WHEN NEW.group_id IN (SELECT id FROM groups WHERE slug IN ('everyone', 'members'))
BEGIN
	SELECT RAISE(ABORT, 'membership of everyone and members is implicit, never stored');
END;

CREATE TRIGGER group_members_not_implicit_on_update
BEFORE UPDATE OF group_id ON group_members
WHEN NEW.group_id IN (SELECT id FROM groups WHERE slug IN ('everyone', 'members'))
BEGIN
	SELECT RAISE(ABORT, 'membership of everyone and members is implicit, never stored');
END;

-- A null forum_id is a global grant. Evaluation is the union of the viewer's
-- groups' grants for the forum, falling back to global rows (docs/04).
-- A plain UNIQUE would treat every NULL forum_id as distinct, so uniqueness
-- is an expression index; forum ids start at 1, so 0 cannot collide.
CREATE TABLE permissions (
	forum_id   INTEGER REFERENCES forums (id),
	group_id   INTEGER NOT NULL REFERENCES groups (id),
	permission TEXT    NOT NULL
		CHECK (permission IN ('read', 'create_thread', 'reply', 'upload', 'moderate')),
	created_at INTEGER NOT NULL
);
CREATE UNIQUE INDEX permissions_unique ON permissions (ifnull(forum_id, 0), group_id, permission);
CREATE INDEX permissions_group ON permissions (group_id);

-- A fresh install is usable at once: members read, start threads and reply
-- everywhere, and everyone reads, which applies only on public forums
-- (docs/04, decided 2026-09-24).
INSERT INTO permissions (forum_id, group_id, permission, created_at) VALUES
	(NULL, 2, 'read',          0),
	(NULL, 2, 'create_thread', 0),
	(NULL, 2, 'reply',         0),
	(NULL, 1, 'read',          0);
