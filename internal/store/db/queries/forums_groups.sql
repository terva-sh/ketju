-- name: CreateForum :one
INSERT INTO forums (parent_id, slug, name, description, position, visibility, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
RETURNING id;

-- name: GetForum :one
SELECT * FROM forums WHERE id = ?;

-- name: GetForumBySlug :one
SELECT * FROM forums WHERE slug = ?;

-- Roots in position order, each followed by its children in position order.
-- A child sorts by its root's position and id, then after the root itself.
-- name: ListForums :many
SELECT f.* FROM forums AS f
LEFT JOIN forums AS root ON root.id = f.parent_id
ORDER BY coalesce(root.position, f.position), coalesce(f.parent_id, f.id),
         f.parent_id IS NOT NULL, f.position, f.id;

-- name: SetForumParent :exec
UPDATE forums SET parent_id = ?, updated_at = ? WHERE id = ?;

-- name: CreateGroup :one
INSERT INTO groups (name, slug, description, created_at, updated_at)
VALUES (?, ?, ?, ?, ?)
RETURNING id;

-- name: GetGroupBySlug :one
SELECT * FROM groups WHERE slug = ?;

-- name: ListGroups :many
SELECT * FROM groups ORDER BY id;

-- An admin adding a membership takes it over: a row an IdP mapping made
-- becomes 'admin', so the next login's re-evaluation no longer removes it.
-- A row that is already 'admin' keeps its original adder and time.
-- name: AddAdminMembership :exec
INSERT INTO group_members (group_id, user_id, source, added_by, added_at)
VALUES (?, ?, 'admin', ?, ?)
ON CONFLICT (group_id, user_id) DO UPDATE
SET source = 'admin', added_by = excluded.added_by, added_at = excluded.added_at
WHERE group_members.source = 'idp';

-- An IdP mapping never overrides a membership an admin set by hand: if the
-- row exists already, whatever its source, it is left alone.
-- name: AddIdPMembership :exec
INSERT INTO group_members (group_id, user_id, source, added_by, added_at)
VALUES (?, ?, 'idp', NULL, ?)
ON CONFLICT (group_id, user_id) DO NOTHING;

-- Login re-evaluation clears the IdP-sourced rows and re-adds what the claims
-- say now, in one transaction. Admin rows are not touched.
-- name: DeleteIdPMemberships :exec
DELETE FROM group_members WHERE user_id = ? AND source = 'idp';

-- name: GroupsForUser :many
SELECT group_id, source FROM group_members WHERE user_id = ? ORDER BY group_id;

-- name: GrantPermission :exec
INSERT INTO permissions (forum_id, group_id, permission, created_at)
VALUES (?, ?, ?, ?);

-- IS rather than =, so a null forum_id matches the global row. Every
-- parameter is named: see TestQueriesDoNotMixParameterStyles.
-- name: RevokePermission :exec
DELETE FROM permissions
WHERE forum_id IS sqlc.narg('forum_id')
  AND group_id = sqlc.arg('group_id')
  AND permission = sqlc.arg('permission');

-- The rows permission evaluation (TKT-01M29C5Y2E) needs for one forum: the
-- forum's own grants and the global ones, for the viewer's groups.
-- name: PermissionRows :many
SELECT forum_id, group_id, permission FROM permissions
WHERE (forum_id = CAST(sqlc.arg('forum_id') AS INTEGER) OR forum_id IS NULL)
  AND group_id IN (sqlc.slice('group_ids'))
ORDER BY forum_id IS NULL, group_id, permission;

-- ForumAccessRows is everything perm needs to evaluate a set of forums for
-- one viewer, in one statement: each forum's visibility, with every
-- permission row, local to it or global, held by a group the viewer is in,
-- and that group's slug. The viewer's groups are the implicit ones, found
-- here by slug (everyone always; members when members_slug is not empty), and
-- their stored memberships; user_id 0 has none. A forum with no such rows
-- still comes back once, with a null group. perm applies the rules to these
-- rows in Go, as it always has.
--
-- The named arguments come before the slice on purpose: sqlc numbers a named
-- argument (?1), and a number after an expanded slice would point at the
-- wrong value.
-- name: ForumAccessRows :many
SELECT f.id AS forum_id, f.visibility, p.forum_id AS grant_forum_id, p.group_id, g.slug AS group_slug,
       p.permission
FROM forums AS f
LEFT JOIN permissions AS p
	ON (p.forum_id = f.id OR p.forum_id IS NULL)
	AND (p.group_id IN (SELECT gm.group_id FROM group_members AS gm WHERE gm.user_id = sqlc.arg('user_id'))
	     OR p.group_id IN (SELECT ig.id FROM groups AS ig
	                       WHERE ig.slug = sqlc.arg('everyone_slug') OR ig.slug = sqlc.arg('members_slug')))
LEFT JOIN groups AS g ON g.id = p.group_id
WHERE f.id IN (sqlc.slice('forum_ids'))
ORDER BY f.id, p.forum_id IS NULL, p.group_id, p.permission;

-- name: RemoveMembership :exec
DELETE FROM group_members WHERE group_id = ? AND user_id = ?;

-- name: SetForumVisibility :exec
UPDATE forums SET visibility = ?, updated_at = ? WHERE id = ?;

-- ForumThreadCounts is each forum's threads with a post and their posts that
-- are neither deleted nor moved (migrations 0007 and 0009, kept by triggers).
-- A forum with no thread that has a post has no row.
-- name: ForumThreadCounts :many
SELECT forum_id, threads, live_posts FROM forum_thread_counts;

-- ForumLastPosts is each listed forum's most recently active thread, for the
-- forum index's last-post column: one seek per forum on
-- threads_forum_activity rather than a scan. A forum with no thread that has
-- a post has no row.
-- name: ForumLastPosts :many
SELECT f.id AS forum_id, t.id AS thread_id, t.slug, t.title, t.last_post_at, t.last_post_author_id
FROM forums AS f
JOIN threads AS t ON t.id = (
	SELECT t2.id FROM threads AS t2 INDEXED BY threads_forum_activity
	WHERE t2.forum_id = f.id AND t2.last_post_ordinal > 0
	ORDER BY t2.last_post_at DESC, t2.id DESC LIMIT 1)
WHERE f.id IN (sqlc.slice('forum_ids'));
