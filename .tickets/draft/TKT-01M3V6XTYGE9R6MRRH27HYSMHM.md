---
schema: 3
id: TKT-01M3V6XTYGE9R6MRRH27HYSMHM
title: Add handle change and self-deactivation to the settings screen
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:client
  - effort:S
  - area:api
assignees: []
milestone: phase-4
parent: null
origin: null
dependencies:
  - TKT-01M29CXK4E23MC6DWK4VBX6DXS
  - TKT-01M29CCC0X7SY4SHJ46FV40GNW
blocks_on: none
references:
  - ref: pr:360
    path: null
claim: null
archive: null
created_at: 2026-10-01T07:47:39Z
updated_at: 2026-10-02T18:30:52Z
created_by:
  id: agent:claude-code/phase3-groom-pages
  name: ""
updated_by:
  id: agent:claude-code/phase4-run-orchestrator
  name: ""
extensions: {}
---

## Description

`PUT /settings/handle` (TKT-01M3NRP3W9) and `POST /settings/deactivate` (TKT-01M29CKRFS) are built, but no screen calls them. A member cannot change their own handle or deactivate their own account from the client.

Found while grooming TKT-01M29CXK4E (Build the user settings screens). That ticket keeps to the settings docs/09 lists, so these two controls are left to this ticket.

The handle control shows the 30-day cooldown from `next_change_at` or `available_at`, and the field errors `invalid_handle`, `reserved`, `taken` and `same`. Deactivation asks for confirmation, says how to come back (sign in and press Reactivate, docs/05 "Account lifecycle"), then loads `/`.

## Acceptance criteria

- [ ] GET /settings/handle answers {handle, next_change_at} with no-store and the PUT's gates, and next_change_at equals the available_at a PUT inside the cooldown answers (TestHandleStateRoute; golden, dto.ts and docs/07 row)
- [ ] /settings/account shows the handle and, while the cooldown runs, the date it ends; a change is sent only after a confirmation naming the 30 days and the 90-day reservation, and each field code and the cooldown conflict show their words
- [ ] Deactivation is sent only after a confirmation that says how to come back; it ends every session and loads / signed out, and the last active admin's conflict shows in the dialog
- [ ] An e2e test with a seed member of its own per project covers both at 390 px and 1440 px, with one axe scan per test, paceAuth first and a row in e2e/session.ts
- [ ] The dist from go generate ./internal/web/spa is committed, npm run budget keeps the reader within 120000 B gzip, and the PR states the reader's growth
- [ ] After a change, this tab's top bar and Back link show the new handle without a reload, and /u/{old} redirects to /u/{new}

## Implementation plan

Groomed 2026-10-02 for the Phase 4 run (docs/15), against main at ed82f15.

### Settled in grooming

- **What docs/05 already decides, and this ticket does not reopen.**
  - **Handle change** (docs/05 "Changing a handle", decided 2026-09-28, built in TKT-01M3NRP3W9). A member changes their handle at most once every 30 days. The new handle passes registration's checks. The old one is reserved for 90 days, and nobody may take it in that time, the former owner included. `/u/{old}` and `GET /users/{old}` answer a member a 302 with `no-store` (`api.ProfileRedirects`, `moved`). `user.renamed` is audited with `{from, to}`.
  - **Self-deactivation** (docs/05 "Account lifecycle", built in TKT-01M29CKRFS). `perm.ChangeStatus` deletes every session of the account in the transaction that changes the status, the request's own session included. The handler clears the cookie. The last active admin is `conflict`. The member comes back by signing in and pressing Reactivate.
  - Both routes exist and are answered by `PUT /api/v1/settings/handle` (`changeHandle`, `handle.go`) and `POST /api/v1/settings/deactivate` (`deactivate`, `account.go`). This ticket adds the screen, and one read route for the cooldown.
- **The screen.** Decided by the run, pending maintainer review, in docs/05 "Changing a handle" (2026-10-02), with the alternatives that lost:
  - The controls go on a settings screen of their own, `/settings/account` ("Account"), last in the settings links.
  - `GET /settings/handle` answers `{handle, next_change_at}`, so the cooldown shows before the member types.
  - A handle change asks for confirmation, naming the 30 days and the 90-day reservation.
  - Deactivation asks for confirmation, says how to come back, and loads `/` in full.
- **The field errors** are the server's: `invalid_handle`, `reserved`, `taken` and `same` on `handle`, and `conflict` with `available_at` inside the cooldown (docs/07 `PUT /settings/handle`). The words are the ones staff already read (`screens/admin/common/handle.ts`), except for `same`, which says "That is already your handle."

