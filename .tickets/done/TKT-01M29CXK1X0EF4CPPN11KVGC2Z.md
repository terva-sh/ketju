---
schema: 3
id: TKT-01M29CXK1X0EF4CPPN11KVGC2Z
title: Build the admin users, groups, and permissions screens
type: task
status: done
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
  - ref: pr:152
    path: null
claim: null
archive: null
created_at: 2026-09-11T23:30:21Z
updated_at: 2026-09-29T17:19:36Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase2-run-cxk1x
  name: ""
extensions: {}
---

## Description

The user list with role and status, group membership editing, and the permission grid of groups against forums over read, create_thread, reply, upload, and moderate.

A permission grid is where an admin quietly locks everyone out of a forum, including themselves. The UI should show the effective result of a change for a sample member rather than only the rows being edited.

Memberships sourced from an IdP are shown as such and are not editable by hand, since the next login would undo the edit anyway.

## Acceptance criteria

- [x] An admin can add and remove group members and apply a batch of per-forum and global grants in one transaction, and each change applies on the next request (TestGrantsApplyInOneTransaction, TestPermissionChangeAppliesOnNextRequest)
- [x] IdP-sourced memberships are marked in the screen, and removing one or adding over one is conflict with the row left as it was (TestIdPMembershipNotEditable)
- [x] Before applying, the screen shows a chosen member's effective read, create_thread, reply, upload and moderate per forum with the staged changes, and the preview equals the result after applying (TestPreviewMatchesApply)
- [x] Every role, membership, grant and group change writes its audit_log row in the same transaction, and a failed audit insert leaves the change unmade (TestRoleChangeAuditedInOneTransaction, TestMembershipWritesAudited)
- [x] Promotion to admin goes through perm.Promote, a demotion writes user.demoted and leaves user.promoted rows alone so bootstrap does not promote again, and the last active admin cannot be demoted (TestDemotionLeavesPromotionRows, TestCannotDemoteTheLastAdmin)
- [x] Moderators can list users and read status but cannot change roles, groups or permissions or see identities (TestUsersScreenAccess)

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

### Approach (revised at claim, 2026-09-29, against main 10a3a7b)

