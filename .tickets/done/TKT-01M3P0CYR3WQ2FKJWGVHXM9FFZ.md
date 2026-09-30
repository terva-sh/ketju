---
schema: 3
id: TKT-01M3P0CYR3WQ2FKJWGVHXM9FFZ
title: Add a rename control to the admin users screen
type: task
status: done
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
references:
  - ref: pr:239
    path: null
claim: null
archive: null
created_at: 2026-09-29T07:17:22Z
updated_at: 2026-09-30T16:25:06Z
created_by:
  id: agent:claude-code/phase2-groom
  name: ""
updated_by:
  id: agent:claude-code/phase2-run-p0cyr3
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

- [x] An admin renames a member from the user detail through POST /api/v1/admin/users/{handle}/rename with {handle}, only after confirmation, and a moderator sees no rename control (AdminUserRename.test.tsx)
- [x] The selected account is in the address as /admin/users?user={handle}, and after a rename the address is replaced with the new handle, the detail shows it, and the list is requested again (AdminUserRename.test.tsx)
- [x] A deleted-user- account shows that it cannot be renamed and offers no field (AdminUserRename.test.tsx)
- [x] The control lives under src/screens/admin, Decision.test.tsx and TestTheAdminBundleIsSeparate pass, and the dist regenerated with go generate ./internal/web/spa is committed
- [x] invalid_handle, reserved, taken and same on a validation-failed body attach to the handle field in the shared words of screens/admin/common/handle.ts, taken says the handle may have been given up recently, a conflict says the account cannot be renamed, and a not-found says no account has the handle now (AdminUserRename.test.tsx)
- [x] Loading /admin/users?prefix={p} filters the list by that prefix, so the moderator queue's author link lands on a filtered list, and closing a detail and a new agent's token handover behave as before (AdminUserRename.test.tsx, UsersScreen.test.tsx, AgentControls.test.tsx)

## Implementation plan

Groomed 2026-09-30 for Phase 2 (TKT-01M3M9RR26) against main at f04a476. Refreshed 2026-09-30 against main at 5124209. Revised at claim on 2026-09-30 against main at 6d1e9ab. The route is TKT-01M3NRP3W9's, merged in PR #186. The screen is TKT-01M29CXK1X's UsersScreen, merged.

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

