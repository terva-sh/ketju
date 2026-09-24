-- Projections: what the thread logs say, in a form the database can query
-- across threads (docs/04 "Projection tables", docs/03 "Write path").
--
-- Every table here is a PROJECTION. Every row is derived from a thread log
-- and every one is disposable: `ketju reindex` empties these tables and
-- replays the logs into them. The marker comment inside each CREATE is what
-- sqlite_schema keeps, and TestProjectionTablesAreMarked holds the marked set
-- equal to db.ProjectionTables, which is the list reindex empties.
--
-- No table here has a foreign key, to users or to forums. Reindex must be
-- able to rebuild from the logs alone, into a database whose users came from
-- an older backup or from none (settled in grooming, TKT-01M29BYYJ8). A
-- projection row naming a user or forum the database does not have is a
-- reader's problem to render, not a write to refuse.
--
-- Thread ids are threadid.ID, a uint64 with the millisecond in the high 48
-- bits. They are positive as a signed INTEGER until the year 6429.

CREATE TABLE threads (
	-- PROJECTION: derived from thread logs; rebuild with ketju reindex
	id                   INTEGER PRIMARY KEY,
	-- The current forum: ThreadCreated, then the latest ThreadMoved. The log
	-- file header's forum id is where the thread began and is not this.
	forum_id             INTEGER NOT NULL,
	slug                 TEXT    NOT NULL,
	title                TEXT    NOT NULL,
	author_id            INTEGER NOT NULL,
	created_at           INTEGER NOT NULL,
	post_count           INTEGER NOT NULL DEFAULT 0 CHECK (post_count >= 0),
	last_post_at         INTEGER NOT NULL,
	last_post_author_id  INTEGER NOT NULL,
	last_post_ordinal    INTEGER NOT NULL DEFAULT 0 CHECK (last_post_ordinal >= 0),
	locked               INTEGER NOT NULL DEFAULT 0 CHECK (locked IN (0, 1)),
	-- Both null when the thread is not pinned; until is null for a pin with
	-- no expiry.
	pinned_scope         TEXT CHECK (pinned_scope IN ('forum', 'global')),
	pinned_until         INTEGER CHECK (pinned_until IS NULL OR pinned_scope IS NOT NULL),
	archived             INTEGER NOT NULL DEFAULT 0 CHECK (archived IN (0, 1)),
	-- How far into the log this row reflects. projection_state.log_length is
	-- the same number; the writer sets both in one transaction, and startup
	-- catch-up reads projection_state, which also covers a thread whose
	-- ThreadCreated has not been projected yet.
	log_length_projected INTEGER NOT NULL CHECK (log_length_projected >= 0)
);

-- The forum thread list: a forum's threads by latest activity (docs/06).
CREATE INDEX threads_forum_activity ON threads (forum_id, last_post_at);

CREATE TABLE thread_tags (
	-- PROJECTION: derived from thread logs; rebuild with ketju reindex
	thread_id INTEGER NOT NULL,
	tag       TEXT    NOT NULL,
	PRIMARY KEY (thread_id, tag)
) WITHOUT ROWID;

-- Tiny on purpose. post_meta exists for questions that cross threads: a
-- user's post history, reverse reply lookups when the thread is not open,
-- moderation lists. Within-thread questions belong to the thread's .idx file,
-- and a column or index here that only a within-thread question needs is the
-- mistake to avoid. TestPostMetaStaysTiny pins the columns and indexes.
CREATE TABLE post_meta (
	-- PROJECTION: derived from thread logs; rebuild with ketju reindex
	thread_id        INTEGER NOT NULL,
	ordinal          INTEGER NOT NULL CHECK (ordinal > 0),
	author_id        INTEGER NOT NULL,
	-- Of revision 1, as the index keeps it, so history sorts by when a post
	-- was written rather than when it was last edited.
	created_at       INTEGER NOT NULL,
	current_revision INTEGER NOT NULL DEFAULT 1 CHECK (current_revision >= 1),
	edited_at        INTEGER,
	-- Null for a post that replies to nothing.
	reply_to         INTEGER CHECK (reply_to IS NULL OR reply_to > 0),
	deleted          INTEGER NOT NULL DEFAULT 0 CHECK (deleted IN (0, 1)),
	moved            INTEGER NOT NULL DEFAULT 0 CHECK (moved IN (0, 1)),
	-- Of the record last projected into this row. The writer is idempotent
	-- on (thread_id, log_offset) (docs/03), so a replayed record is a no-op.
	log_offset       INTEGER NOT NULL CHECK (log_offset >= 0),
	PRIMARY KEY (thread_id, ordinal)
) WITHOUT ROWID;

CREATE INDEX post_meta_author ON post_meta (author_id, created_at);
CREATE INDEX post_meta_reply_to ON post_meta (thread_id, reply_to);

CREATE TABLE mentions (
	-- PROJECTION: derived from thread logs; rebuild with ketju reindex
	thread_id         INTEGER NOT NULL,
	ordinal           INTEGER NOT NULL CHECK (ordinal > 0),
	mentioned_user_id INTEGER NOT NULL,
	PRIMARY KEY (thread_id, ordinal, mentioned_user_id)
) WITHOUT ROWID;

CREATE TABLE projection_state (
	-- PROJECTION: derived from thread logs; rebuild with ketju reindex
	thread_id  INTEGER PRIMARY KEY,
	-- The log length this thread has been projected up to. Startup catch-up
	-- replays from here to the log's real length (docs/03).
	log_length INTEGER NOT NULL CHECK (log_length >= 0),
	updated_at INTEGER NOT NULL
);
