-- Per-forum counts of threads with a post, and an index for counting a
-- forum's threads active since a moment, both for GET /me's unread badges
-- (TKT-01M29CA99R).
--
-- /me runs on every page load, because the shell inlines it. A reader who has
-- never used "mark all read" has every thread they never opened unread, so
-- their count is the number of threads with a post, per forum; without this
-- table that is a scan of threads on every page. With a mark set, the count
-- is of threads active since the mark, which the index below answers by a
-- seek per forum without reading the table, so the work follows activity
-- since the mark rather than the size of the site.

-- The table is maintained by triggers on threads rather than by the
-- projection writer, so every path that changes threads -- the writer,
-- catch-up, reindex's ClearThreads -- keeps it right without knowing it
-- exists. A forum with no such threads has no row rather than a zero, so the
-- table is the same whichever order the threads arrived in, which is what
-- lets the replay tests compare it.
CREATE TABLE forum_thread_counts (
	-- PROJECTION: derived from thread logs; rebuild with ketju reindex
	forum_id INTEGER PRIMARY KEY,
	-- Zero only for the moment between a trigger's decrement and its delete.
	threads  INTEGER NOT NULL CHECK (threads >= 0)
);

INSERT INTO forum_thread_counts (forum_id, threads)
SELECT forum_id, count(*) FROM threads WHERE last_post_ordinal > 0 GROUP BY forum_id;

CREATE TRIGGER forum_thread_counts_insert AFTER INSERT ON threads
WHEN NEW.last_post_ordinal > 0
BEGIN
	INSERT INTO forum_thread_counts (forum_id, threads) VALUES (NEW.forum_id, 1)
	ON CONFLICT (forum_id) DO UPDATE SET threads = threads + 1;
END;

CREATE TRIGGER forum_thread_counts_delete AFTER DELETE ON threads
WHEN OLD.last_post_ordinal > 0
BEGIN
	UPDATE forum_thread_counts SET threads = threads - 1 WHERE forum_id = OLD.forum_id;
	DELETE FROM forum_thread_counts WHERE forum_id = OLD.forum_id AND threads = 0;
END;

CREATE TRIGGER forum_thread_counts_update AFTER UPDATE OF forum_id, last_post_ordinal ON threads
WHEN (OLD.last_post_ordinal > 0) <> (NEW.last_post_ordinal > 0) OR OLD.forum_id <> NEW.forum_id
BEGIN
	UPDATE forum_thread_counts SET threads = threads - 1
		WHERE OLD.last_post_ordinal > 0 AND forum_id = OLD.forum_id;
	DELETE FROM forum_thread_counts WHERE forum_id = OLD.forum_id AND threads = 0;
	INSERT INTO forum_thread_counts (forum_id, threads)
		SELECT NEW.forum_id, 1 WHERE NEW.last_post_ordinal > 0
	ON CONFLICT (forum_id) DO UPDATE SET threads = threads + 1;
END;

-- A forum's threads with a post after a moment, answered from the index.
CREATE INDEX threads_forum_active ON threads (forum_id, last_post_at, last_post_ordinal);
