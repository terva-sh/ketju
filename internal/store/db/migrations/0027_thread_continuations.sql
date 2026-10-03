-- Reply as a new thread (docs/03 "ThreadCreated.continues", docs/06 "Reply as
-- a new thread", TKT-01M29CYSC0): which post each thread started that way
-- continues. One row per such thread, from its ThreadCreated's continues key,
-- so the source's log is never written and a reindex rebuilds the source
-- post's "continued in" note from the new thread's log alone.
--
-- No backfill: no log written before this migration carries the key.
CREATE TABLE thread_continuations (
	-- PROJECTION: derived from thread logs; rebuild with ketju reindex
	thread_id        INTEGER NOT NULL PRIMARY KEY,
	source_thread_id INTEGER NOT NULL CHECK (source_thread_id <> thread_id),
	source_ordinal   INTEGER NOT NULL CHECK (source_ordinal > 0)
) WITHOUT ROWID;

-- The source's page asks which threads continue its posts.
CREATE INDEX thread_continuations_source ON thread_continuations (source_thread_id, source_ordinal);
