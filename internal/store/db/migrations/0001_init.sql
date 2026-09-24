-- The migration runner's own record. Every later migration is listed here
-- once it has run, with a sha256 of its text so an edit after the fact is
-- refused rather than skipped (internal/store/db/migrate.go).
CREATE TABLE schema_migrations (
	version    INTEGER PRIMARY KEY,
	name       TEXT    NOT NULL,
	checksum   TEXT    NOT NULL,
	created_at INTEGER NOT NULL
);
