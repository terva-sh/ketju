-- Quote backlinks (docs/04 "post_quotes", docs/06 "Post anatomy",
-- TKT-01M3YME9AZ): which earlier posts of its thread each post quotes.
--
-- The thread log carries no quotes field. The projection writer finds them by
-- rendering the current revision's source with no resolver
-- (render.QuotedOrdinals), so the rows depend on the source alone and a
-- reindex rebuilds them exactly. A Post or an Edit replaces the post's rows,
-- and a Redacted post marker removes them. Whether a quoting or quoted post
-- is there and visible is decided when the rows are read, not here.
CREATE TABLE post_quotes (
	-- PROJECTION: derived from thread logs; rebuild with ketju reindex
	thread_id      INTEGER NOT NULL,
	-- The quoting post.
	ordinal        INTEGER NOT NULL CHECK (ordinal > 0),
	-- The post it quotes. Never the quoting post itself.
	quoted_ordinal INTEGER NOT NULL CHECK (quoted_ordinal > 0 AND quoted_ordinal <> ordinal),
	PRIMARY KEY (thread_id, quoted_ordinal, ordinal)
) WITHOUT ROWID;

-- The writer's per-post replace and the notifier's "what does this post
-- quote" both ask by the quoting post.
CREATE INDEX post_quotes_quoting ON post_quotes (thread_id, ordinal);
