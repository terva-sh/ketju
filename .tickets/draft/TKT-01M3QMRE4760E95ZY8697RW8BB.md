---
schema: 3
id: TKT-01M3QMRE4760E95ZY8697RW8BB
title: Add the suspension controls to the admin users screen
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:moderation
  - area:client
  - effort:M
assignees: []
milestone: phase-2
parent: TKT-01M29BN36JH4M1W5819D06PGPP
origin: null
dependencies:
  - TKT-01M29CP2QKKZSK4PDB40EJHC3P
  - TKT-01M3QH3GS23M8VG1JEPCTGSGR1
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-29T22:32:24Z
updated_at: 2026-09-30T01:18:10Z
created_by:
  id: agent:claude-code/phase2-run-cp2qk
  name: ""
updated_by:
  id: agent:claude-code/phase2-groom
  name: ""
extensions: {}
---

## Description

Put the suspension controls into the admin users screen's detail panel (web/client, admin chunk). The server side is TKT-01M29CP2QK (Implement user suspension from the moderator tools). It shipped without UI because the client lane held the committed dist at the time (the docs/13 lane rule).

The routes are in docs/07 "Users, groups and permissions". Each answers 204 `no-store`:
- `POST /api/v1/admin/users/{handle}/suspend` `{reason, until?, hide_posts?}`
- `POST /api/v1/admin/users/{handle}/unsuspend` `{restore_posts?}`
- `POST /api/v1/admin/users/{handle}/posts/hide`
- `POST /api/v1/admin/users/{handle}/posts/restore`

The groomed plan for TKT-01M29CP2QK (step 7) described the screen:
- **Suspend** opens a dialog with a required reason (1 to 500 characters) and an end: 1 day, 7 days, 30 days, until lifted, or a date. It has a "Hide their posts" checkbox, explained as "Members see 'removed by a moderator'; you can restore them later".
- **Unsuspend** has a "Restore posts hidden with this suspension" checkbox.
- A standalone "Restore hidden posts" action works on any account whose posts a suspension hid, which is how a moderator restores the posts of a suspension that expired.
- The status badge shows the reason and the end time.
- A moderator sees the controls only on members, and an admin on anyone but themselves. The server refuses anything else, and answers `conflict` for the last active admin.
- Moderators open the users screen read-only apart from these controls.

`GET /admin/users/{handle}` does not return the suspension's reason or end yet. Adding them to `AdminUserDetail` is a server change for whoever takes this ticket.

Tests: the dialog's validation and the badge text. `TestTheAdminBundleIsSeparate` stays green, and the bundle stays within the 60000 B gzip budget.

## Acceptance criteria

- [ ] GET /api/v1/admin/users/{handle} answers suspension {reason, until} for a suspended account and null otherwise, to admins and moderators, no-store, with a DTO golden matched in dto-contract.ts (TestAdminDetailCarriesTheSuspension)
- [ ] Suspend sends exactly {reason, until, hide_posts} only after confirmation: until is now plus 1, 7 or 30 days, null for until lifted, or the chosen local moment, and hide_posts is unchecked by default (SuspensionControls.test.tsx)
- [ ] An empty or blank reason, one over 500 code points, one with a control character, and a past end are refused beside the field with nothing sent (SuspensionControls.test.tsx)
- [ ] Unsuspend sends {restore_posts} from an unchecked checkbox; Hide their posts (suspended accounts only) and Restore hidden posts each send one bodiless POST after confirmation (SuspensionControls.test.tsx)
- [ ] A moderator sees the controls only on members, an admin on anyone but themselves, and nobody on a pending or deleted-user- account (SuspensionControls.test.tsx)
- [ ] The controls live in screens/admin/SuspensionPanel.tsx, UserDetail gains only its import, refetch and one element, TestTheAdminBundleIsSeparate passes, and the dist from go generate ./internal/web/spa is committed
- [ ] A suspended account shows its reason as text and its end, or until lifted; after every answer, success or failure, both the detail and the users list are fetched again, so a suspend whose hide failed leaves the row saying suspended (SuspensionControls.test.tsx)
- [ ] The panel awaits refetch; the finish-the-hide words show only for a 503 or 500 to a suspend with hide_posts when the account went from not suspended to suspended; a 409 with the refreshed account suspended says it is already suspended; a 409, 503 or 500 leaving it active, or a failed refetch, never mentions a hide; a 409 on unsuspend has its own sentence (a hide that stopped, already suspended is not a partial hide, a refused suspend is not a partial hide, a busy database is not a partial hide, a refetch that fails)

