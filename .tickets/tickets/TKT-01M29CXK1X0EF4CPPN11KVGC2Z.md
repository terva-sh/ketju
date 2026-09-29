---
schema: 3
id: TKT-01M29CXK1X0EF4CPPN11KVGC2Z
title: Build the admin users, groups, and permissions screens
type: task
status: ready
status_reason: null
priority: high
due_on: null
labels:
  - area:moderation
  - area:client
  - effort:L
assignees: []
milestone: phase-2
parent: TKT-01M29BN36JH4M1W5819D06PGPP
origin: null
dependencies:
  - TKT-01M29BYYHGZY39T1F2NGJX8MED
  - TKT-01M29CEE4KNQBFP49FVSCRNZA4
  - TKT-01M3NRYCP5BSA717S1HANWY1AA
blocks_on: none
references:
  - ref: design:data-model-perms
    path: docs/04-data-model.md
claim: null
archive: null
created_at: 2026-09-11T23:30:21Z
updated_at: 2026-09-29T07:18:12Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase2-run-orchestrator
  name: ""
extensions: {}
---

## Description

The user list with role and status, group membership editing, and the permission grid of groups against forums over read, create_thread, reply, upload, and moderate.

A permission grid is where an admin quietly locks everyone out of a forum, including themselves. The UI should show the effective result of a change for a sample member rather than only the rows being edited.

Memberships sourced from an IdP are shown as such and are not editable by hand, since the next login would undo the edit anyway.

## Acceptance criteria

- [ ] An admin can add and remove group members and apply a batch of per-forum and global grants in one transaction, and each change applies on the next request (TestGrantsApplyInOneTransaction, TestPermissionChangeAppliesOnNextRequest)
- [ ] IdP-sourced memberships are marked in the screen, and removing one or adding over one is conflict with the row left as it was (TestIdPMembershipNotEditable)
- [ ] Before applying, the screen shows a chosen member's effective read, create_thread, reply, upload and moderate per forum with the staged changes, and the preview equals the result after applying (TestPreviewMatchesApply)
- [ ] Every role, membership, grant and group change writes its audit_log row in the same transaction, and a failed audit insert leaves the change unmade (TestRoleChangeAuditedInOneTransaction, TestMembershipWritesAudited)
- [ ] Promotion to admin goes through perm.Promote, a demotion writes user.demoted and leaves user.promoted rows alone so bootstrap does not promote again, and the last active admin cannot be demoted (TestDemotionLeavesPromotionRows, TestCannotDemoteTheLastAdmin)
- [ ] Moderators can list users and read status but cannot change roles, groups or permissions or see identities (TestUsersScreenAccess)

## Implementation plan

### Settled in grooming

- Roles are one column, `member`, `moderator` or `admin`. Groups carry forum permissions. An admin passes every check without rows, and `role = moderator` grants `moderate` on every forum the user can see (docs/04 "permissions", docs/05 "Roles and groups").
- Evaluation falls back per group: a group with any row on a forum is governed by those rows alone (docs/04, decided 2026-09-24).
- IdP-mapped memberships are `source = 'idp'` and re-evaluated at every login. An admin's rows are never clobbered (docs/05 "Roles and groups"). This ticket's AC2 says IdP memberships cannot be edited by hand. Note that `AddAdminMembership` would take an `idp` row over and make it `admin`. This ticket refuses instead, so that path is not reachable from the API.
- **Role changes** go through `perm.Evaluator.Promote`, or a sibling that writes its audit row in the same transaction. A demotion writes its own action and leaves `user.promoted` rows alone, because bootstrap reads `user.promoted` as "made admin once", and a demoted bootstrap admin must stay demoted at the next sign-in (docs/05 "Principles", TKT-01M29C800G).
- A role or status change applies from the user's next request, since the viewer is read from `users` on every request (docs/04). The response cache's authors digest sees a role change (docs/07 "The response cache").
- A user's status is shown only to moderators and admins (docs/05 "Account lifecycle", decided 2026-09-28, TKT-01M3KJJNDC).
- Agent accounts are provisioned in this screen, and the credential is TKT-01M29P9RSW's, which depends on this ticket (docs/05 "Agent accounts").
- `internal/perm` owns every write that changes a decision and clears the cache after the commit (`TestMutationsGoThroughPerm`).
- The gate, the audit helper and the admin scaffold come from TKT-01M3NRYCP5. The screens are in the lazy admin chunk. The built client is committed: run `go generate ./internal/web/spa`.
- The URL scheme has `/admin/users` and nothing for groups (docs/07). Groups and permissions are tabs of that screen, `?tab=groups` and `?tab=permissions`, so docs/07's route table does not change.

