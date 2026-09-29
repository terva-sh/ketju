---
schema: 3
id: TKT-01M3P0CYR3WQ2FKJWGVHXM9FFZ
title: Add a rename control to the admin users screen
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:client
  - area:moderation
  - effort:S
assignees: []
milestone: phase-2
parent: TKT-01M29BN36JH4M1W5819D06PGPP
origin: null
dependencies:
  - TKT-01M3NRP3W9XSXYMSPNYS7JGYF4
  - TKT-01M29CXK1X0EF4CPPN11KVGC2Z
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-29T07:17:22Z
updated_at: 2026-09-29T14:45:25Z
created_by:
  id: agent:claude-code/phase2-groom
  name: ""
updated_by:
  id: agent:claude-code/phase2-groom
  name: ""
extensions: {}
---

## Description

TKT-01M3NRP3W9 (Implement handle changes with cooldown, reservation, and redirect) builds `POST /api/v1/admin/users/{handle}/rename`, and docs/05 "Changing a handle" (decided 2026-09-28) lets an admin rename any member, which is how grandfathered handles ending in `-` get fixed (TKT-01M3ANX1NF). No screen calls the route. TKT-01M29CXK1X (Build the admin users, groups, and permissions screens) lists admin renames as out of scope, and the member's own control belongs to TKT-01M29CXK4E (Build the user settings screens), which is phase-4.

Found while planning TKT-01M3NRP3W9 in Phase 2 grooming (TKT-01M3M9RR26).

### What to build

- On the admin user detail from TKT-01M29CXK1X, for an admin only: a "Change handle" field and a confirm button, through `screens/admin/common/` (TKT-01M3NRYCP5).
- The answer's field errors attach to the field, with the meanings TKT-01M3NRP3W9 fixes: `invalid` (not a valid handle), `reserved` (a route name or the `deleted-user-` prefix), `taken` (held by someone in any case, or released by a rename within the last 90 days), and `same` (the current handle). The words for `taken` say it may have been released recently. There is no separate code for a released handle. A `conflict` for an anonymized account says it cannot be renamed.
- The screen says that the old handle is reserved for 90 days and redirects to the new one.
- After success the detail reloads under the new handle, and the address is replaced with it.
- Everything stays under `screens/admin/`, so `TestTheAdminBundleIsSeparate` passes. Rebuild the dist with `go generate ./internal/web/spa`.

## Acceptance criteria

- [ ] An admin can rename a member from the admin user detail through POST /api/v1/admin/users/{handle}/rename, and a moderator sees no rename control (AdminUserRename.test.tsx)
- [ ] After a rename the detail shows the new handle and the address is replaced with it
- [ ] The control lives under src/screens/admin, TestTheAdminBundleIsSeparate passes, and the dist is regenerated with go generate ./internal/web/spa and committed
- [ ] invalid, reserved, taken and same attach to the handle field with TKT-01M3NRP3W9's meanings, a released handle shows as taken, and an anonymized account's conflict is said in words (AdminUserRename.test.tsx)