Added in the 2026-09-30 refresh:
- **The field codes as built** (TKT-01M3NRP3W9, PR #186, `internal/service/identity/handle.go`): `invalid_handle` (`CodeInvalidHandle`), `reserved`, `taken` and `same`. Every one arrives on `validation-failed` with `errors: [{field: "handle", code}]` (`handleError` in `rename.go`, docs/07 "Field errors on `validation-failed`"). The settled item above says `invalid`; the code on main is `invalid_handle`, the same code approval answers. The meaning is unchanged. `same` is decided without case (`strings.EqualFold`).
- **The other refusals of the admin route**: `conflict` for an anonymized account (`members.Anonymized`) or for an account renamed by someone else while this rename ran; `not-found` for a handle nobody holds any more. `available_at` is the member's own 30-day cooldown and never comes from the admin route, which has no cooldown.
- **Rename stays admin-only under the forum-grant decision.** The maintainer decided on 2026-09-30 that thread moderation follows the forum grant, and that "Admin screens and account actions keep their role gates" (docs/08 "Decided 2026-09-30", on PR #208's branch; TKT-01M3RQP0TG).

Engineering choices made in the refresh, each with the alternative that lost:
- **The users tab also reads `?prefix=` from the address into its filter.** The moderator queue's author link for staff is `/admin/users?prefix={handle}` (`authorHref` in `screens/admin/queue/QueueScreen.tsx`, PR #185), but `UsersTab` keeps its filters in state and ignores the address, so the link opens an unfiltered list. This ticket moves the selection into the address anyway, and reading the filter's starting value there is one more line of the same change. Filing a separate ticket lost: it would change the same lines of `UsersTab` in another PR on the client lane. The filters still live in state after the first render, so typing in the field does not rewrite the address.
- **The agent handover and the focus return survive the move.** `UsersTab` carries a new agent's detail and token to `UserDetail` through `created` (TKT-01M3QH3GS2), and closing the detail returns focus to the account's row. With the selection in the address, `onCreated` routes to `?user={handle}` and keeps `created`. `close` routes to `/admin/users`, keeping any other query parameter, and the focus effect keys on the handle read from the address.

### Revised at claim

Read from main at 6d1e9ab, after PR #229 (TKT-01M3QMRE47, suspension controls):
- `UserDetail` now has a `refetch` helper and renders `<SuspensionPanel>` after the facts list. `ConfirmButton` has an optional `beforeAsk`. `AgentPanel` (#179) is still rendered for an agent. The rename control goes between the role control and the agent panel and changes none of them.
- The server is as the refresh read it. `RenameMember` calls `members.RequireAdmin`, and the route is `adminRoute(…, adminGate, h.adminRename)`. `rename` answers `validation-failed` with `{field: "handle", code}` for `invalid_handle`, `reserved`, `taken` and `same`; `conflict` for an anonymized account or a lost race; `not-found` for a handle nobody holds. `available_at` rides only on the member's own cooldown conflict (`own && next != nil`), which the admin route never reaches. A conflict that carried one would still read as the cannot-be-renamed sentence.
- preact-iso keeps the matched component mounted when only the query changes (`routeChanged` compares the component), so `UsersTab`'s state, `created` included, survives routing to `?user=`.
- `CheckHandle` normalises (trim, lower case) before `same` is decided. The button is disabled while the trimmed field is empty or equals the current handle in any case, and `same` still has words for a race.

Engineering choices made at claim, each with the alternative that lost:
- **A field error closes the question and focuses the field.** The refresh says the confirmation closes on a field error. `ConfirmButton` closes only when `onConfirm` does not answer `false`, so the control answers `true` after setting the field's error and moving focus to the field. Keeping the question open, as approval's `Decision` does, lost: its prompt would name a handle the admin now has to change.
- **`created` needs no new effect for Back and Forward.** With the selection in the address, history changes it without `choose`. `AgentPanel` takes the token into its own state as it mounts and clears `created` at once, so there is nothing left for history to bring back; `AgentControls.test.tsx` holds that. An effect dropping `created` whenever the address moved was tried and removed: no test could fail without it, because the state it guarded is already empty.
- **After a rename the new detail says what happened and takes focus.** The detail remounts under the new handle, so the Rename button that had focus is gone. `UsersTab` keeps the old handle for the new detail, which shows "Renamed from @{old}. The old handle is kept for 90 days and sends visitors here." as a status line and focuses its heading once it loads. Leaving focus on the page's body lost: a keyboard user would start again from the top.
- **`isAnonymizedHandle` joins the words in `common/handle.ts`.** `SuspensionPanel` keeps its own inline check; changing it is not this ticket's.
- **The `invalid` alias is not added.** The groomed choice read `invalid` and `invalid_handle` as one; the refresh found the server sends only `invalid_handle`, so an unknown code keeps the generic words.

### Approach

1. **Address** (`screens/admin/UsersScreen.tsx`, `UsersTab`):
   - `selected` is `useLocation().query.user`, or null.
   - `userHref(query, handle)` keeps every other query parameter and sets or drops `user`.
   - A row click routes to `userHref(query, handle)`, a push as a link would. `close` sets the focus-return ref and routes to `userHref(query, null)`.
   - `onCreated` reloads the list, sets `created` and routes to the new agent's handle.
   - The effect on `selected` returns focus to the closed row as before, and drops the rename notice once another account is open.
   - `filters.prefix` starts from `query.prefix`.
2. **Handle words** (`screens/admin/common/handle.ts`): `handleWords`, `handleMessage` and `isAnonymizedHandle`, moved from `applications/Decision.tsx` with `same` added: "That is already this account's handle." `Decision.tsx` imports them.
3. **`screens/admin/RenameControl.tsx`**, rendered by `UserDetail` for an admin below the role control:
   - An anonymized handle gets "An anonymized account cannot be renamed." and no field.
   - Otherwise a `Field` "Change handle" with the hint "The old handle is kept for 90 days and sends visitors to the new one. Nobody else can take it until then.", and a `ConfirmButton` "Rename" asking "Rename @{old} to @{new}?".
   - `send<HandleChange>('POST', `${url}/rename`, {handle})`. A field code goes on the field in `handleMessage`'s words; `conflict` and `not-found` get their sentences beside the control; anything else gets `send`'s sentence.
   - On success, `onRenamed(answer.previous, answer.handle)`: `UsersTab` reloads the list, remembers the old handle and routes to `userHref(query, answer.handle)` with replace.
4. `go generate ./internal/web/spa` and commit `internal/web/spa/dist`.

### Tests

- `screens/admin/AdminUserRename.test.tsx`: an admin renames a member (confirmation first, the request, the address replaced with `history.length` unchanged, the detail and its notice, the list asked again); a moderator sees no control; each of `invalid_handle`, `reserved`, `taken` and `same` on the field through `aria-describedby`, keeping what was typed; `taken` says recently given up and no word says released; 409 and 404 in words; an anonymized account; `?user=bob` opens bob; `?prefix=bo` requests and shows the filter, which is where the queue's `authorHref` lands.
- `UsersScreen.test.tsx`: a row click puts `?user=` in the address, and Close takes it out with focus back on the row.
- `AgentControls.test.tsx`: the token does not come back when the agent is reached again through history.
- `Decision.test.tsx`, `SuspensionControls.test.tsx`, `QueueScreen.test.tsx` and `TestTheAdminBundleIsSeparate` pass unchanged.
- `e2e/admin-rename.spec.ts`: ada renames a fresh agent against the real server and the address and the detail follow, with one axe scan per test and the stored session.

### Out of scope

- The rename route, the reservation and the redirect: TKT-01M3NRP3W9, done.
- A member's own handle control: TKT-01M29CXK4E (Build the user settings screens, phase 4).
- Fixing the grandfathered handles that end in `-`: TKT-01M3ANX1NF, done, which can use this control or the route.
- An `anonymized` flag on the admin detail. The `users.anonymized` column exists (`member.Anonymized`), but `dto.AdminUser` does not carry it, so the client keeps the `deleted-user-` prefix rule.
- Changing the queue's author link to open the detail at `?user=`: a follow-up once this lands. Reading `?prefix=` makes the current link work.

## Notes

**agent:claude-code/phase2-groom** at 2026-09-29T22:07:20Z

Planned in Phase 2 grooming (TKT-01M3M9RR26), 2026-09-30. Two things for whoever claims this. (1) The users tab kept the selected account in component state, so there was no address to replace; the plan moves it to ?user={handle}, as the applications screen does with ?id=. (2) TKT-01M3NRP3W9's plan answers a taken handle as conflict with errors[handle] = taken, but internal/problem writes errors only on validation-failed (TestOnlyValidationFailedCarriesErrors) and the client reads field errors only there. Approval already answers taken as validation-failed. This control is built to that shape; TKT-01M3NRP3W9 should answer taken the same way. The grooming report raises it; that ticket was not edited here.

**agent:claude-code/phase2-groom** at 2026-09-30T08:57:21Z

Plan refreshed against main at 5124209 on 2026-09-30. The Settled items are carried verbatim; new ones follow them. Changes: (1) TKT-01M3NRP3W9 merged in PR #186. The codes on main are invalid_handle, reserved, taken and same, all on validation-failed. The settled item's 'invalid' is invalid_handle, and criterion 3 is rewritten to match. (2) The 'Cross-ticket dependency on how taken is answered' section is removed, because taken arrives as validation-failed as this control expected. The step that added HandleChange to dto.ts is removed too, since it is on main. (3) A not-found refusal gets its own words. available_at never comes from the admin route. (4) Moving the selection into the address has to keep the agent token handover and the focus return that TKT-01M3QH3GS2 added to UsersTab. (5) Engineering choice: the users tab also seeds its prefix filter from ?prefix=, because the moderator queue's author link (/admin/users?prefix=, PR #185) opens an unfiltered list today. That criterion is added. (6) The maintainer's decision of 2026-09-30 keeps rename behind the admin gate.

**agent:claude-code/phase2-run-p0cyr3** at 2026-09-30T13:39:10Z

Built on run/p0cyr3-admin-rename against main at 6d1e9ab; held locally until the client dist is free.

Engineering choice added while building, with the alternative that lost: **an admin who renames their own account gets the new handle in the viewer at once.** The admin route allows a self-rename (`RenameMember` checks only `RequireAdmin`). `SuspensionPanel`'s `mayAct` tells the viewer's own account by its handle, so with the viewer left on the old handle, the detail under the new one would offer Suspend on the admin's own account, a control the server refuses (`account.mayActOn`). `onRenamed` therefore sets the viewer's handle before the detail under the new one loads, then asks for `/me` again. Waiting for `/me` alone lost: the detail can load first, and the controls would show until it answered. Hiding the rename control on the admin's own account lost: the server allows it, and docs/05 lets an admin rename any member.

Checks run locally: gofmt, build, vet, `go test -race ./...`, `npx tsc --noEmit -p .`, `npm test` (1712 tests), `go generate ./internal/web/spa`, `TestTheAdminBundleIsSeparate`, and the full Playwright suite on port 8931 (105 passed, 11 skipped by project), including the new `e2e/admin-rename.spec.ts`.

Bundle: the reader bundle is 51211 B gzip at the base and 51212 B with this change, of the 60000 B budget; only the lazy chunks' hashed names changed in it. The admin chunk is 24.69 kB gzip as vite reports it, about 0.6 kB more than the base.

**agent:claude-code/phase2-run-p0cyr3** at 2026-09-30T16:16:15Z

Terva review 1596 on PR #239 (head 057b9c6, base 2ffd8d9), one medium finding, "Keep the prefix filter in sync with query-only navigation": fixed in 18b9f6b. The prefix filter was read from `?prefix=` only as the users tab mounted, so a new prefix in the address by a link or Back, with the tab still mounted, left the list on the old filter. An effect on the address's prefix now makes it the filter whenever it changes. Opening or closing an account keeps the prefix in the address, so what was typed in the field stays. Covered by "follows a new ?prefix= in the address" in AdminUserRename.test.tsx. The reader bundle is 54927 B gzip after the fix.

**agent:claude-code/phase2-run-p0cyr3** at 2026-09-30T16:25:06Z

Terva review 1597 on PR #239 (head 5e1e1e9, base 2ffd8d9). It marks review 1596's finding resolved and raises one medium finding, "Do not navigate back to a rename result after leaving the screen", fixed in c46ad40. `onRenamed` now reads the tab's address and whether the tab is mounted from a ref. Once the tab is gone it moves nobody, and the list is fetched afresh when the tab is next shown. When another account was opened meanwhile, the list is asked for again and the address stays on that account. The viewer's own handle is corrected either way, so the self-rename guard for the suspension controls still holds. Two tests in AdminUserRename.test.tsx cover it. The reader bundle is 54924 B gzip after the fix.

## Summary

An admin renames an account from its detail on /admin/users, after a confirmation, through `POST /api/v1/admin/users/{handle}/rename` (TKT-01M3NRP3W9). The control is `screens/admin/RenameControl.tsx`, rendered by `UserDetail` for an admin only, below the role control. A moderator sees none. An account with a `deleted-user-` handle gets "An anonymized account cannot be renamed." instead of a field.

The open account is now the address's `?user={handle}`. A row click pushes it, Close takes it out and returns focus to the row, a new agent's detail and token are handed over as before, and a rename replaces the address with the new handle. `userHref` keeps any other query parameter. The users tab seeds its prefix filter from `?prefix=`, so the moderator queue's author link (`authorHref`) lands on a filtered list.

The handle words moved from `applications/Decision.tsx` to `screens/admin/common/handle.ts`, with `same` added, and `isAnonymizedHandle` beside them. `invalid_handle`, `reserved`, `taken` and `same` go on the field, and the question closes with focus on the field; `conflict` and `not-found` get sentences of their own. After a rename the detail under the new handle says "Renamed from @{old}…" and takes focus, and the list is asked for again. An admin who renames their own account gets the new handle in the viewer at once and `/me` again, so the suspension controls are never offered on their own account.

Tests: `AdminUserRename.test.tsx` (12), additions to `UsersScreen.test.tsx` and `AgentControls.test.tsx`, and `e2e/admin-rename.spec.ts` (two tests per project, one axe scan each, ada's stored session, no `/auth/*` spend). Replayed onto main at 2ffd8d9. The reader bundle is 54921 B gzip at that base and 54929 B with this change, of the 70000 B budget; only lazy-chunk hashes changed in it. The admin chunk grows from 24952 B to 25647 B gzip.
