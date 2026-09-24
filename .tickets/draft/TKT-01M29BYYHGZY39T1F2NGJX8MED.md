---
schema: 3
id: TKT-01M29BYYHGZY39T1F2NGJX8MED
title: Add the forums, groups, and permissions schema
type: task
status: draft
status_reason: null
priority: urgent
due_on: null
labels:
  - area:db
  - effort:M
assignees: []
milestone: phase-1
parent: TKT-01M29BKBKV6HH8W0DN6QXQNZ0R
origin: null
dependencies:
  - TKT-01M29BW9Q17KEBMHJ91G6QHW36
blocks_on: none
references:
  - ref: design:data-model-perms
    path: docs/04-data-model.md
claim: null
archive: null
created_at: 2026-09-11T23:13:37Z
updated_at: 2026-09-24T05:21:02Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude/phase1-groom
  name: ""
extensions: {}
---

## Description

`forums` with at most one level of nesting and a visibility of public, members, or restricted. `groups` and `group_members` with the system groups everyone, members, moderators, and admins seeded. `permissions` as (forum_id nullable, group_id, permission) rows over read, create_thread, reply, upload, and moderate.

Deliberately phpBB-shaped and small. A nullable forum_id means a global row, and evaluation is the union of the viewer's groups for that forum falling back to global. Group membership carries a source so an IdP-mapped membership can be re-evaluated at login without clobbering one an admin set by hand.

## Acceptance criteria

- [ ] The four system groups are seeded by migration and cannot be deleted
- [ ] Forum nesting deeper than one level is refused at write time
- [ ] A group_members row records whether it came from an admin or from an IdP claim mapping
- [ ] The permission set is exactly the five in docs/04, enforced rather than free text

## Implementation plan

Pre-groom plan (2026-09-24). Revise at claim, after reading the code as it stands then.

### Approach
1. Add migration `NNNN_forums_groups.sql` with the tables `forums`, `groups`, `group_members`, and `permissions`, using the column lists in docs/04. `forums.visibility` is constrained by `CHECK (visibility IN ('public','members','restricted'))`.
2. Refuse nesting deeper than one level with `BEFORE INSERT` and `BEFORE UPDATE OF parent_id` triggers on `forums`. The triggers `RAISE(ABORT, ...)` when the new parent itself has a parent, when a forum that has children is given a parent, or when a forum is made its own parent. The rule lives in the schema, so every write path gets it.
3. Seed the four system groups in the same migration with fixed ids and slugs: `everyone`, `members`, `moderators`, `admins`, each with `is_system = 1`. A `BEFORE DELETE ON groups WHEN OLD.is_system` trigger raises. A `BEFORE UPDATE` trigger refuses changing `is_system` or `slug` on a system row. Go code finds these groups by slug, so they are exported constants: `db.GroupEveryone` and the others.
4. `group_members(group_id, user_id, source, added_by, added_at)` has `CHECK (source IN ('admin','idp'))` and `PRIMARY KEY (group_id, user_id)`. docs/05 names only `'idp'`, and the ticket names "admin". Use `'admin'` and update docs/05 "Roles and groups" in the same commit.
5. `permissions(forum_id NULL, group_id, permission)` has `CHECK (permission IN ('read','create_thread','reply','upload','moderate'))`. A unique expression index on `(ifnull(forum_id, 0), group_id, permission)` is needed because a plain UNIQUE treats NULL forum_ids as distinct. Export the Go constants `db.PermRead` and the others from one list. A test checks that the list matches the CHECK.
6. Add sqlc queries in `queries/forums_groups.sql`: `GroupsForUser(userID)`, `PermissionRows(forumID, groupIDs)` returning forum rows plus global rows, `ForumByID`, and a `SetGroupMembership` that replaces only `source = 'idp'` rows for a user. That last one is the shape TKT-01M29CKRF9 needs.

### Files and packages
New: `internal/store/db/migrations/NNNN_forums_groups.sql`, `queries/forums_groups.sql`, the regenerated `dbq/`, and `forums_groups_test.go`. The triggers depend on the sqlc parse smoke in BW9Q1 step 6. Reuses BW9Q1's `DB` and migration runner, and the generic no-cascade test from BYYH7.

### Tests
- AC1: `TestSystemGroupsSeededAndUndeletable` checks that exactly four `is_system` rows exist after migrate, and that deleting each one fails.
- AC2: a table-driven `TestForumNestingDepth` covers a root (ok), a child of a root (ok), a grandchild (refused), re-parenting a forum that has children (refused), and a self-parent (refused).
- AC3: `TestGroupMemberSource` checks that both sources insert, that `'manual'` is refused, and that the idp-replace query leaves admin rows untouched.
- AC4: `TestPermissionSetIsClosed` checks that each of the five inserts, that `'write'` is refused, and that the Go constant list equals the set parsed from the CHECK in `sqlite_schema`.

### Design
docs/04-data-model.md "forums", "groups, group_members", and "permissions". docs/05-identity-and-accounts.md "Roles and groups".

### Interactions
Consumed by C5Y2E (permission evaluation), TKT-01M29CKRF9 (IdP group mapping), TKT-01M29CKRGS (lock and pin), and the admin screens TKT-01M29CXK19 and TKT-01M29CXK1X.

### Open items
- No doc states the default permission rows for a fresh install, such as everyone:read global and members:create_thread and reply global. Without them a fresh forum is unusable. Decide at claim whether this migration seeds them.
- Open whether membership of `everyone` and `members` is implicit (computed from the viewer) or stored as rows. Implicit is the likely intent. C5Y2E needs the answer.

### Settled in grooming (2026-09-24)

- Default permissions (docs/04, decided 2026-09-24): `everyone` and `members` are implicit memberships, not rows. The seed migration grants members read, create_thread and reply globally, and grants everyone read, which applies only on public forums.
