-- The search index (docs/04 "search (FTS)", TKT-01M29CRJ3B).
--
-- Every row here is derived from the thread logs, like a projection, but the
-- tables are not projections: they are db.SearchTables, apart from
-- db.ProjectionTables, so `ketju reindex --projections` leaves them alone and
-- `ketju reindex --search` rebuilds them. The marker comment inside each
-- CREATE TABLE is what sqlite_schema keeps, and TestSearchTablesAreMarked
-- holds the marked set equal to db.SearchTables.
--
-- The index holds only what every reader of a thread may see: one row per
-- visible post, at its current revision, and the thread's title as a row of
-- its own at ordinal 0. A post that stops being visible loses its row.
--
-- As in 0004, no table has a foreign key: the index is rebuilt from the logs
-- alone, into a database whose users and forums may be older than the logs.

CREATE TABLE search_docs (
	-- SEARCH INDEX: derived from thread logs; rebuild with ketju reindex --search
	--
	-- The rowid of the FTS5 table below. search_docs is that table's
	-- external content, so the text is stored once, here.
	id           INTEGER PRIMARY KEY,
	thread_id    INTEGER NOT NULL,
	-- 0 is the thread's title; a post is its one-based ordinal.
	ordinal      INTEGER NOT NULL CHECK (ordinal >= 0),
	-- The post's current revision; 0 for the title row.
	revision     INTEGER NOT NULL,
	-- The post's author and revision 1's time, from its index entry, for
	-- the author and date filters. The title row carries the thread's
	-- creator and creation time.
	author_id    INTEGER NOT NULL,
	created_at   INTEGER NOT NULL,
	-- Set on the title row only.
	title        TEXT    NOT NULL DEFAULT '',
	-- Set on a post's row only: render.Plain of the current revision.
	source_plain TEXT    NOT NULL DEFAULT '',
	UNIQUE (thread_id, ordinal)
);

-- No triggers keep this in step with search_docs. db.PutSearchDoc and
-- db.DropSearchDoc are its only writers, and each issues FTS5's 'delete'
-- command with a row's old values before changing it. A trigger would make
-- db.ClearSearch unsafe: FTS5's 'delete-all', the one clear that works on a
-- damaged index, followed by trigger-issued deletes of rows the index no
-- longer holds, is undefined behaviour in FTS5's documentation.
--
-- No rank is configured and nothing orders by one: bm25's statistics span
-- the whole index, so posts in forums a viewer cannot read would reorder the
-- ones they can (docs/07 "Search").
CREATE VIRTUAL TABLE search USING fts5(
	title, source_plain,
	content = 'search_docs', content_rowid = 'id',
	tokenize = 'unicode61 remove_diacritics 2'
);

CREATE TABLE search_state (
	-- SEARCH INDEX: derived from thread logs; rebuild with ketju reindex --search
	--
	-- How far each thread's log has been indexed: the end of the last frame
	-- scanned, never past it. Catch-up resumes here.
	thread_id  INTEGER PRIMARY KEY,
	log_length INTEGER NOT NULL,
	updated_at INTEGER NOT NULL
);

CREATE TABLE search_meta (
	-- SEARCH INDEX: derived from thread logs; rebuild with ketju reindex --search
	--
	-- One row: the version of the plain-text extraction and tokenizer that
	-- built the index. A build that finds another version, or no row, empties
	-- the index and records its own in one transaction.
	id      INTEGER PRIMARY KEY CHECK (id = 1),
	version INTEGER NOT NULL
);
