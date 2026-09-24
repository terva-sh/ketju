-- name: CreateForum :one
INSERT INTO forums (parent_id, slug, name, description, position, visibility, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
RETURNING id;

-- name: GetForum :one
SELECT * FROM forums WHERE id = ?;

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
