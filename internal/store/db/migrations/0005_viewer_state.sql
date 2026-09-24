-- Viewer state: what each user has read, watches, is notified of, and is
-- drafting (docs/04 "read_positions", "watches", "notifications", "drafts").
--
-- Every table here is AUTHORITATIVE: the only copy, so it must be backed up.
-- Foreign keys point at users and forums, which are authoritative too. A
-- thread id is a plain INTEGER with no foreign key, because threads is a
-- projection that `ketju reindex` empties and refills (migration 0004).

-- One row per (user, thread) the user has ever opened, holding one integer:
-- how far they have read. Unread counts come from that and the thread's posts
-- (UnreadCount), and users.mark_all_read_at covers every thread the user never
-- opened, so those need no row at all. That is what keeps read tracking cheap
-- at 1 000 users and 10 000 threads (docs/04 "Sizing sanity check"). A table
-- with a row per read post is the mistake this design exists to avoid.
CREATE TABLE read_positions (
	user_id           INTEGER NOT NULL REFERENCES users (id),
	thread_id         INTEGER NOT NULL,
	last_read_ordinal INTEGER NOT NULL CHECK (last_read_ordinal >= 0),
	last_read_at      INTEGER NOT NULL,
	PRIMARY KEY (user_id, thread_id)
) WITHOUT ROWID;

-- A watch or a mute, on a thread or on a forum: exactly one of the two.
CREATE TABLE watches (
	id         INTEGER PRIMARY KEY,
	user_id    INTEGER NOT NULL REFERENCES users (id),
	thread_id  INTEGER,
	forum_id   INTEGER REFERENCES forums (id),
	level      TEXT    NOT NULL CHECK (level IN ('watch', 'mute')),
	created_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL,
	CHECK ((thread_id IS NULL) <> (forum_id IS NULL))
);

CREATE UNIQUE INDEX watches_thread ON watches (user_id, thread_id) WHERE thread_id IS NOT NULL;
CREATE UNIQUE INDEX watches_forum ON watches (user_id, forum_id) WHERE forum_id IS NOT NULL;

-- The kinds are the six in docs/04; domain.NotificationKinds is the same list
-- in Go, and TestNotificationKindsClosed holds the two together. Thread and
-- ordinal are null for the application kinds, which concern no post.
CREATE TABLE notifications (
	id        INTEGER PRIMARY KEY,
	user_id   INTEGER NOT NULL REFERENCES users (id),
	kind      TEXT    NOT NULL CHECK (kind IN ('reply_to_you', 'mention', 'quoted',
	                                           'thread_you_watch', 'application_decided',
	                                           'new_application')),
	thread_id INTEGER,
	ordinal   INTEGER CHECK (ordinal IS NULL OR ordinal > 0),
	actor_id  INTEGER REFERENCES users (id),
	at        INTEGER NOT NULL,
	read_at   INTEGER
);

-- A user's notifications, unread first (read_at IS NULL sorts first), newest
-- within each.
CREATE INDEX notifications_user ON notifications (user_id, read_at, at);

-- Composer autosave. A draft belongs to a thread (a reply) or, with no thread
-- yet, to the forum it will be posted in (a new thread): exactly one of the
-- two. docs/04 wrote PRIMARY KEY (user_id, thread_id), but a nullable column
-- in a key does not make it unique in SQLite, which lets NULLs repeat, and it
-- could not key a new-thread draft by forum. The two partial unique indexes
-- below say what was meant: one draft per user per thread, and one new-thread
-- draft per user per forum (settled in grooming, TKT-01M29BYYHQ).
CREATE TABLE drafts (
	id         INTEGER PRIMARY KEY,
	user_id    INTEGER NOT NULL REFERENCES users (id),
	thread_id  INTEGER,
	forum_id   INTEGER REFERENCES forums (id),
	source     TEXT    NOT NULL,
	reply_to   INTEGER CHECK (reply_to IS NULL OR reply_to > 0),
	created_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL,
	CHECK ((thread_id IS NULL) <> (forum_id IS NULL)),
	-- A reply target only makes sense inside a thread.
	CHECK (reply_to IS NULL OR thread_id IS NOT NULL)
);

CREATE UNIQUE INDEX drafts_thread ON drafts (user_id, thread_id) WHERE thread_id IS NOT NULL;
CREATE UNIQUE INDEX drafts_new_thread ON drafts (user_id, forum_id) WHERE thread_id IS NULL;
