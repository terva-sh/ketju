-- Per-thread slow mode (docs/04 "thread_slow_mode", docs/08 "Primitives",
-- TKT-01M29CRJ50). Authoritative: a moderator's live control, kept in the
-- database and not in the thread's log, so ketju reindex neither clears nor
-- rebuilds it. A thread id is a plain INTEGER with no foreign key, as in 0005
-- and 0006: threads is a projection, and a key into it would let a reindex's
-- ClearThreads reach this table.
--
-- A row overrides slow_mode.default_seconds, an explicit 0 included, which
-- turns slow mode off in a thread under a non-zero default. No row means the
-- default. The upper bound of a day guards against a typo; it is not a
-- policy.
CREATE TABLE thread_slow_mode (
	thread_id INTEGER PRIMARY KEY,
	seconds   INTEGER NOT NULL CHECK (seconds BETWEEN 0 AND 86400),
	set_by    INTEGER REFERENCES users (id),
	set_at    INTEGER NOT NULL
);