### Open decision

**Class 1, security posture beyond the design (docs/14 "The stop rule, changed", docs/15).** Do a handle change and a self-deactivation need a recent sign-in?

- docs/05 does not say. Its 2026-10-01 decision, the maintainer's (TKT-01M3V6XSW2): "I don't think we need a recent fresh signin for any of those actions". It names three actions: signing out everywhere, linking an identity and removing one. A handle change and a deactivation are not among them. The routes as built ask for none.
- What a stolen session cookie can do with each, as built:
  - **Handle change.** It can rename the member. The member then cannot change their handle again for 30 days. The old handle is reserved for 90 days, and docs/05 lets nobody take it back in that time, the former owner included. An admin's rename skips the cooldown but not the reservation, so even an admin cannot restore the old handle inside 90 days. The audit row records it.
  - **Deactivation.** It can deactivate the account, which ends every session. The member comes back by signing in and pressing Reactivate, so the harm is the same as signing out everywhere, which the maintainer accepted without a recent sign-in.
- Options, with no default chosen:
  1. No recent sign-in for either, as for the three actions the maintainer named.
  2. A recent sign-in for the handle change only, since its effect outlasts the session for 90 days and the member cannot undo it.
  3. A recent sign-in for both.
  4. No recent sign-in, but let the former owner reclaim their own released handle inside the 90 days. That changes docs/05's "nobody, the former owner included" rule.
- Whatever the answer, it is a server change to the routes, which are already live. This screen adds no capability that a request with the cookie lacks today. Under option 1 the plan below stands as written. Under options 2 or 3, a server ticket adds the check and a problem code first, and this screen adds a step that sends the member to sign in again.

The ticket stays in `draft` until the maintainer answers.

### Approach

1. **Server: `GET /api/v1/settings/handle`.**
   - It answers `dto.HandleState {handle, next_change_at}`, `no-store`. `next_change_at` is null when a change is allowed now.
   - It is read from `users.handle_changed_at` with the rule `identity.rename` already applies. Factor the 30-day computation into one function that both call, so the date shown and the refusal cannot disagree.
   - It has the same gates as the `PUT`: `member.Require`, so anonymous is `unauthenticated` and a pre-user `forbidden`.
   - It goes in the route table in `api.go` beside the `PUT`. docs/07 gains a row, `internal/dto` gains the type, and `api/dto.ts` and `dto-contract.ts` gain it with a golden.
2. **Client: problem parsing.** `api/problem.ts` reads `available_at` from a problem body into `ProblemError.availableAt`, a number or null. Nothing reads it today.
3. **Client: shared words.** Move `handleMessage` from `screens/admin/common/handle.ts` to a module outside `screens/admin/`, so the reader does not import from the admin chunk. Give `same` a member's wording beside staff's. `Decision.tsx` and `RenameControl.tsx` import it from the new place.
4. **Client: `screens/account/AccountScreen.tsx`** at `/settings/account`, exported from `AccountRoot.tsx` and routed in `routes.tsx`.
   - It is added to `pages` in `settings.tsx` as `['/settings/account', 'Account']`, last.
   - **Handle card.** It shows the current handle and a text field (`autocomplete="off"`, `spellcheck=false`). While `next_change_at` is in the future, the field is disabled and a line reads "You can change your handle again on <date>", the date drawn through `PostTime` (docs/06, absolute times). Otherwise the hint reads "Once every 30 days. Your old handle sends people to your new one for 90 days, and nobody, you included, can take it in that time."
   - **Change.** "Change handle" opens `Confirm` (`settings.tsx`) with "Change your handle to @new?" and the same two facts. Confirming sends `PUT /api/v1/settings/handle`.
     - On 200 it calls `refreshViewer()`, so the top bar, the Back link and `viewerGuard` see the new handle, and shows "Your handle is now @new." in a status region.
     - A field error closes the dialog and shows its words beside the field, focused.
     - `conflict` shows the `available_at` date.
   - **Deactivate card.** A short paragraph says what happens: you are signed out everywhere, your posts stay with your handle, and you come back by signing in and pressing Reactivate. "Deactivate account" opens `Confirm` with the same words. Confirming sends `POST /api/v1/settings/deactivate`.
     - On 204 it calls `leaveHome()`, the full load of `/` that signing out uses.
     - `conflict` (last active admin) stays in the dialog with its words: "You are the only active admin. Make someone else an admin first."
   - The shell context's title is "Account", and the Back link goes to the profile, as the other settings screens do (`SettingsHead`).