What changed from the groomed approach, and why:
- `Grant` and `Revoke` stay as one-change wrappers over `ApplyGrants`, taking `audit.By`, rather than being removed. Every write is still audited in its transaction; the wrappers save rewriting some thirty test call sites in five packages.
- A batch change that disagrees with the current rows (a grant of a row that exists, a revoke of one that does not) is `conflict`, and the whole batch is refused. The screen stages against the grid it loaded, so this means someone changed the grid meanwhile, which send.ts already words.
- The groups service is built inside `api.build` from `Deps.Users.DB` and `Deps.Perm` rather than being a new required `Deps` field, so the ten places that build `Deps` (and the server lane's) do not all change.
- `GET /api/v1/admin/groups/{id}` is added: the Groups tab needs a group's members, and the member routes answer with the same shape.
- There is no group slug rule in the tree (forums have no CRUD yet). `domain.ValidateGroupSlug` settles one: 1 to 40 characters of a-z, 0-9 and `-`, starting with a letter or a number. It has no product meaning beyond being a stable identifier for `group_map`; docs/04 records it.
- Permission audit rows use a new target type `permission`, id `<forum id or global>:<group id>:<permission>`, added to `audit.Types` and docs/04's table (TestTargetTypesDocumented holds them together). Membership rows target the user with `{group, group_id, source}` as the IdP mapping's rows do, so one query finds every membership change of a user.
- New queries go in `queries/admin.sql`, and new DTOs in `internal/dto/admin.go` with their fixtures registered from `admin_fixtures_test.go`, to keep out of the server lane's files. `declaredTypes` in the golden test reads every non-test file of the package instead of `dto.go` only, so the new types are held to the same reachability rule.

1. `internal/perm`: split `evaluateMany` into reading (`ForumAccessRows`), `collect` and a pure `decide`. Add `ChangeRole`, `ApplyGrants` (with `Grant`/`Revoke` wrappers), audited `AddMember`/`RemoveMember` taking `audit.By`, and `Preview`. `Promote`'s body becomes `promote(q, ...)` so `ChangeRole` promotes inside its own transaction. `SetRole` is removed. `ErrLastAdmin`, `ErrIdPMembership`, `ErrGrantState` map to `conflict`; `ErrInvalidChange` and `ErrInvalidRole` to `validation-failed`; `ErrNoUser` to `not-found`.
2. `internal/service/groups`: list, detail, create, update, delete (empty only, one guarded statement), members, grid, apply, preview.
3. `internal/store/db/views/adminusers.go` and `internal/service/users/admin.go`: `List`, `Detail` (staff; identities and session count for admins only), `SetRole` (admin).
4. Routes in `internal/web/api/admin_users.go` through `adminRoute`, as the groomed approach lists, plus `GET /admin/groups/{id}`.
5. DTOs and goldens for admin and moderator viewers where they differ.
6. Client: `UsersScreen`, `GroupsTab`, `PermissionsTab` in the admin chunk; AdminRoot renders the Users section.
7. Docs: docs/07 endpoints, docs/04 audit actions, target type and group slug rule, docs/05 IdP rows not editable by hand.
8. `go generate ./internal/web/spa`, commit dist, budget check.

### Tests

The groomed list stands: TestRoleChangeAuditedInOneTransaction, TestDemotionLeavesPromotionRows, TestCannotDemoteTheLastAdmin, TestResponseCacheAfterRoleChange, TestMembershipWritesAudited, TestIdPMembershipNotEditable, TestGrantsApplyInOneTransaction, TestPreviewMatchesApply, TestPermissionChangeAppliesOnNextRequest, TestGroupDeleteOnlyWhenEmpty, TestUsersScreenAccess, TestMutationsGoThroughPerm plus a check that only `promote` and `ChangeRole` call `SetUserRole`, and the client tests UsersScreen, GroupsTab, PermissionsTab. TestTheAdminBundleIsSeparate stays green.

### Out of scope

- Suspension and its controls (TKT-01M29CP2QK, which adds them to this screen).
- The lifecycle states (TKT-01M29CKRFS).
- Agent creation and credentials (TKT-01M29P9RSW, which adds them to this screen).
- IdP claim mapping (TKT-01M29CKRF9).
- Admin handle renames: docs/05 "Changing a handle" is decided, but no ticket builds it, and docs/09 does not list it for Phase 2.

## Notes

**agent:claude-code/phase2-run-cxk1x** at 2026-09-29T16:46:48Z

Evidence for the criteria, and the decisions taken while building (2026-09-29).

### Where each criterion is proved

1. `internal/perm` TestGrantsApplyInOneTransaction (a batch with a bad third change, of six kinds, applies nothing and writes no audit row; a good batch writes one row per change; a failing audit insert takes the batch with it). `internal/web/api` TestPermissionChangeAppliesOnNextRequest (the member's next request is refused after the batch) and TestGroupDeleteOnlyWhenEmpty (members added and removed through the API).
2. `internal/perm` TestIdPMembershipNotEditable and `internal/web/api` TestIdPMembershipNotEditableThroughTheAPI (409, row and source unchanged). The screen marks the row (`data-idp`) with no remove button: UsersScreen.test.tsx and GroupsTab.test.tsx.
3. `internal/perm` TestPreviewMatchesApply (40 random grids and batches per run, members of every role, some suspended; before equals Check now, after equals Check after ApplyGrants) and TestPreviewEdgeCases. PermissionsTab.test.tsx covers staging, the preview request and its marked cells.
4. TestRoleChangeAuditedInOneTransaction, TestMembershipWritesAudited, TestGrantsApplyInOneTransaction, and TestGroupWritesAuditedInOneTransaction for create, rename and delete, each with an injected audit failure.
5. ChangeRole promotes through `promote`, the body Promote runs, so it is the same write and audit row as bootstrap's. TestDemotionLeavesPromotionRows, TestCannotDemoteTheLastAdmin (a suspended admin does not count), TestCannotDemoteTheLastAdminThroughTheAPI, and TestOnlyRoleWritesSetTheRole (only `promote` and `ChangeRole` call SetUserRole). `internal/web/auth` TestBootstrap… now demotes through ChangeRole and still sees no second promotion at sign-in.
6. TestUsersScreenAccess: member 403; moderator lists and reads with `identities` and `sessions` null and the email absent from the body; every change route 403 to them.

### Decisions settled here (the plan's revised approach lists the rest)

- The only group-change rule the tree lacked was a slug rule. `domain.ValidateGroupSlug` sets 1 to 40 of a-z, 0-9 and `-`, starting with a letter or a number, recorded in docs/04. Alternative: the handle rule, which lost because it allows `_` and caps at 30, neither of which means anything for a group.
- Name 1 to 80 characters with no control characters, description at most 500, batches at most 500 changes: limits with no product meaning, recorded in docs/07.
- Adding a member already in the group, or removing one who is not, is a no-op with no audit row rather than conflict. Only an IdP row is conflict, because that is the one refusal with a reason the admin must see.
- docs/05 used to say an admin removes an IdP membership to cut it short. Since AC2 forbids that, it now says suspension is the way, and why the screen refuses the edit.
- The admin test fixtures' writes are audited now too, so the IdP-mapping audit helpers in `perm` and `web/auth` filter to `source = 'idp'` rows.
- `web/client/src/testing.tsx` settle waits up to a second rather than 250 ms: the first import of the larger admin chunk failed `routes.test.tsx` once in four full runs.
- The reader bundle went from 38548 to 38665 B: export bindings the admin chunk imports from the main chunk.

**agent:claude-code/phase2-run-cxk1x** at 2026-09-29T16:46:59Z

Supersedes one name in the previous note: the internal/web/auth test under AC5 is TestBootstrapPromotesOnceAtSignIn.

**agent:claude-code/phase2-run-cxk1x** at 2026-09-29T16:53:16Z

Review 1414 (PR #152, head 6c5e40f, base 10a3a7b, run 932): three findings, all fixed in 1233cb9 with a test each that fails without its fix. Finding 1 (high, an Apply in flight could clear a later staged change): the grid and Discard are locked while the batch is with the server, and success unstages only what was sent. Finding 2 (high, a stale preview answer could be shown): a counter over the batch, handle and grid drops an answer for an older one. Finding 3 (medium, a More page could join a list drawn by a later filter): a filter change aborts the request and drops a late answer. The reader bundle is now 38671 B.

**agent:claude-code/phase2-run-cxk1x** at 2026-09-29T16:59:48Z

Terva review 1416 (reviewed head 3b9ecea, base 10a3a7b, run 936, PR #152 comment 18673) confirmed the three review 1414 findings were resolved and raised three more, all fixed in b8674c1 (branch SHA; rebase merge rewrites it).

1. high, a preview asked for during Apply could survive the grid change: Preview is disabled while a batch is with the server, runPreview returns early then, and a successful apply calls stale() so any outstanding answer is dropped. Test: PermissionsTab "previews nothing across an apply".
2. medium, a committed batch could be reported as a failed Apply because the grid was read back in a second transaction: perm.ApplyGrantsThen runs a read callback inside the transaction that applied the batch, and groups.Apply reads the grid through it. Test: TestApplyGrantsThenReadsInTheSameTransaction.
   Considered and not changed: SetRole and group membership writes also read the answer back after their transaction. Left as is because a retry of either is idempotent (a role already set, a membership already present or absent, is a no-op without an audit row), so a failed read-back cannot lead an admin into a second, different write. A grant batch is also idempotent per row, but its answer is the grid the admin stages the next batch against, so it was the one worth making atomic.
3. medium, the groups screen could not edit a description: the group form now has Name and Description, labelled "Edit the group", and sends only the changed fields in the PATCH. Test: GroupsTab "edits the description, sending only the fields that changed".

Checks: gofmt, go vet, go test -race ./..., tsc, vitest (1021 tests) all pass; dist regenerated, reader bundle 38669 of 60000 B.

**agent:claude-code/phase2-run-cxk1x** at 2026-09-29T17:17:46Z

Terva review 1419 (reviewed head b20ef7c, base 10a3a7b, run 939) marked all three review 1416 findings resolved and raised one:

1. medium, the users list kept an account's old role after a role change, and could keep it under a Role filter it no longer matched: fixed in 3ad3d28 (branch SHA). A role change now restarts the list (later pages dropped, a pending More aborted, as a filter change does) and asks the server for it again. Test: UsersScreen "asks for the list again after a role change", confirmed to fail with the reload call removed.

Main moved to 3eab1e9 (TKT-01M29CP2KE, the form schema) and the PR conflicted; merged in be1a606 without a force-push. Every conflict kept both sides (audit target types, admin route registration, handler deps, docs/04 and docs/07, dto.ts). Two test helpers collided: the admin fixtures use main's tAdmin, and this branch's adminWorld.send is renamed call. The admin world's setup grant and membership now go through perm's audited writes, so the world starts with two audit rows; main's two forms tests that nothing is written (TestInvalidSchemaIsNotSaved, TestPreviewWritesNothing) now compare against the count after setup rather than an absolute 1.

Checks after both commits: gofmt, go vet, go test -race ./..., tsc, vitest (1024) pass; dist regenerated, reader bundle 38673 of 60000 B.

**agent:claude-code/phase2-run-cxk1x** at 2026-09-29T17:19:36Z

### Re-branched: PR 152 replaced

Main moved to 3eab1e9 (TKT-01M29CP2KE, the form schema) and PR 152 conflicted. The run instruction was to add a commit that resolves it without force-pushing, so main was merged in (be1a606). But docs/12 says a conflicting lane rebases onto main rather than merging main in, and the merge would not have landed: Forgejo's rebase merge replays each non-merge commit onto main, and `git merge-tree` shows 1762985 conflicting there in internal/audit/audit.go, internal/problem/domain.go and docs/04, although the PR reported mergeable.

Alternatives considered: a force-push after a local rebase is not allowed; a merge style other than rebase is outside the run's authority. So, as TKT-01M29CP2KE did with PR 150, the work is carried onto main at 3eab1e9 as `run/cxk1x-admin-users-2`: one commit, 375650a, whose tree equals PR 152's head 5b323bc (checked with an empty `git diff`), and this ticket-store commit. PR 152 is closed unmerged, pointing to the new PR, and its branch is kept for the per-commit history, which includes the answers to reviews 1414, 1416 and 1419. Review round 4 dispatched on 5b323bc is superseded; the new PR gets a fresh review.

## Summary

The admin users screen is built, server and client, and every role, membership, grant and group change is audited in its own transaction.

- `internal/perm`: `ChangeRole` (promotion through Promote's body, `user.demoted` that leaves `user.promoted` alone, `user.role_changed`, and the last active admin refused), `ApplyGrants` (one transaction, one `permission.*` row per change, refused whole when a change names nothing or disagrees with the rows), audited `AddMember`/`RemoveMember` that refuse IdP rows, and `Preview`, which decides with the same `decide` Check uses. The unaudited `SetRole` is gone; `Grant`/`Revoke` are one-change batches.
- New `internal/service/groups`, admin methods on `internal/service/users`, `views.AdminUsers`, and thirteen routes under `/api/v1/admin/` behind the staff or admin gate (docs/07).
- DTOs and goldens in `internal/dto/admin.go`; new audit target type `permission` and the group slug rule in docs/04; docs/05 says an IdP membership is not edited by hand.
- Client: `UsersScreen` with Groups and Permissions tabs in the lazy admin chunk, with staging, preview and apply for the grid. The reader bundle is 38665 of 60000 B.

For the next ticket: suspension (TKT-01M29CP2QK) and agent provisioning (TKT-01M29P9RSW) add their controls to `UsersScreen`'s detail panel; `member.RequireStaff`/`RequireAdmin` and `member.AdminUser` are there to reuse. AdminRoot renders a section through its `built` map.
