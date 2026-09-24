---
schema: 3
id: TKT-01M29BYYHGZY39T1F2NGJX8MED
title: Add the forums, groups, and permissions schema
type: task
status: done
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
  - TKT-01M29BYYH73ZBC9WTS7HN03PKJ
blocks_on: none
references:
  - ref: design:data-model-perms
    path: docs/04-data-model.md
  - ref: pr:51
    path: null
claim: null
archive: null
created_at: 2026-09-11T23:13:37Z
updated_at: 2026-09-24T07:52:19Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase1-run-415af6aa
  name: ""
extensions: {}
---

## Description

`forums` with at most one level of nesting and a visibility of public, members, or restricted. `groups` and `group_members` with the system groups everyone, members, moderators, and admins seeded. `permissions` as (forum_id nullable, group_id, permission) rows over read, create_thread, reply, upload, and moderate.

Deliberately phpBB-shaped and small. A nullable forum_id means a global row, and evaluation is the union of the viewer's groups for that forum falling back to global. Group membership carries a source so an IdP-mapped membership can be re-evaluated at login without clobbering one an admin set by hand.

## Acceptance criteria

- [x] The four system groups are seeded by migration and cannot be deleted
- [x] Forum nesting deeper than one level is refused at write time
- [x] A group_members row records whether it came from an admin or from an IdP claim mapping
- [x] The permission set is exactly the five in docs/04, enforced rather than free text

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

## Notes

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T07:22:22Z

Claimed and released on 2026-09-24 without code. group_members.user_id and added_by reference users, which TKT-01M29BYYH7 creates, and BYYH7's plan declares real foreign keys (REFERENCES, NO ACTION). With foreign_keys=on, a group_members insert before users exists fails, and this plan also reuses BYYH7's TestNoCascadingDeletes. The dependency was missing from the store and is now recorded. BYYH7 goes first.

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T07:43:33Z

### Built, 2026-09-24, against main at e422e22 (after TKT-01M29BYYH7 merged)

Migration `0003_forums_groups.sql` and `queries/forums_groups.sql`.

- **Nesting is one level, enforced by triggers.** On insert, a parent that already has a parent is refused. On an update of parent_id, three cases are refused: the forum as its own parent, a parent that has a parent, and a forum that has children. A table CHECK `parent_id <> id` covers an explicit-id insert that names itself as parent.
- **System groups.** They are seeded with fixed ids 1–4 (everyone, members, moderators, admins), before the triggers that refuse deleting one, changing its slug or is_system, promoting another group, or inserting a new system row. That makes "exactly four" an invariant, not a starting state. Display names can still change.
- **group_members.source** is 'admin' or 'idp'. An idp row names no adder (CHECK). `AddIdPMembership` uses ON CONFLICT DO NOTHING, so an IdP mapping never overwrites a row an admin set. `DeleteIdPMemberships` plus re-adds is the login re-evaluation, and admin rows survive it. A trigger refuses rows for everyone and members, which are implicit (docs/04).
- **Permissions.** CHECK over the five. UNIQUE on `(ifnull(forum_id, 0), group_id, permission)`, because a plain UNIQUE treats NULL global rows as distinct; a mutation showed it. Seeded: members read, create_thread, reply; everyone read, all global.
- **Go vocabulary in internal/domain:** `Permission` with `AllPermissions`, the group slugs and `SystemGroups`, the visibility values and the membership sources. A test compares domain.AllPermissions with the CHECK list parsed from sqlite_schema.
- **`PermissionRows(forum_id, group_ids)`** returns the forum's own grants and the global ones, for the given groups only. That is the query TKT-01M29C5Y2E evaluates.

