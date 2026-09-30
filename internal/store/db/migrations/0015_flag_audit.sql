-- The moderator queue's audit link and its recovery marker (docs/04 "flags",
-- docs/08 "Flags", TKT-01M29CRJ4E).
--
-- audit_id names the audit_log row that records a flag's closing. The queue
-- sets it in the transaction that writes that row. Flags closed any other
-- way, and every flag closed before this migration, keep it null, and a
-- null audit_id means nothing on its own.
--
-- audit_pending marks a flag that is owed its row. The queue sets it on a
-- flag it dismisses before a Restore append, in the transaction that
-- dismisses it, because the row may be written only after the append
-- succeeds (Terva review on PR #140, head 1c78b85). The value is the
-- action the moderator asked for, dismiss or resolve ("Keep post"). Writing
-- the row clears it, and so does reopening the flag after a failed append.
-- Recovery, on a retry or in jobs.FlagAuditCatchUp, reads only flags with
-- the marker set (Terva review 1371), so no flag closed by another path is
-- ever given a row it was not owed.
--
-- The CHECK allows the marker only on a dismissed flag that has no row yet.
-- A column CHECK may name other columns of the row, and SQLite tests it
-- against the existing rows, where both columns are null.
ALTER TABLE flags ADD COLUMN audit_id INTEGER REFERENCES audit_log (id);

ALTER TABLE flags ADD COLUMN audit_pending TEXT
	CHECK (audit_pending IS NULL
	       OR (audit_pending IN ('dismiss', 'resolve') AND status = 'dismissed' AND audit_id IS NULL));

-- The catch-up job's scan: few rows, and normally none.
CREATE INDEX flags_audit_pending ON flags (resolved_at) WHERE audit_pending IS NOT NULL;
