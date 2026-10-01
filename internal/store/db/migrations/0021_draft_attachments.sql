-- A draft's attachments (docs/04 "drafts", docs/07 "Drafts",
-- TKT-01M3V73DP6). Decided by the run on 2026-10-01, pending maintainer
-- review: both kinds of draft keep the composer's attachments, so a draft
-- restored on another device comes back whole (maintainer decision of
-- 2026-09-28, TKT-01M3K9XBJA).
--
-- attachments: a JSON array of the blob hashes the draft names, as the
-- attach check answers them: each once, in the order sent, at most ten.
-- Saving a draft adds no reference to a blob. Blob GC and ketju redact keep
-- a blob any draft's attachments name instead (DeleteCollectableBlob).
-- drafts is authoritative, and the default reads every draft stored before
-- this migration as one with no attachments.
ALTER TABLE drafts ADD COLUMN attachments TEXT NOT NULL DEFAULT '[]'
	CHECK (json_valid(attachments) AND json_type(attachments) = 'array');

-- A tombstone holds no attachments, as it holds no source (0018). SQLite
-- cannot add that to the deleted column's CHECK without rebuilding the
-- table, so these triggers hold it on every write instead.
CREATE TRIGGER drafts_tombstone_no_attachments_insert
BEFORE INSERT ON drafts
WHEN NEW.deleted = 1 AND json_array_length(NEW.attachments) > 0
BEGIN
	SELECT RAISE(ABORT, 'a deleted draft holds no attachments');
END;

CREATE TRIGGER drafts_tombstone_no_attachments_update
BEFORE UPDATE OF deleted, attachments ON drafts
WHEN NEW.deleted = 1 AND json_array_length(NEW.attachments) > 0
BEGIN
	SELECT RAISE(ABORT, 'a deleted draft holds no attachments');
END;
