-- A new-thread draft's title, and every draft's revision (docs/04 "drafts",
-- docs/07 "Drafts", TKT-01M3K9XBJA).
--
-- title: a new-thread draft saves its title as well as its body, so a draft
-- restored on another device comes back whole (maintainer decision of
-- 2026-09-28). A reply draft has no title of its own, and the CHECK keeps it
-- null there. It is stored as typed, untrimmed, because it is unfinished.
--
-- revision: a count per draft key, (user, thread) or (user, forum), that
-- only grows. Every save adds one, and so does deleting a live draft. A
-- client orders its local copy of a draft against the server's by it, never
-- by comparing its clock with updated_at (TKT-01M3NS1931), so no clock
-- enters it: every revision is computed in SQL from the stored value, inside
-- the write's own transaction.
--
-- deleted: a draft that is deleted is kept as a tombstone, with its source
-- emptied and its reply target and title cleared, so that its key's
-- revision survives. A draft deleted and started again then answers a
-- higher revision than any copy of the old one. Tombstones are never swept,
-- because a sweep would restart the count. There is at most one per thread
-- or forum a member has ever drafted in, the partial unique indexes
-- drafts_thread and drafts_new_thread hold one row per key whether it is a
-- tombstone or not, and blob GC finds no attachment in an empty source.
ALTER TABLE drafts ADD COLUMN title TEXT CHECK (title IS NULL OR thread_id IS NULL);
ALTER TABLE drafts ADD COLUMN revision INTEGER NOT NULL DEFAULT 0 CHECK (revision >= 0);
ALTER TABLE drafts ADD COLUMN deleted INTEGER NOT NULL DEFAULT 0
	CHECK (deleted = 0 OR (deleted = 1 AND source = '' AND reply_to IS NULL AND title IS NULL));

-- A draft already stored was saved at least once, and a save answers
-- revision 1 or more, so 0 is left to mean a key that has never been saved.
UPDATE drafts SET revision = 1;