5. **`state/viewerGuard.ts`.** It names a member by handle (`who`). After a rename, another tab of the browser reloads when the guard next hears from a page that starts. That is acceptable, because its pages show the old handle. Until then it keeps the old handle, as it keeps any other stale value. The renaming tab must not reload itself. Its guard sends `who` once, at start, and `refreshViewer` does not send, so this holds. A test pins it.
6. **Build.** Run `go generate ./internal/web/spa` and `npm run budget`.

**Reader bundle.** The settings screens are in the reader's chunk (`AccountRoot.tsx`, which measured both ways). The new screen and its words are estimated at +1300 B gzip. Measure it, and state the growth in the PR.

### Tests

- **Go.**
  - `TestHandleStateRoute` (`internal/web/api/handle_test.go`):
    - `next_change_at` is null before any change, the date after a member's own change, and null again 30 days on;
    - an admin's rename does not move it;
    - it equals the `available_at` a `PUT` inside the cooldown answers;
    - anonymous is `unauthenticated` and a pre-user `forbidden`;
    - the answer is `no-store`.
  - Golden: `HandleState`.
  - The existing `TestRenameRoutes`, `TestReleasedHandleRedirects`, `TestShellRedirectsReleasedProfiles` and `TestReleasedHandleIsTakenEverywhere` stay green. They hold the redirect and the reservation this screen relies on.
  - `TestEachTransitionSignsOutAtOnce` (`account_test.go`) already holds that deactivation ends every session. It must stay green, since the **session chokepoint** invariant rests on it.
- **Vitest** (`screens/account/AccountScreen.test.tsx`, through `renderAt('/settings/account')`):
  - "shows the cooldown date and disables the field while next_change_at is ahead".
  - "a change is confirmed first, then refreshes the viewer and announces the new handle".
  - "each field code shows its words beside the field, and conflict shows available_at".
  - "Cancel and Escape send nothing and return focus to the button".
  - "deactivation is confirmed first, then loads / in full" (`nav.leave`, mocked).
  - "the last active admin's conflict stays in the dialog".
  - `api/problem.test.ts`: `available_at` is read, and is null when absent.
  - `state/viewerGuard.test.ts`: "a rename in this tab does not reload it".
  - `routes.test.tsx` gains `['/settings/account', 'settings-account']`.
  - `screens/admin/applications/Decision.test.tsx` and `AdminUserRename.test.tsx` stay green after the words move.
- **e2e** (`e2e/settings.spec.ts`, a new describe, or `e2e/account.spec.ts`):
  - Two new seed members, one per project, in `cmd/ketju/e2e.go`, following `seedSettings`. No other spec signs in as them, so renaming and deactivating them costs no other spec anything.
  - At 390 px and 1440 px the member signs in, changes their handle on `/settings/account`, sees the top bar with the new handle, and is redirected from `/u/{old}` to `/u/{new}`. The field is then disabled, with the date.
  - They deactivate and land on `/` signed out. A request with their old session cookie is anonymous: `/api/v1/me` answers `user: null`.
  - The spec spends `/auth/*`: one sign-in per test, and the Reactivate path if the test signs in again. So it calls `paceAuth(n)` first and adds its row to the budget table in `e2e/session.ts`.
  - One axe scan per test, with the confirmation open.

### Out of scope

- A recent-sign-in check, which waits on the open decision above.
- Changing docs/05's reservation rule.
- An admin's rename screen, which TKT-01M3NRP3W9 built.

## Notes

**agent:claude-code/phase4-groom-client-account** at 2026-10-02T17:21:26Z

run decision: pending maintainer review. Handle change and self-deactivation go on /settings/account ('Account'), last in the settings links. GET /settings/handle answers {handle, next_change_at} so the cooldown shows first, and both actions are confirmed first. Recorded with the alternatives in docs/05 'Changing a handle' (2026-10-02). Separately, the plan's Open decision asks the maintainer whether either needs a recent sign-in (class 1): docs/05's 2026-10-01 decision names only signing out everywhere, linking and removing an identity. The ticket stays in draft until that is answered.

**agent:claude-code/phase4-run-orchestrator** at 2026-10-02T18:30:52Z

Maintainer decision, 2026-10-02, on the open decision: neither handle change nor self-deactivation asks for a recent sign-in. Asked as a choice, the maintainer picked 'Neither (Recommended)' over 'Handle change only' and 'Both'. Written into docs/05 'Sessions and CSRF' beside the 2026-10-01 decision. The plan's open decision is settled, and the ticket can be promoted.
