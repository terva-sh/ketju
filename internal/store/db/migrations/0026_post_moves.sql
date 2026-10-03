-- Moved posts (docs/03 "How a move is written", TKT-01M29CXJZZ): one row per
-- post a PostsSplitOut moved, keyed by where it came from, with where its copy
-- landed once the target's records are projected.
--
-- The source and the target are projected independently and in either order,
-- live or in a reindex, so a row is written from both sides by upserts that
-- each set only their own columns:
--   - the source's PostsSplitOut sets target_thread_id, split_offset and
--     source_revision, the moved post's revision, which a moved post keeps;
--   - the target's PostsMergedIn sets target_thread_id, merged_offset and
--     opens_thread, true when the target held no post yet (a split's new
--     thread);
--   - the target's copied Post, the first Post after that PostsMergedIn
--     whose origin names the row, sets target_ordinal. Any other Post with
--     an origin is no move's copy and touches no row.
-- A row is finished when target_ordinal is set and the copy's
-- post_meta.current_revision has reached source_revision. A null
-- source_revision counts as unfinished.
CREATE TABLE post_moves (
	-- PROJECTION: derived from thread logs; rebuild with ketju reindex
	source_thread_id INTEGER NOT NULL,
	source_ordinal   INTEGER NOT NULL CHECK (source_ordinal > 0),
	source_revision  INTEGER CHECK (source_revision IS NULL OR source_revision >= 1),
	target_thread_id INTEGER NOT NULL,
	target_ordinal   INTEGER CHECK (target_ordinal IS NULL OR target_ordinal > 0),
	split_offset     INTEGER CHECK (split_offset IS NULL OR split_offset >= 0),
	merged_offset    INTEGER CHECK (merged_offset IS NULL OR merged_offset >= 0),
	opens_thread     INTEGER NOT NULL DEFAULT 0 CHECK (opens_thread IN (0, 1)),
	PRIMARY KEY (source_thread_id, source_ordinal)
) WITHOUT ROWID;

-- A copy's row, from the target: whether a Post is a copy merged into its
-- thread, and whether an Edit of a copy is a carried one.
CREATE INDEX post_moves_target ON post_moves (target_thread_id, target_ordinal);