Engineering choices made here, each with the alternative that lost:
- **Moderators may open `/admin/users` read-only**, seeing the list, the status and the detail without identities. Suspension is a moderator action (docs/08 "Primitives") and is reached from here (TKT-01M29CP2QK). Roles, groups, permissions and identities are admin-only. An admin-only list lost because a moderator would have no way to find the account to suspend.
- **The last active admin cannot be demoted** (`conflict`). The CLI can still promote, but the screen should not be the way a forum loses its only admin.
- **A group can be deleted only when empty**, with no members and no permission rows, or it is `conflict`. Deleting its rows implicitly lost to "No cascading deletes" (docs/04). The screen lists what to clear first.
- **The effective-permission preview runs before the change.** `perm.Preview` evaluates a chosen member against the current rows with the staged changes laid over them, in memory, and shows before and after per forum. Showing only the result after applying lost because the description asks for the effect of a change, and a lockout is only useful to see before it happens.
- **A batch of grid changes is one transaction**, with one audit row per grant or revoke. A half-applied grid is the lockout the description warns about.

### Approach

1. `internal/perm`:
   - Refactor `evaluateMany` into reading the rows, `ForumAccessRows` as now, and a pure `decide(rows, viewer)`. `Check` and `CanEach` behave exactly as before.
   - `ChangeRole(ctx, by, userID, role)`. To `admin` it calls `Promote(ctx, userID, Promotion{Actor: by.Actor, Details: {"from": old}})`, which writes `user.promoted`. From `admin` it writes `user.demoted {from, to}`. Between `member` and `moderator` it writes `user.role_changed {from, to}`. `ErrLastAdmin` is returned when the change would leave no active admin, counted inside the transaction. Remove the unaudited `SetRole` (only tests call it) so no path changes a role without a row.
   - `ApplyGrants(ctx, by, []GrantChange{ForumID, GroupID, Permission, Grant bool})` runs in one `write`. It appends `permission.granted` or `permission.revoked` per change, with target `permission` and id `<forum id or global>:<group id>:<permission>`. It replaces the exported `Grant` and `Revoke`, whose callers are tests.
   - `AddMember(ctx, by, groupID, userID)` refuses an existing `idp` row with `ErrIdPMembership`. `RemoveMember(ctx, by, groupID, userID)` refuses an `idp` row the same way. Each appends `group.member_added` or `group.member_removed` in the same transaction.
   - `Preview(ctx, u dbq.User, changes []GrantChange) ([]ForumEffect, error)`: before and after decisions for all five permissions on every forum, read without the cache.
2. Group CRUD in the new `internal/service/groups`, not in `perm`, because creating, renaming or deleting an empty group changes no decision. `Create` takes a slug (the forum slug rule) and a name; system groups cannot be created (trigger). `Update` changes name and description; a system group's slug and flag are fixed by trigger. `Delete` refuses a system group or a non-empty one. Each appends `group.created`, `group.updated` or `group.deleted`. Queries: `UpdateGroup`, `DeleteGroup`, `GroupHasRows`, `GroupMembers`.
3. `internal/store/db/views/users.go`: `AdminUsers(ctx, tx, filter{Prefix, Role, Status, Kind}, after string, limit)`, keyset by handle. `internal/service/users/admin.go`: `List` and `Detail` gated by `staff`. The detail's identities (provider, email, verified, last login) and session count are filled for admins only. `SetRole` is gated by `admin`.
4. Routes in `internal/web/api/admin_users.go`:
   - `GET /api/v1/admin/users?prefix=&role=&status=&kind=&cursor=` and `GET /api/v1/admin/users/{handle}` (staff).
   - `PUT /api/v1/admin/users/{handle}/role` with `{role}` (admin).
   - `GET` and `POST /api/v1/admin/groups`, and `PATCH` and `DELETE /api/v1/admin/groups/{id}` (admin).
   - `POST /api/v1/admin/groups/{id}/members` with `{handle}`, and `DELETE /api/v1/admin/groups/{id}/members/{handle}` (admin).
   - `GET` and `POST /api/v1/admin/permissions`, the grid and a batch of changes (admin).
   - `POST /api/v1/admin/permissions/preview` with `{handle, changes}` (admin).
   `ErrLastAdmin` and `ErrIdPMembership` map to `conflict` in `internal/problem/domain.go`. Writes are JSON-only and write-limited. All are `no-store`.
