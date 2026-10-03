-- Backfill without forgetting the projected length (TKT-01M41MTXWH, Terva
-- review 2074).
--
-- 0025 backfilled post_quotes by setting every projection_state.log_length
-- back to 64, the end of the file header, so the next start's catch-up
-- replayed each log. That also threw away the length catch-up compares a
-- log against (docs/03 "Durability and recovery"). A log cut short before
-- the upgrade then classified as drift and was replayed as the truth, or,
-- cut to its header, as current, instead of being quarantined as corrupt.
--
-- 0025 stays as it is: it has run on databases since it merged, and the
-- runner refuses a migration whose text changed after it ran (migrate.go).
-- This migration undoes its rewind and asks for the replay another way.
--
-- backfill = 1 asks the next projection of the thread to replay its log
-- from the file header over the rows already there, up to at least
-- log_length, and then clears itself in the same transaction. log_length
-- keeps the real projected length, so catch-up classifies the thread first
-- and quarantines a log shorter than it, as on any other start. A later
-- migration that adds a projection backfills it the same way, and never by
-- moving log_length.
ALTER TABLE projection_state ADD COLUMN backfill INTEGER NOT NULL DEFAULT 0 CHECK (backfill IN (0, 1));

-- A row at 64 is one 0025 moved back that no catch-up has replayed since:
-- the writer records a length only after a log's first frame, so no other
-- row holds 64. Its real length is still in threads.log_length_projected,
-- which the writer sets in the same transaction and 0025 left alone. A row
-- a catch-up already replayed holds the length of the log it replayed, and
-- has its backlinks; it is left as it is, because nothing records what
-- length it held before. A row at 64 whose threads row is missing or no
-- longer than its header keeps 64, and is replayed as 0025 intended.
UPDATE projection_state SET
	backfill = 1,
	log_length = coalesce((SELECT t.log_length_projected FROM threads AS t
	                       WHERE t.id = projection_state.thread_id AND t.log_length_projected > 64), 64)
WHERE log_length = 64;
