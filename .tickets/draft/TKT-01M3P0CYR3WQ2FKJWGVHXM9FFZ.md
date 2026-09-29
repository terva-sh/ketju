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
updated_at: 2026-09-29T22:07:20Z
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

- [ ] An admin renames a member from the user detail through POST /api/v1/admin/users/{handle}/rename with {handle}, only after confirmation, and a moderator sees no rename control (AdminUserRename.test.tsx)
- [ ] The selected account is in the address as /admin/users?user={handle}, and after a rename the address is replaced with the new handle, the detail shows it, and the list is requested again (AdminUserRename.test.tsx)
- [ ] invalid, reserved, taken and same on a validation-failed body attach to the handle field in the shared words of screens/admin/common/handle.ts, taken says the handle may have been given up recently, and a conflict says the account cannot be renamed (AdminUserRename.test.tsx)
- [ ] A deleted-user- account shows that it cannot be renamed and offers no field (AdminUserRename.test.tsx)
- [ ] The control lives under src/screens/admin, Decision.test.tsx and TestTheAdminBundleIsSeparate pass, and the dist regenerated with go generate ./internal/web/spa is committed

## Implementation plan

Groomed 2026-09-30 for Phase 2 (TKT-01M3M9RR26), against main at f04a476. The route comes from TKT-01M3NRP3W9 (Implement handle changes with cooldown, reservation, and redirect), which is `ready` and not yet built; this ticket depends on it. The screen is TKT-01M29CXK1X's `UsersScreen`, merged.

### Settled in grooming

- **The policy** (decided 2026-09-28, docs/05 "Changing a handle", docs/10 item 9). An admin may rename any member, without the 30-day cooldown and with the same 90-day reservation and redirect. The new handle passes registration's checks: valid, not reserved, free in any case. The old handle is reserved for 90 days, during which `/u/{old}` and `GET /users/{old}` answer 302 to the new one. Every change is audited with the old and new handle. An anonymized account cannot be renamed.
- **The route and its answer are TKT-01M3NRP3W9's**: `POST /api/v1/admin/users/{handle}/rename` with `{handle}`, through `adminRoute` with the admin gate, answering 200 `dto.HandleChange{handle, previous, next_change_at}`. The rename, the release row and the `user.renamed` audit row are one transaction there (`TestRenameIsOneTransaction`, `TestRenameWritesOneAuditRow`). This ticket adds no server code.
- **The field codes have one meaning each** (TKT-01M3NRP3W9, after Terva review 1372): `invalid` (not a valid handle), `reserved` (a route name or the `deleted-user-` prefix, never a released handle), `taken` (held by someone in any case, or released within 90 days), and `same` (the current handle). There is no `released` code, so the words for `taken` say the handle may have been given up recently.
- **Anonymized accounts** are refused with `conflict`. Until the admin detail carries an `anonymized` flag, the client recognises one by the `deleted-user-` handle prefix (the rule in this grooming brief). `domain.AnonymizedHandlePrefix` says registration refuses that prefix, so only an anonymized account holds one; the server itself decides by the `users.anonymized` column.
- **Admins only.** A moderator sees the user detail read-only (TKT-01M29CXK1X) and is offered no rename. The route's admin gate refuses anyone else whatever the screen shows.
- **The admin chunk.** Everything stays under `web/client/src/screens/admin/`, so `TestTheAdminBundleIsSeparate` passes. The built client is committed: run `go generate ./internal/web/spa` and commit `internal/web/spa/dist`.

Engineering choices made here, each with the alternative that lost:
- **The selected account moves into the address, as `?user={handle}`.** On main the users tab keeps the selection in component state (`selected` in `UsersTab`), so there is no address to replace, and this ticket's second criterion cannot be met as written. `ApplicationsScreen` already uses `?id=` for the same purpose. A row click routes to `/admin/users?user={handle}`, and after a rename the address is replaced with the new handle (`route(…, true)`), so Back does not return to a handle that now redirects. A link to an account's detail also becomes possible, which the suspension controls (TKT-01M29CP2QK) can use. Dropping the address half of the criterion lost: an admin who reloads after a rename would lose the account.
- **The handle words move to `screens/admin/common/handle.ts`**, shared by approval (`screens/admin/applications/Decision.tsx`, whose `handleWords` they are today) and rename, with `same` added. Approval's route answers `invalid_handle` (`applications.CodeInvalidHandle`) where TKT-01M3NRP3W9 plans `invalid`, so the table reads both as the same words. A second table in the rename control lost: two sets of words for one rule drift apart.
- **`RenameControl.tsx` is a file of its own**, so this ticket and TKT-01M3QH3GS2 (agent controls), which both change the user detail, do not conflict.
- **No control for an anonymized account.** A `deleted-user-` handle gets the line "An anonymized account cannot be renamed." in place of the field, following the rule that a control the server would refuse is not offered. The `conflict` words below still cover an account anonymized while the detail was open.

### Cross-ticket dependency on how `taken` is answered