5. `internal/dto`:
   - `AdminUser{Author fields, ID, Status, CreatedAt, LastSeenAt}`.
   - `AdminUserDetail{User, Groups [{group, source, added_by *Author, added_at}], Identities []AdminIdentity (null to moderators), Sessions *int}`.
   - `Group`.
   - `PermissionGrid{Groups, Forums, Grants [{forum_id *int64, group_id, permission}]}`.
   - `PermissionPreview{User, Forums [{forum, before, after}]}`, where `before` and `after` map each permission to `allow`, `deny` or `hidden`.
   Goldens for admin and moderator viewers where they differ, with every nullable field both null and set.
6. Client, admin chunk:
   - `screens/admin/UsersScreen.tsx` has the list with filters and a detail panel: a role select for admins, the status badge, groups with a "from IdP" mark and no remove button on those rows, and identities for admins.
   - `GroupsTab.tsx` handles create, rename, delete and members.
   - `PermissionsTab.tsx` is a groups-by-forums grid for one permission at a time, with a permission picker and a Global column first. It holds staged changes, a "Preview for member" handle field showing before and after per forum with changed cells marked, and Apply.
   - The screen says changes take effect within a minute.
7. Docs: docs/07 "Moderation and admin" documents the endpoints. docs/04 "audit_log" lists the actions this ticket writes. docs/05 "Roles and groups" says IdP rows are not editable in the screen.
8. Run `go generate ./internal/web/spa`, commit `dist`, and run the budget check. The reader total is unchanged.

### Tests

- `TestRoleChangeAuditedInOneTransaction`: for each of promote, demote, and member to moderator and back, the action above is written. With a test trigger aborting the audit insert, the role is unchanged and the cache is not invalidated.
- `TestDemotionLeavesPromotionRows`: demote a bootstrap admin, and `user.promoted` is still there. A later sign-in through `identity` bootstrap with `Once` does not promote them again.
- `TestCannotDemoteTheLastAdmin`.
- `TestResponseCacheAfterRoleChange`: extend `TestResponseCacheAfterPromotion` to a demotion through the API. The thread page's role badge changes on the next request.
- `TestMembershipWritesAudited`: add and remove, each in one transaction with its row.
- `TestIdPMembershipNotEditable`: removing an `idp` row is `conflict` and the row stays. Adding over one is `conflict` and the source stays `idp`.
- `TestGrantsApplyInOneTransaction`: a batch whose third change is invalid applies nothing and writes no audit rows.
- `TestPreviewMatchesApply`: for random change sets, `Preview`'s after equals `Check` after `ApplyGrants`, for a chosen member.
- `TestPermissionChangeAppliesOnNextRequest`.
- `TestGroupDeleteOnlyWhenEmpty`.
- `TestUsersScreenAccess`: a member is 403. A moderator can list and read details without identities, and their role change is 403. An admin can do everything.
- `TestMutationsGoThroughPerm` still passes, and inside `perm` only `Promote` and `ChangeRole` call `SetUserRole`.
- Client: `UsersScreen.test.tsx`, `GroupsTab.test.tsx`, `PermissionsTab.test.tsx` (staging, preview and apply). `TestTheAdminBundleIsSeparate` stays green.

### Out of scope

- Suspension and its controls (TKT-01M29CP2QK, which adds them to this screen).
- The lifecycle states (TKT-01M29CKRFS).
- Agent creation and credentials (TKT-01M29P9RSW, which adds them to this screen).
- IdP claim mapping (TKT-01M29CKRF9).
- Admin handle renames: docs/05 "Changing a handle" is decided, but no ticket builds it, and docs/09 does not list it for Phase 2.
