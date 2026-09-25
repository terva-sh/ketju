-- Indexes for the forum thread list (GET /forums/{slug}/threads,
-- TKT-01M29CA9AG), so neither of its orders nor its pins reads the whole
-- threads table.
--
-- threads_forum_activity (0004) already serves the default order,
-- last_post_at within a forum; this is ?sort=created's, for announcement
-- forums (docs/06). Both end in the rowid, which is the id the keyset cursor
-- breaks ties on.
CREATE INDEX threads_forum_created ON threads (forum_id, created_at);

-- Pinned threads are a handful on a site of any size. The list's first page
-- asks for all of them, global ones from every forum, so the index holds
-- only pinned rows.
CREATE INDEX threads_pinned ON threads (pinned_scope, forum_id) WHERE pinned_scope IS NOT NULL;
