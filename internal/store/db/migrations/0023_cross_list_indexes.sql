-- Indexes for the cross-forum lists (GET /unread, /new, /participated and
-- /watching) and their /me badges (TKT-01M29CTY7K).
--
-- Unread and New page every forum the viewer may read at once, newest
-- first. The per-forum indexes (threads_forum_activity, threads_forum_created)
-- cannot give that order across forums without sorting every qualifying
-- thread, and for a member who has never used "mark all read" nearly every
-- thread qualifies. These walk the whole site in the list's order instead,
-- from the viewer's mark up, and the query stops once it has a page. Both
-- end in the rowid, which is the id the keyset cursor breaks ties on.
CREATE INDEX threads_activity ON threads (last_post_at);
CREATE INDEX threads_created ON threads (created_at);

-- /me's new count for a reader who has used "mark all read": per forum, the
-- threads with a post started after the mark. It is threads_forum_created
-- with last_post_ordinal added, so the count reads only index entries, as
-- threads_forum_active does for the unread count (0007). It is a separate
-- index because the extra column would put last_post_ordinal between
-- created_at and the rowid, and the ?sort=created list's keyset needs those
-- two adjacent.
CREATE INDEX threads_forum_fresh ON threads (forum_id, created_at, last_post_ordinal);