### Found: a sqlc parameter-numbering trap
`RevokePermission` first mixed `sqlc.narg('forum_id')` with bare `?`. sqlc emitted `forum_id IS ?3 AND group_id = ? AND permission = ?`. SQLite numbers a bare `?` after `?3` as 4, so the arguments bound to the wrong placeholders and the delete silently matched nothing. TestPermissionRowsForAForum caught it. Every parameter in that query is now named, and **`TestQueriesDoNotMixParameterStyles`** fails any query in queries/*.sql that mixes the two styles. Later schema tickets should know this.

### Mutations tried, all caught
The insert depth trigger disabled; the update trigger ignoring children; system groups deletable; rows for the implicit groups allowed; a plain UNIQUE on nullable forum_id; a sixth permission in the CHECK; an IdP mapping overwriting an admin row; the everyone:read seed dropped. The first attempt at the insert-trigger mutation was itself faulty (it left the trigger in place under another name) and was redone.

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T07:45:55Z

Terva review 378 (run c1d78912-d7a0-45ef-b0c2-423cdfaedc5c, Actions run 139), head d1506c315d0255a0b829a1192148c1571369fb1a, base e422e22113dafe542a645d09588bd3c52e5101f7: two medium findings, **both accepted and fixed**. (1) 'Block updates that move memberships into implicit groups': a new trigger, group_members_not_implicit_on_update, refuses UPDATE OF group_id into everyone or members. TestGroupMemberSource moves a row into each; with the trigger disabled, both moves are accepted. (2) 'Preserve the fixed IDs of seeded system groups': groups_system_no_change now also watches id and refuses changing a system group's id. TestSystemGroupsSeededAndUndeletable tries id + 100 on each; without the id clause, moderators and admins move. everyone and members were already held by the FK from the seeded permissions. A follow-up review of the new head is requested.

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T07:48:01Z

Terva review 379 (run 874f23e7-9dad-4e67-8030-ec87f2856a7b, Actions run 141), head 47c911dbcee47c2b681bd835b0f518f90d08b322, base e422e22113dafe542a645d09588bd3c52e5101f7. Both findings from review 378 are marked resolved. One new medium finding, 'Do not conflate forum ID 0 with global grants'. **Accepted and fixed.** SQLite accepts an explicit id of 0 for an INTEGER PRIMARY KEY, and permissions_unique maps a NULL forum_id to 0. forums now has CHECK (id > 0). TestForumNestingDepth inserts ids 0 and -1; without the CHECK, id 0 is accepted. A follow-up review of the new head is requested.

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T07:50:46Z

Terva review 380 (run 90cd900c-b5d7-4f45-b76c-4ea1985fae1b, Actions run 143), head 160f9dde5e562b513d32670b5e99d9f4c2b2b967, base e422e22113dafe542a645d09588bd3c52e5101f7. The finding from review 379 is marked resolved. One new medium finding, 'Allow an admin to take ownership of an existing IdP membership'. **Accepted and fixed.** AddAdminMembership is now INSERT ... ON CONFLICT DO UPDATE SET source='admin', added_by, added_at WHERE group_members.source = 'idp'. An admin add promotes an IdP row, so the next login's re-evaluation keeps it, and it leaves an existing admin row with its original adder and time. AddIdPMembership still never overwrites an admin row. TestGroupMemberSource covers the takeover followed by a login, and the idempotent re-add. The old plain insert fails that test on the primary key; dropping the WHERE fails it on added_at. A follow-up review of the new head is requested.

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T07:52:19Z

Terva review 381 (run 6944ffc5-d11f-4080-a991-27db28872175, Actions run 145), head 8647d281bfc1e46cd3acace8fd83c0e2224e6869, base e422e22113dafe542a645d09588bd3c52e5101f7. The finding from review 380 is marked resolved. One new medium finding, 'Order root forums by position in ListForums'. **Accepted and fixed.** ListForums now left-joins each forum to its parent and orders by the root's position, then the root's id, then root before children, then the child's position. The generated method still returns []Forum. The new TestListForumsOrder builds roots and children whose positions disagree with their ids; the old ordering fails it with [late late-a late-b early early-a]. A follow-up review of the new head is requested.

## Summary

Migration 0003_forums_groups adds forums, groups, group_members and permissions. Forum nesting is one level, enforced by insert and update triggers. The four system groups are seeded with fixed ids, and triggers keep them from being deleted, re-slugged or unflagged, and stop any other group becoming one. group_members.source is 'admin' or 'idp': IdP re-evaluation replaces only idp rows and never overwrites an admin row, and rows for the implicit everyone and members groups are refused. Permissions are the closed five, unique across NULL global rows through an expression index, and seeded with members read, create_thread and reply and everyone read. internal/domain carries the permission, group, visibility and source vocabulary, tested against the schema. A guard test now refuses sqlc queries that mix named and bare parameters, after that silently broke RevokePermission. docs/04 and docs/05 are updated.
