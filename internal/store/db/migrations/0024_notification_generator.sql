-- The notification generator (docs/04 "notifications", TKT-01M29CRJ2T): how
-- far into each thread log it has generated, and the indexes its writes and
-- the notification list need.
--
-- notification_progress is AUTHORITATIVE, not a projection: `ketju reindex`
-- leaves it alone, as it leaves notifications alone, because what a member
-- has already been alerted to cannot be rebuilt from the logs. It carries no
-- PROJECTION marker for that reason.
CREATE TABLE notification_progress (
	thread_id  INTEGER PRIMARY KEY,
	-- The log length the generator has processed up to: a frame boundary.
	log_length INTEGER NOT NULL CHECK (log_length >= 0),
	updated_at INTEGER NOT NULL
);

-- The seed only saves the first catch-up a scan. It does not keep history
-- quiet: the projection may be behind as this runs, so a range past it would
-- still be processed. The install cutoff does that instead. The generator
-- reads this migration's schema_migrations.created_at, found by its name, and
-- generates nothing for a record written at or before it (review 1693).
INSERT INTO notification_progress (thread_id, log_length, updated_at)
SELECT thread_id, log_length, updated_at FROM projection_state;

-- One notification per post per member. The generator upserts on it and
-- keeps the stronger kind. No code wrote a post kind before this migration,
-- so nothing here can collide.
CREATE UNIQUE INDEX notifications_post ON notifications (user_id, thread_id, ordinal)
	WHERE thread_id IS NOT NULL;

-- At most one unread thread_you_watch per member per thread: later posts add
-- nothing until it is read (docs/04, run decision 2026-10-01).
CREATE UNIQUE INDEX notifications_watch_unread ON notifications (user_id, thread_id)
	WHERE kind = 'thread_you_watch' AND read_at IS NULL;

-- The notification list: a member's rows, newest written first. The rowid
-- ends every index entry, so this serves the keyset on id.
CREATE INDEX notifications_recent ON notifications (user_id);

-- A post's watchers, found from the thread and from its forum without a scan.
-- watches_thread and watches_forum lead with user_id, which serves the
-- viewer's own rows and not these.
CREATE INDEX watches_by_thread ON watches (thread_id) WHERE thread_id IS NOT NULL;
CREATE INDEX watches_by_forum ON watches (forum_id) WHERE forum_id IS NOT NULL;
