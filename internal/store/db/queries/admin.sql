-- The admin users, groups and permissions screens (TKT-01M29CXK1X). Every
-- write here that changes a permission decision is called only from
-- internal/perm (TestMutationsGoThroughPerm); the group writes change no
-- decision, since a group is created empty and deleted only when empty.

-- The active admins, which a demotion must not bring to zero.
-- name: CountActiveAdmins :one
SELECT count(*) FROM users WHERE role = 'admin' AND status = 'active';

-- name: GetMembership :one
SELECT source FROM group_members WHERE group_id = ? AND user_id = ?;

-- name: GetGroup :one
SELECT * FROM groups WHERE id = ?;

-- Every group with its stored members and its permission rows, counted, so
-- the screen can say what stands between a group and its deletion.
-- name: GroupSummaries :many
SELECT g.id, g.name, g.slug, g.description, g.is_system,
       (SELECT count(*) FROM group_members AS m WHERE m.group_id = g.id) AS members,
       (SELECT count(*) FROM permissions AS p WHERE p.group_id = g.id) AS grants
FROM groups AS g
ORDER BY g.id;

-- name: GroupMembers :many
SELECT gm.user_id, gm.source, gm.added_by, gm.added_at
FROM group_members AS gm JOIN users AS u ON u.id = gm.user_id
WHERE gm.group_id = ?
ORDER BY u.handle;

-- name: MembershipsForUser :many
SELECT gm.group_id, g.slug, g.name, gm.source, gm.added_by, gm.added_at
FROM group_members AS gm JOIN groups AS g ON g.id = gm.group_id
WHERE gm.user_id = ?
ORDER BY g.name, g.id;

-- A system group's slug and flag are fixed by trigger; its name and
-- description are not.
-- name: UpdateGroup :execrows
UPDATE groups SET name = ?, description = ?, updated_at = ? WHERE id = ?;

-- Deletes a group only when nothing refers to it: not a system group, no
-- stored member, no permission row. One statement, so a member added
-- between a check and the delete cannot be orphaned (docs/04 "No cascading
-- deletes").
-- name: DeleteEmptyGroup :execrows
DELETE FROM groups
WHERE id = ? AND is_system = 0
  AND NOT EXISTS (SELECT 1 FROM group_members AS m WHERE m.group_id = groups.id)
  AND NOT EXISTS (SELECT 1 FROM permissions AS p WHERE p.group_id = groups.id);

-- Every permission row, for the grid and for checking a batch of changes
-- against what is there.
-- name: ListPermissions :many
SELECT forum_id, group_id, permission FROM permissions
ORDER BY ifnull(forum_id, 0), group_id, permission;

-- The identities that sign a user in, for an admin's view of the account.
-- name: IdentitiesForUser :many
SELECT provider_key, email, email_verified, last_login_at
FROM identities WHERE user_id = ?
ORDER BY id;

-- A user's live member sessions, by the lookup's own cutoffs
-- (session.ExpiredBefore).
-- name: CountUserSessions :one
SELECT count(*) FROM sessions
WHERE user_id = sqlc.arg(user_id) AND kind = 'user'
  AND expires_at > sqlc.arg(expires_after)
  AND last_used_at > sqlc.arg(last_used_after);
