-- Per-forum post totals for the forum index (GET /forums, TKT-01M3B6A68B).
--
-- A forum's total counts its posts that are neither deleted nor moved, the
-- rule the profile's post count follows (docs/07, TKT-01M3KJJNDC); a moved
-- post counts in the thread it moved to. threads.post_count cannot be summed
-- for it: that is the latest ordinal, and deleted posts keep theirs.
--
-- Two columns, both kept by triggers like 0007's, so every path that writes
-- post_meta or threads -- the writer, catch-up, reindex's clears -- keeps
-- them right without knowing they exist:
--
-- - threads.live_post_count is the thread's post_meta rows that count, kept
--   by triggers on post_meta. A thread row inserted after its post_meta rows
--   counts them, so the column equals a count of post_meta whatever order
--   rows arrive in. UpsertThread leaves it alone: a replay from the log's
--   start overwrites the thread's post_meta rows rather than removing them,
--   and the post_meta triggers follow each overwrite.
-- - forum_thread_counts.live_posts is the sum of live_post_count over the
--   forum's threads with a post, the same threads its threads column counts,
--   so a forum with no row has no posts either. Not "posts": that word names
--   a per-post table to TestForumIndexQueriesScanNoTable.

ALTER TABLE threads ADD COLUMN
	live_post_count INTEGER NOT NULL DEFAULT 0 CHECK (live_post_count >= 0);

UPDATE threads SET live_post_count = (
	SELECT count(*) FROM post_meta AS p
	WHERE p.thread_id = threads.id AND p.deleted = 0 AND p.moved = 0);

ALTER TABLE forum_thread_counts ADD COLUMN
	live_posts INTEGER NOT NULL DEFAULT 0 CHECK (live_posts >= 0);

UPDATE forum_thread_counts SET live_posts = (
	SELECT coalesce(sum(t.live_post_count), 0) FROM threads AS t
	WHERE t.forum_id = forum_thread_counts.forum_id AND t.last_post_ordinal > 0);

CREATE TRIGGER post_meta_live_insert AFTER INSERT ON post_meta
WHEN NEW.deleted = 0 AND NEW.moved = 0
BEGIN
	UPDATE threads SET live_post_count = live_post_count + 1 WHERE id = NEW.thread_id;
END;

CREATE TRIGGER post_meta_live_delete AFTER DELETE ON post_meta
WHEN OLD.deleted = 0 AND OLD.moved = 0
BEGIN
	UPDATE threads SET live_post_count = live_post_count - 1 WHERE id = OLD.thread_id;
END;

CREATE TRIGGER post_meta_live_update AFTER UPDATE OF thread_id, deleted, moved ON post_meta
WHEN (OLD.deleted = 0 AND OLD.moved = 0) <> (NEW.deleted = 0 AND NEW.moved = 0)
	OR OLD.thread_id <> NEW.thread_id
BEGIN
	UPDATE threads SET live_post_count = live_post_count - 1
		WHERE OLD.deleted = 0 AND OLD.moved = 0 AND id = OLD.thread_id;
	UPDATE threads SET live_post_count = live_post_count + 1
		WHERE NEW.deleted = 0 AND NEW.moved = 0 AND id = NEW.thread_id;
END;

-- 0007's triggers, now moving a thread's whole contribution -- one thread and
-- its live_post_count posts -- and firing when live_post_count changes too.
DROP TRIGGER forum_thread_counts_insert;
DROP TRIGGER forum_thread_counts_delete;
DROP TRIGGER forum_thread_counts_update;

-- One trigger rather than two, because the order matters: the thread's
-- contribution goes in first, and the correction to live_post_count after it
-- then moves through forum_thread_counts_update like any other change.
CREATE TRIGGER forum_thread_counts_insert AFTER INSERT ON threads
BEGIN
	INSERT INTO forum_thread_counts (forum_id, threads, live_posts)
		SELECT NEW.forum_id, 1, NEW.live_post_count WHERE NEW.last_post_ordinal > 0
	ON CONFLICT (forum_id) DO UPDATE SET threads = threads + 1, live_posts = live_posts + excluded.live_posts;
	UPDATE threads SET live_post_count = (
		SELECT count(*) FROM post_meta AS p
		WHERE p.thread_id = NEW.id AND p.deleted = 0 AND p.moved = 0)
	WHERE id = NEW.id;
END;

CREATE TRIGGER forum_thread_counts_delete AFTER DELETE ON threads
WHEN OLD.last_post_ordinal > 0
BEGIN
	UPDATE forum_thread_counts SET threads = threads - 1, live_posts = live_posts - OLD.live_post_count
		WHERE forum_id = OLD.forum_id;
	DELETE FROM forum_thread_counts WHERE forum_id = OLD.forum_id AND threads = 0;
END;

CREATE TRIGGER forum_thread_counts_update AFTER UPDATE OF forum_id, last_post_ordinal, live_post_count ON threads
WHEN (OLD.last_post_ordinal > 0) <> (NEW.last_post_ordinal > 0) OR OLD.forum_id <> NEW.forum_id
	OR OLD.live_post_count <> NEW.live_post_count
BEGIN
	UPDATE forum_thread_counts SET threads = threads - 1, live_posts = live_posts - OLD.live_post_count
		WHERE OLD.last_post_ordinal > 0 AND forum_id = OLD.forum_id;
	DELETE FROM forum_thread_counts WHERE forum_id = OLD.forum_id AND threads = 0;
	INSERT INTO forum_thread_counts (forum_id, threads, live_posts)
		SELECT NEW.forum_id, 1, NEW.live_post_count WHERE NEW.last_post_ordinal > 0
	ON CONFLICT (forum_id) DO UPDATE SET threads = threads + 1, live_posts = live_posts + excluded.live_posts;
END;