## Implementation plan

Groomed 2026-09-30 for Phase 2 (TKT-01M3M9RR26), against main at 43a844d. The server half is merged: TKT-01M29CP2QK (Implement user suspension from the moderator tools), PR #172, on the lifecycle from TKT-01M29CKRFS. The screen is TKT-01M29CXK1X's `UsersScreen`, merged.

### Settled in grooming

- **Who may suspend whom** (maintainer decision 2026-09-29, docs/05 "Account lifecycle", docs/08 "Suspension"): a moderator suspends members only, and an admin anyone except themselves. Nobody may suspend the last active admin (`conflict`). The same authority covers unsuspending, hiding and restoring (`account.mayActOn`, asked by the server in the transaction whatever this screen offers).
- **Expiry leaves the posts hidden** (maintainer decision 2026-09-29, docs/08 "Suspension"). The posts a suspension hid stay hidden when it expires, until a moderator restores them. So the standalone "Restore hidden posts" action must be reachable on an account that is no longer suspended.
- **No shadow banning** (docs/08 "What is intentionally missing"). The member reads the reason and any end at sign-in (docs/05 "Login flow"), so the reason field says that the member will see it.
- **The routes are TKT-01M29CP2QK's** (docs/07 "Users, groups and permissions"). Each answers 204 `no-store` and carries no count:
  - `POST /api/v1/admin/users/{handle}/suspend` `{reason, until?, hide_posts?}`. The reason is 1 to 500 characters once trimmed (`account.ReasonMaxLen`, counted in code points), with no control characters. `until` is Unix ms in the future, or null for no end. Suspension starts only from `active` or `deactivated`; anything else is `conflict`. With `hide_posts`, the suspension commits first. A hide that fails part-way answers the error with the account suspended, and `…/posts/hide` finishes it.
  - `POST …/unsuspend` `{restore_posts?}`. An account that is not suspended is `conflict`. With `restore_posts`, only the posts of the suspension it lifted come back.
  - `POST …/posts/hide` needs a suspended account (`conflict` otherwise). It is idempotent and answers `unavailable` when a thread could not be reached, after doing the rest.
  - `POST …/posts/restore` works whatever the account's status. It restores only posts a suspension of this account hid, in forums the caller may moderate.
- **Suspending an agent revokes its credential, and unsuspending does not bring it back** (docs/05 "Agent accounts"). The confirmation says so for an agent.
- **The groomed screen** (TKT-01M29CP2QK's plan, step 7, carried into this ticket's description). Suspend opens a form with a required reason and an end: 1 day, 7 days, 30 days, until lifted, or a chosen date. It has a "Hide their posts" checkbox, explained as "Members see 'removed by a moderator'; you can restore them later". Unsuspend has a "Restore posts hidden with this suspension" checkbox. A standalone "Restore hidden posts" works on any account whose posts a suspension hid. The status shows the reason and the end. Moderators open the users screen read-only apart from these controls.
- **The audit log is admin-only** (maintainer decision 2026-09-29, docs/08). Nothing shown to a moderator here may be read from `audit_log`.
- **The admin chunk.** Everything stays under `web/client/src/screens/admin/`, reached only through the lazy import in `routes.tsx`, so `TestTheAdminBundleIsSeparate` passes. The built client is committed: run `go generate ./internal/web/spa` and commit `internal/web/spa/dist`. Only one lane may have a PR open that changes the dist (docs/13 "Running two lanes").

