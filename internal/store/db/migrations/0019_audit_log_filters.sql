-- The audit log's filters (docs/04 "audit_log", docs/07 "The audit log",
-- TKT-01M29CP2R4). The admin view pages newest first by id and filters by
-- actor, action, target and time. audit_log_target and audit_log_at (0006)
-- already serve the target and time filters; these two serve the other two,
-- each ending in id so a page seeks to its cursor and reads in id order
-- without sorting.
--
-- actor_id is NULL for work no account did, so the "system" filter,
-- actor_id IS NULL, is a search on the same index.
CREATE INDEX audit_log_actor ON audit_log (actor_id, id);
CREATE INDEX audit_log_action ON audit_log (action, id);