TKT-01M3NRP3W9's plan step 4.4 answers a taken handle as `conflict` with `errors[handle] = taken`. That body cannot reach the client as planned:
- `internal/problem/problem.go` writes `errors` only on a `validation-failed` body, and `TestOnlyValidationFailedCarriesErrors` holds that.
- `toProblem` in `web/client/src/api/problem.ts` reads `fieldErrors` only when the type is `validation-failed`.

Approval already answers a taken handle as `validation-failed` with `errors[handle] = taken` (`applications.CodeTaken` in `internal/service/applications/review.go`). This control is built to that shape: every field code arrives on `validation-failed`, and `conflict` means the account cannot be renamed now (anonymized, or renamed by someone else meanwhile). Whoever claims TKT-01M3NRP3W9 should answer `taken` the same way. This grooming does not edit that ticket; the grooming report carries it.

### Approach

1. **Address** (`screens/admin/UsersScreen.tsx`): `UsersTab` reads the selection from `useLocation().query.user` in place of `useState`. `pick` routes to `/admin/users?user={encodeURIComponent(handle)}`. The filters stay in state as now. `UserDetail` is keyed by the handle, as now.
2. **Handle words** (`screens/admin/common/handle.ts`): `handleWords` and `handleMessage` move here from `Decision.tsx` and gain `invalid` (the same words as `invalid_handle`) and `same`: "That is already this account's handle." `taken` keeps its words: "That handle is in use, or was given up too recently to reuse. Choose another." `Decision.tsx` imports them.
3. **`screens/admin/RenameControl.tsx`**, rendered by `UserDetail` when `admin`, below the role control:
   - A `Field` "Change handle" with the hint "The old handle is kept for 90 days and sends visitors to the new one. Nobody else can take it until then."
   - A `ConfirmButton` "Rename", disabled while the field is empty or equal to the current handle, prompting "Rename @{old} to @{new}?".
   - On confirm, `send<HandleChange>('POST', `${url}/rename`, {handle})`.
   - `validation-failed` with `fieldErrors.handle` shows `handleMessage(code)` as the field's `error`, and the confirmation closes by resolving `false`. `conflict` shows "This account cannot be renamed. It may have been anonymized, or renamed by someone else. Reload to see it as it is now." Anything else shows `send`'s sentence.
   - On success, `route('/admin/users?user=' + encodeURIComponent(answer.handle), true)`, and the tab's `reload` asks for the list again so the row shows the new handle.
   - For a `deleted-user-` handle, the line "An anonymized account cannot be renamed." and no field.
4. **Type**: `HandleChange` in `web/client/src/api/dto.ts` is TKT-01M3NRP3W9's (its step 6). If that ticket landed without it, add it here with its golden in `dto-contract.ts`.
5. Run `go generate ./internal/web/spa` and commit `internal/web/spa/dist`.

### Tests

- `screens/admin/AdminUserRename.test.tsx`, with `fetch` stubbed as `UsersScreen.test.tsx` does:
  - `an admin renames a member`: the request is `POST /api/v1/admin/users/alice/rename` with `{"handle":"alicia"}`. Afterwards the address is `/admin/users?user=alicia` by replacement (`history.length` unchanged), the detail shows `@alicia`, and the list is requested again.
  - `a moderator sees no rename control`: with `role: 'moderator'`, no "Change handle" field.
  - `each field code attaches to the field`: for `invalid`, `reserved`, `taken` and `same` in a `validation-failed` body, the words appear as the handle field's error, tied by `aria-describedby`, and the field keeps what was typed.
  - `a released handle reads as taken`: the `taken` words say the handle may have been given up recently, and no word says "released".
  - `a conflict is said in words`: a 409 shows the cannot-be-renamed sentence.
  - `an anonymized account offers no rename`: a `deleted-user-7` detail shows the line and no field.
  - `the address opens the account`: loading `/admin/users?user=bob` shows bob's detail.
- `screens/admin/applications/Decision.test.tsx` still passes with the moved words.
- `TestTheAdminBundleIsSeparate` passes.
- The server's rename tests are TKT-01M3NRP3W9's; the audit row and the transaction boundary are held there by `TestRenameWritesOneAuditRow` and `TestRenameIsOneTransaction`. No session is issued by a rename.

### Out of scope

- The rename route, the reservation and the redirect: TKT-01M3NRP3W9.
- A member's own handle control: TKT-01M29CXK4E (Build the user settings screens, phase 4).
- Fixing the grandfathered handles that end in `-`: TKT-01M3ANX1NF, which uses this control or the route.
- An `anonymized` flag on the admin detail.

## Notes

**agent:claude-code/phase2-groom** at 2026-09-29T22:07:20Z

Planned in Phase 2 grooming (TKT-01M3M9RR26), 2026-09-30. Two things for whoever claims this. (1) The users tab kept the selected account in component state, so there was no address to replace; the plan moves it to ?user={handle}, as the applications screen does with ?id=. (2) TKT-01M3NRP3W9's plan answers a taken handle as conflict with errors[handle] = taken, but internal/problem writes errors only on validation-failed (TestOnlyValidationFailedCarriesErrors) and the client reads field errors only there. Approval already answers taken as validation-failed. This control is built to that shape; TKT-01M3NRP3W9 should answer taken the same way. The grooming report raises it; that ticket was not edited here.