Engineering choices made here, each with the alternative that lost:
- **The detail gains the suspension's reason and end.** `GET /admin/users/{handle}` does not return them today, so the screen cannot show them. `dto.AdminUserDetail` gains `suspension: {reason, until} | null`, read from `users.status_reason` and `users.status_until` and null unless the status is `suspended`. It is staff's, moderators included, because moderators suspend and the member is shown the same reason. Putting it on `AdminUser`, so the list rows carry it, lost: the list shows the status only, and every row would grow for one detail panel.
- **"Restore hidden posts" is offered on every account the viewer may act on, except a pending or anonymized one.** A flag saying "this account has posts hidden by a suspension" lost for two reasons. It would be read from the `user.posts_hidden` audit rows, which would show a moderator part of the admin-only audit log, including an admin's hide in a forum the moderator cannot see. It would also go stale after a restore that stopped part-way, hiding the one control that finishes it. The restore is exact and idempotent, so pressing it on an account with nothing hidden changes nothing.
- **"Hide their posts" is offered on its own for a suspended account.** It finishes a hide that stopped part-way, and it lets a moderator hide the posts after suspending without doing so.
- **Both checkboxes start unchecked.** Hiding and restoring the posts are judgements separate from the suspension, as the maintainer's expiry decision reasons. Checked defaults lost because they would hide or restore posts without anyone choosing to.
- **A chosen end is a date and a time** (`<input type="datetime-local">`, `min` now), sent as that local moment in Unix ms, so the end the moderator picks is the end the status shows. A date alone lost: it leaves open when on that day the suspension ends. The presets are now plus 24 h times 1, 7 or 30.
- **`refetch` answers the detail it fetched** (Terva review 1520 on PR #181). A failed suspend's words depend on the account's status after the failure, so the panel must read the refreshed detail, not the one it rendered with. `refetch: () => Promise<AdminUserDetail | null>` fetches the detail with `apiGet`, stores it as `UserDetail`'s `fresh`, and resolves to it, or to `null` when the fetch itself fails. The panel awaits it before choosing its words. Two alternatives lost. `res.retry()` returns nothing to await, so the words would be chosen from the stale detail. Deriving the words in an effect when new props arrive would tie the message to render timing, and a detail refreshed by something else could set it off.
- **One new file per control group.** The controls are `screens/admin/SuspensionPanel.tsx`, which holds the status line, the suspend form, unsuspend, hide and restore. `UserDetail` gains one import, a `refetch` helper and one element. TKT-01M3QH3GS2 (agent controls, PR #179) and TKT-01M3P0CYR3 (rename control) also change `UserDetail`, in `AgentPanel.tsx` and `RenameControl.tsx`. Adding the controls inline in `UserDetail` lost because of the merge conflicts with those two.
- **Every answer refetches the detail and asks for the list again**, success or failure (Terva review 1516 on PR #181, finding 2). The routes answer 204 with no detail. A failure can also leave the account changed: a suspension can commit before its hide fails, and after that the list row would still say `active`. Two alternatives lost. Patching the detail locally would guess at what the server did. Reloading the list only when the refetched detail differs from the old one lost: a missed change costs more than one extra list request after a refusal.
- **The status line lives in the panel, not in the facts list.** `UserDetail`'s `<dd data-status>` stays as it is. The panel draws its own line under the facts, so the other two tickets' hunks are left alone.

### Approach

1. **Server: the detail carries the suspension.**
   - `internal/dto/admin.go`: a new `AdminSuspension` struct with `Reason string` (JSON `reason`) and `Until *Millis` (JSON `until`), and `AdminUserDetail.Suspension *AdminSuspension` (JSON `suspension`). It is documented as null unless the account is suspended, with `until` null for no end.
   - `internal/service/users/admin.go`, `adminDetail`: set it from `u.StatusReason` and `u.StatusUntil` when `u.Status == domain.StatusSuspended`, before the `if !admin` return, so moderators get it. `SetRole` and `SetAgentLimits` answer through `adminDetail` and carry it too.
   - `internal/dto/admin_fixtures_test.go`: an `admin-user-suspended.moderator` fixture, with its golden. `web/client/src/api/dto.ts`: `AdminSuspension` and the field. `web/client/src/api/dto-contract.ts`: `matches<AdminUserDetail>()` for the new golden.
   - docs/07 "Users, groups and permissions": the `GET /admin/users/{handle}` row names `suspension`.
2. **`screens/admin/SuspensionPanel.tsx`**, with props `{d: AdminUserDetail, url: string, refetch: () => Promise<AdminUserDetail | null>, onListChanged: () => void}`. It reads the viewer from `state/viewer`.
   - `mayAct(viewer, d)` mirrors `mayActOn`: false for the viewer's own account, true for an admin, true for a moderator on a `member`, false otherwise. It is also false for a `pending` account and for a `deleted-user-` handle (the anonymized rule in this grooming). When false, the panel renders only the status line.
   - **Status line** (`data-suspension`), when `d.suspension` is set: "Suspended until {whenAt(until)}" or "Suspended until lifted", then "Reason: {reason}" as text, never HTML. `whenAt(ms)` formats local `YYYY-MM-DD HH:MM` and sits in this file.
   - **Suspend** (status `active` or `deactivated`): a "Suspend" button opens an inline form, not a modal, in the style of the other admin forms.
     - A `Field` "Reason", with the hint "The member sees this when they try to sign in." It is a single-line `<input>`, so it cannot hold a newline. A live count shows code points out of 500.
     - A radio group "Ends": 1 day, 7 days, 30 days, until lifted, or on a date. The last reveals a `datetime-local` input.
     - The "Hide their posts" checkbox with the settled hint.
     - Then a `ConfirmButton` "Suspend" with `danger`, whose prompt names the account and the end, for example "Suspend @alice for 7 days?". For an agent, the prompt adds "Its token is revoked, and unsuspending does not restore it."
     - The client checks what the server checks, and says it beside the field before asking: the reason, trimmed, is 1 to 500 code points (`[...s].length`), with no character matching `/\p{Cc}/u`, and a chosen end is in the future. The server's `validation-failed` is still shown.
     - Send `POST {url}/suspend` through `send`, with `{reason, until, hide_posts}` and `until` a number or null.
   - **Unsuspend** (status `suspended`): a "Restore posts hidden with this suspension" checkbox, unchecked, then `ConfirmButton` "Unsuspend", which sends `{restore_posts}`.
   - **Hide their posts** (status `suspended`): `ConfirmButton` "Hide their posts", prompt "Hide every visible post by @{handle} in forums you moderate?", sending `POST …/posts/hide` with no body.
   - **Restore hidden posts** (whenever `mayAct`): `ConfirmButton` "Restore hidden posts", prompt "Restore the posts a suspension of @{handle} hid, in forums you moderate?", sending `POST …/posts/restore` with no body. On success it says "Posts a suspension hid are restored, where you moderate." It says no count, because the server gives none.
   - **Answers.** After every answer, success or failure, the panel calls `onListChanged()` and awaits `refetch()`, so the row's status follows the account, including after a failed request that changed it. It chooses its words from what `refetch` resolved to:
     - **Which failures come after the commit** (read from main, Terva review 1523 on PR #181). `account.Service.Suspend` checks the reason and the end, looks the account up, and commits the status in `perm.ChangeStatusID`. What it refuses up to there leaves the account as it was: `validation-failed`, `not-found`, `forbidden` (`mayActOn`), and `conflict`, which covers `perm.ErrBadTransition` (already suspended, or pending), `perm.ErrLastAdmin` and `perm.ErrAnonymized` (`internal/problem/domain.go`). Only after the commit, with `hide_posts`, does it call `s.hide` and so `threads.Service.HidePostsOf`. That step fails in one of two ways. It answers 503 `unavailable`, coded from `threads.ErrSomeThreadsUnavailable` in `eachAuthorThread`, when some threads could not be reached after the rest were done. Or it answers 500 `internal` for an uncoded failure, such as an append error. Neither code is unique to the hide, though. `db.ErrBusy` and `context.DeadlineExceeded` map to `unavailable` and can come from the lookup or the commit, and a database failure in the commit is `internal`. So the code is necessary but not enough on its own.
     - **The finish words need both signs.** The answer is `unavailable` or `internal` to a suspend sent with `hide_posts`, the detail the panel rendered was not `suspended`, and the refreshed detail is `suspended`. Then the panel says "@{handle} is now suspended, but the request did not finish, so some posts may not be hidden. Press Hide their posts to finish." and offers Hide their posts. The words are true whichever step failed, and Hide their posts is idempotent, so pressing it is safe either way. A new problem code on the suspend route for "suspended, hide unfinished" lost: it is a server change for a case these words already cover.
     - **`conflict` never gets the finish words**, because a refused suspend committed nothing and hid nothing. When the refreshed detail is `suspended`, it says "@{handle} is already suspended, perhaps by someone else. What is shown now is current." Otherwise it says "This account cannot be suspended. It may be pending, anonymized, or the last active admin."
     - Any other failure, including `unavailable` or `internal` when the refreshed detail is not suspended, says the refusal's words (`send`'s sentence) and never mentions a hide.
     - When `refetch` resolves to `null`, the panel cannot tell what happened. It says the refusal's words and "Reload to see the account as it is now.", and never the finish words.
     - `unavailable` from Hide their posts on its own says "Some threads could not be reached. Press Hide their posts again to finish them."
     - `conflict` on unsuspend says "This account is no longer suspended."
     - Anything else gets `send`'s sentence.
3. **`screens/admin/UsersScreen.tsx`**:
   - Import `SuspensionPanel`.
   - In `UserDetail`, add `const refetch = async () => { try { const next = await apiGet<AdminUserDetail>(url); setFresh(next); return next; } catch (err) { if (isAbort(err)) throw err; return null; } };` and render `<SuspensionPanel d={d} url={url} refetch={refetch} onListChanged={onRoleChanged} />` directly after the facts `</dl>`.
   - Nothing else in the file changes. If TKT-01M3QH3GS2 has merged, keep its `update` helper and `admin-detail-head`. `refetch` sits beside them.
4. **CSS** in `screens/admin/admin.css`: `.admin-suspension` for the status line and the form, using the existing `admin-*` classes and tokens.
5. Run `go generate ./internal/web/spa` and commit `internal/web/spa/dist`. `npm run build` runs the budget, and the admin chunk is outside `threadViewGzipBytes`.

### Tests

- **Server:**
  - `TestAdminDetailCarriesTheSuspension` in `internal/web/api/admin_users_test.go`. Suspend an account with a reason and an end, then read the detail as an admin and as a moderator: both show `suspension` with that reason and end, and the answer is `no-store`. Once the account is unsuspended, `suspension` is null. A suspension with no end has `until` null. An active account has `suspension: null`.
  - The DTO golden `admin-user-suspended.moderator`, with `dto-contract.ts` matching it.
- **Client:** `screens/admin/SuspensionControls.test.tsx`, with `fetch` stubbed as `UsersScreen.test.tsx` does and the clock faked:
  - `suspends for 7 days`: after Confirm the body is exactly `{"reason":"spam","until":<now+7×86400000>,"hide_posts":false}`.
  - `suspends until lifted`: `until` is null.
  - `a chosen end`: the local moment in ms. A past moment is refused beside the field, and nothing is sent.
  - `the reason is checked`: nothing is sent, and the words sit beside the field, for an empty reason, a reason of spaces, 501 code points (`'😀'.repeat(501)`), and a tab. 500 emoji are accepted.
  - `hide their posts`: checking the box sends `hide_posts: true`, and the hint's words are shown.
  - `unsuspends with and without restoring`: the bodies are `{"restore_posts":true}` and `{"restore_posts":false}`.
  - `hides and restores on their own`: one POST each, with no body, only after Confirm. Hide is absent on an active account, and Restore is present on an active account that was never suspended.
  - `the status line`: "Suspended until 2026-10-07 14:00" and "Reason: …" for a detail with `suspension`, "Suspended until lifted" for `until: null`. A reason holding `<b>` is shown as text.
  - `a moderator acts on members only`: a moderator sees the controls on a member, and none on a moderator, an admin, or their own account.
  - `an admin acts on anyone but themselves`: an admin sees the controls on another admin, and none on their own account.
  - `no controls on a pending or anonymized account`: none for status `pending` or a `deleted-user-7` handle.
  - `a hide that stopped`: a suspend with `hide_posts`, from an `active` detail, answers 503 `unavailable`, and the refetched detail is `suspended`. The finish words show only after the refetch has resolved, Hide their posts is offered, and the list's row says `suspended`. The same holds for a 500 `internal`.
  - `already suspended is not a partial hide` (review 1523): a suspend with `hide_posts` answers 409, and the refetched detail is `suspended`, as when another moderator got there first. The already-suspended words show, with no finish words.
  - `a refused suspend is not a partial hide`: a suspend with `hide_posts` answers 409, and the refetched detail is still `active`, as for the last active admin. The cannot-be-suspended sentence shows, with no finish words and no Hide their posts.
  - `a busy database is not a partial hide`: a suspend with `hide_posts` answers 503, and the refetched detail is still `active`. Only `send`'s sentence shows.
  - `a refetch that fails`: the suspend answers 503 and the detail refetch fails. The refusal's words and "Reload to see the account as it is now." show, with no finish words.
  - `conflict on unsuspend`: a 409 on unsuspend shows "This account is no longer suspended."
  - `refetches and reloads`: after a success, and after a failure, the detail and the list are each requested again. `refetch` resolves to the detail it stored.
  - `an agent is warned`: the prompt for a `kind: 'agent'` account names the token.
- `UsersScreen.test.tsx` still passes, including `shows a moderator the list and the detail read-only…`, extended to find the suspension controls on a member.
- `TestTheAdminBundleIsSeparate` passes: nothing outside `screens/admin` imports the panel.
- **Invariants held by the server's tests, which this ticket must keep green and does not weaken:**
  - Status changes are audited in the transaction that makes them: `TestChangeStatusIsOneTransaction` (`internal/perm`) and `TestSuspensionAudited` (`internal/web/api`).
  - No role changes here. The screen changes status, never role, so no `user.promoted` or `user.demoted` row is written.
  - No suspended account gets a session: `TestChokepointRefusesSuspended` and `TestOneDoorIntoSessions`, whose allowed-caller lists this ticket does not change.
  - A hide moves the response cache key: `TestSuspensionHideMovesTheCacheKey`. The detail route is `no-store` and in no cache key, as `TestAdminDetailCarriesTheSuspension` checks.

### Out of scope

- Any change to the suspension routes, their rules, or the expiry job: TKT-01M29CP2QK, done.
- Editing the reason or end of a current suspension. Lift it and suspend again (TKT-01M29CP2QK, out of scope there too).
- Deactivating and anonymizing from this screen: TKT-01M29CKRFS's routes, with no control ticket.
- Suspending from the moderator queue: TKT-01M29CRJ4E links here.
- The agent controls and the rename control: TKT-01M3QH3GS2 and TKT-01M3P0CYR3.
- A count of hidden or restored posts. The routes answer 204, and the count is in the admin-only audit row.

## Notes

**agent:claude-code/phase2-groom** at 2026-09-30T00:29:01Z

Planned in Phase 2 grooming (TKT-01M3M9RR26), 2026-09-30, against main at 43a844d. Two things for whoever claims it. (1) This is not client-only: GET /admin/users/{handle} does not carry the suspension's reason or end, so the plan adds AdminUserDetail.suspension {reason, until}, read from users.status_reason and status_until, with a golden and a docs/07 line. (2) Restore hidden posts is offered on every account the viewer may act on, because a has-hidden-posts flag would have to be read from the admin-only audit log. The controls go in screens/admin/SuspensionPanel.tsx. UserDetail gains one import, a refetch helper and one element after the facts list, so this and TKT-01M3QH3GS2 (agent controls, PR #179) and TKT-01M3P0CYR3 (rename control) do not collide. It depends on TKT-01M3QH3GS2 because it builds on that PR's UserDetail, and only one lane may change the dist at a time. Effort raised to M for the server change.

**agent:claude-code/phase2-groom** at 2026-09-30T00:52:13Z

Terva review 1516 on PR #181 (head 9fc6307), finding 2 (medium), accepted. The panel now asks for the users list again after every answer, success or failure, not only after a success, so a suspend that committed before its hide failed no longer leaves the row saying active. Reloading only when the refetched detail differs lost: it would mean waiting for useResource's refetch. The plan, the test 'refetches and reloads' and criterion 8 are updated.

**agent:claude-code/phase2-groom** at 2026-09-30T01:05:58Z

Terva review 1520 on PR #181 (head 3395e26), one medium finding, accepted. The failed-hide words depended on a refreshed status that the planned refetch (res.retry, returning void) never gave back. refetch now fetches with apiGet, stores the detail, and resolves to it, or to null if the fetch fails. The panel awaits it before choosing its words. Tests are named for both failure cases, 'a hide that stopped' and 'a refused suspend is not a partial hide', plus 'a refetch that fails'. Criterion 6 is rewritten to match.

**agent:claude-code/phase2-groom** at 2026-09-30T01:18:10Z

Terva review 1523 on PR #181 (head f0c1913), one medium finding, accepted. A 409 can mean the account was already suspended, and the refreshed status is then suspended, so status alone no longer picks the finish-the-hide words. Read from main: every refusal before perm.ChangeStatusID commits leaves the account unchanged (validation-failed, not-found, forbidden, and conflict from ErrBadTransition, ErrLastAdmin or ErrAnonymized). The hide step after the commit answers 503 unavailable (threads.ErrSomeThreadsUnavailable) or 500 internal (an uncoded append failure). Neither code is unique to the hide: db.ErrBusy and context.DeadlineExceeded are also unavailable before the commit. So the finish words need both signs, a 503 or 500 and a change from not suspended to suspended, and they are worded to be true whichever step failed. A 409 with the account now suspended gets its own already-suspended words. New tests: 'already suspended is not a partial hide' and 'a busy database is not a partial hide'. Criterion 8 is rewritten.
