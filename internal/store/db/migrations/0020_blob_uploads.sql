-- The upload path (docs/03 "Attachments", docs/07 "Attachments",
-- TKT-01M29CXK0K): who uploaded each blob and under what name, an image's
-- dimensions, and which posts name each blob.

-- Who has uploaded a blob, and the name they first gave it. Authoritative:
-- an attach is allowed only for a blob the poster uploaded, and a post's
-- attachment shows the name its author uploaded it under. A row is written
-- once and never updated, so a later upload of the same bytes under another
-- name renames nothing already posted (Terva review 1696). A blob deleted
-- by the collector or by ketju redact takes its rows with it.
CREATE TABLE blob_uploads (
	hash          TEXT    NOT NULL REFERENCES blobs (hash) ON DELETE CASCADE,
	user_id       INTEGER NOT NULL REFERENCES users (id),
	original_name TEXT    NOT NULL CHECK (original_name <> ''),
	uploaded_at   INTEGER NOT NULL,
	PRIMARY KEY (hash, user_id)
) WITHOUT ROWID;

-- An image's dimensions, from its DecodeConfig at upload; null for anything
-- that is not an image.
ALTER TABLE blobs ADD COLUMN width INTEGER CHECK (width IS NULL OR width > 0);
ALTER TABLE blobs ADD COLUMN height INTEGER CHECK (height IS NULL OR height > 0);

-- Which posts name a blob in their attachments, for the serve path's access
-- check (docs/07 "Attachments"). A Post record adds its rows and a Redacted
-- record removes them. Nothing else changes a post's attachments: an Edit
-- has no attachments field (docs/03).
CREATE TABLE post_attachments (
	-- PROJECTION: derived from thread logs; rebuild with ketju reindex
	thread_id INTEGER NOT NULL,
	ordinal   INTEGER NOT NULL CHECK (ordinal > 0),
	hash      TEXT    NOT NULL,
	-- Where the hash stands in the Post's list, from zero, so an edited
	-- post, whose current record carries no list, keeps its order.
	position  INTEGER NOT NULL CHECK (position >= 0),
	PRIMARY KEY (thread_id, ordinal, hash)
) WITHOUT ROWID;

-- The serve path asks "which posts name this hash", a page at a time in
-- (thread, ordinal) order.
CREATE INDEX post_attachments_hash ON post_attachments (hash, thread_id, ordinal);
