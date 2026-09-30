---
schema: 3
id: TKT-01M3QXV399RNJPKYX814HEKFSJ
title: Add rename and move to the thread screen
type: task
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area:moderation
  - area:client
assignees: []
milestone: phase-2
parent: TKT-01M29BN36JH4M1W5819D06PGPP
origin: null
dependencies:
  - TKT-01M29CKRH92JGZ1WY58JXC4T2G
  - TKT-01M3QGH66WA5X7DBGZS8662S5T
  - TKT-01M3RQP0TGP43KRWGW11XSN2T7
blocks_on: none
references:
  - ref: pr:231
    path: null
claim: null
archive: null
created_at: 2026-09-30T01:11:09Z
updated_at: 2026-09-30T13:33:20Z
created_by:
  id: agent:claude-code/phase2-run-krh9
  name: ""
updated_by:
  id: agent:claude-code/phase2-run-qxv399
  name: ""
extensions: {}
---

## Description

Split from TKT-01M29CKRH9 (Implement thread rename and move), whose plan step 5 was the client. The server lane could not change `internal/web/spa/dist` while the moderator-queue lane held it (docs/13 "Running two lanes"), so the routes landed with only the one client change the new problem code forced: the `rename-window-closed` message in `web/client/src/api/problem.ts`.

The routes are in docs/07 "Renaming and moving threads":
- `PATCH /api/v1/threads/{id}` takes `{title}`. The thread's author may rename it for `threads.rename_window_seconds` (default 86400) after starting it, and a moderator may at any time. Past the window the author gets `rename-window-closed`.
- `POST /api/v1/admin/threads/{id}/move` takes `{forum}`, the destination's slug. It is behind the staff gate and needs `moderate` in both forums. An archived destination is `conflict` except to an admin.
- Both answer 200 with `ThreadHeader`, `no-store`.

What the groomed plan asked of the client:
- Rename in the thread actions, for a moderator and for the author while the window is open. The window has to reach the client per viewer. The plan named a `rename_until` field (Unix ms, or null) in the thread viewer block, which is per viewer and never cached. That is a server change to `internal/dto` `ThreadViewer`, `View` in `internal/service/threads/view.go`, the page goldens, and `web/client/src/api/dto.ts`, and it belongs here.
- Move for a moderator whose role is moderator or admin, with a forum picker (`screens/ForumPicker.tsx`).
- After either action, navigate to the header's new canonical URL. `canonicalThreadUrl` already keeps the ordinal, `/history`, the page, the lens and the hash.
- Run `go generate ./internal/web/spa` and commit `dist/`.

## Acceptance criteria

- [x] The thread viewer block carries rename_until, the end of the viewer's rename window in Unix ms or null, and the page goldens and dto.ts follow
- [x] After a rename or a move the screen shows the answer's header and replaces the URL with the canonical one, keeping ordinal, /history, page, lens and hash
- [x] rename-window-closed, forbidden and conflict answers show their catalogue message and keep the dialog open
- [x] The thread-view budget holds, and the dist regenerated with go generate ./internal/web/spa is committed
- [x] rename_until is set only for the thread's author without moderate while authorRenameCheck passes and the window is open, and is null for a moderator, another member, an anonymous reader, and a window of 0 (TestRenameUntil); Rename and the viewer block both call authorRenameCheck
- [x] rename_until is in the uncached viewer block: two viewers share one cached posts entry, and the window passing with no append turns it null and changes the ETag (TestRenameUntilIsInTheViewerBlock)
- [x] The move list leaves out the current forum and lists archived forums only to an admin, and a move forgets the cached /api/v1/forums GETs (ThreadScreen.test.tsx)
- [x] The thread actions offer Rename whenever viewer.can_moderate is true, and to the author while rename_until has not passed, and Move whenever viewer.can_moderate is true, whatever the role (ThreadScreen.test.tsx 'a group moderator is offered rename and move')
- [x] The timer that withdraws Rename is set through delayUntil, never longer than 2147483647 ms, so a rename_until 40 days ahead leaves Rename offered (deadline.test.ts, ThreadScreen.test.tsx 'the author is offered rename while the window is open'), and TestRenameUntil covers a window of 10000000000 seconds

## Implementation plan

Groomed 2026-09-30 for Phase 2 (TKT-01M3M9RR26), against main at 9a8fc76. Refreshed on 2026-09-30 against main at a660ae5 (5124209 plus the run's tracking commits). The server half is TKT-01M29CKRH9 (Implement thread rename and move), PR #193 at 052db1d. Its summary and notes 3 to 5 were read for this plan.

On main the thread screen has no moderator controls at all. TKT-01M3QGH66W (Add lock, unlock, pin and unpin to the thread screen, ready) builds them: the `moderate` option of `threadActions`, the inline panel `components/thread/ModerateDialog.tsx`, `storeThreadHeader` in `state/threads.ts`, and the `mayModerate` test. After its refresh of 2026-09-30, `mayModerate` is `viewer.can_moderate` alone. This ticket extends that work and depends on it. It also depends on TKT-01M3RQP0TG (Let forum moderators by grant lock, pin, move and set slow mode), which moves the move route to the `moderates` gate.

### Settled in grooming

- **The routes are TKT-01M29CKRH9's** (docs/07 "Renaming and moving threads").
  - `PATCH /api/v1/threads/{id}` takes `{title}`. It is not behind the staff gate, because an author may rename their own thread.
  - `POST /api/v1/admin/threads/{id}/move` takes `{forum}`, the destination's slug, and is behind `staffGate`.
  - Both answer 200 `dto.ThreadHeader` with `no-store`, and are decoded strictly and bounded at 4 KiB.
  - Rename refusals, in order: `forbidden` to anyone but the author or a viewer with `moderate`; `conflict` on an archived thread. For the author without `moderate`: `conflict` in an archived forum or a locked thread, `forbidden` without `reply`, and `rename-window-closed` (403) past the window. Then `validation-failed` for a title `checkTitle` refuses (empty, or over `threads.MaxTitle`, 200 characters), and `conflict` for the title the thread already has.
  - Move refusals: `forbidden` without `moderate` in the source; `conflict` on an archived thread; `not-found` for a destination that is missing or hidden, and `forbidden` for one the mover cannot read; `forbidden` without `moderate` in the destination; `conflict` for an archived destination except to an admin; `conflict` for the current forum.
- **The author's rename window is 24 hours** (maintainer decision 2026-09-29, TKT-01M29CKRH9, docs/08 "Decided 2026-09-29"). It is `threads.rename_window_seconds`, default 86400, and a moderator may rename at any time.
- **Archived forums** (maintainer decision 2026-09-29, TKT-01M3N24RHD, docs/08): moderators may move a thread out of an archived forum, and moving one in stays admin-only.
- **A rename's audit row names no title** (maintainer decision 2026-09-29 on TKT-01M3NS147C, docs/03 "Hard redaction"). This ticket adds no audit row.
- **Who is offered what** (engineering choice, following the routes).
  - Rename is offered to a viewer with `can_moderate`, whatever their role, because PATCH is not staff-gated. It is also offered to the author while `rename_until` is in the future.
  - Move is offered only with `mayModerate` (TKT-01M3QGH66W), because its route is behind the staff gate. A member whose group grants `moderate` would be refused there.
- **`rename_until` is the server's answer, per viewer, in the viewer block** (from TKT-01M29CKRH9's groomed plan, carried here). It is Unix ms when the viewer is the thread's author, lacks `moderate`, and the author's other rename checks pass now: not archived, forum not read-only, not locked, `reply` held, and the window open. It is null otherwise, and so always null for a moderator. The viewer block is never cached (docs/07 "The response cache"), so no response cache key changes. The ETag hashes the viewer block, so a page whose window closed revalidates.
  - A helper `authorRenameCheck(a access, archived bool, window time.Duration, now time.Time) error` in `internal/service/threads/moderate.go` holds the author's checks. `Rename` and the viewer block both call it, so the offer and the refusal cannot disagree. Two alternatives lost. A client-side computation from `created_at` would need the window setting and the lock and permission rules on the client. A plain `can_rename` bool would leave the client no way to hide Rename when the window closes on an open tab.
- **The destination list is built from `GET /api/v1/forums`** (engineering choice). The tree carries no per-forum `moderate`, so the list holds every forum the viewer can see except the current one. Archived forums are listed only to an admin, and `pickable` in `screens/ForumPicker.tsx` drops them for anyone else. A destination the viewer cannot moderate is answered `forbidden`, and the panel says so.
  - Reusing `ForumPicker` as it is lost: each choice is a link to `/f/{slug}/new`, its title is fixed to "Start a thread in…", and it is a modal, while `ModerateDialog` is an inline panel.
  - Adding `can_moderate` to each forum in the tree lost: it is a server change to a shared response for one control, and the `forbidden` answer already covers it.
- **After either action the address follows the header at once.** `canonicalThreadUrl(location, header)` from `web/client/src/view.ts` gives the new URL, keeping the ordinal, `/history`, the page, the lens and the hash, and `route(to, true)` replaces it. The header goes through `storeThreadHeader` as a lock's does.
- **The built client is committed.** Run `go generate ./internal/web/spa` and commit `internal/web/spa/dist`. Only one lane may have a PR open that changes the dist (docs/13 "Running two lanes").


Added in the refresh of 2026-09-30:
- **Who is offered Move, from 2026-09-30. This supersedes the Move half of "Who is offered what" above**, and the Rename half stands. Thread moderation follows the forum grant (maintainer decision 2026-09-30, docs/08 "Decided 2026-09-30"). After TKT-01M3RQP0TG, `POST /api/v1/admin/threads/{id}/move` is behind `moderatesGate`, and the service still needs `moderate` in both forums. So Move is offered on `viewer.can_moderate`, whatever the role, which is the server's check on the source forum. The route bullet above that says the move is "behind `staffGate`" describes main before TKT-01M3RQP0TG.
- **The destination list is exact only for staff** (engineering choice). A moderator by role moderates every forum they can see (docs/04 "permissions"), and an admin every forum, so for them the list built from `GET /api/v1/forums` holds only forums the move can reach, apart from the archived rule. A group moderator may moderate only some of the listed forums, and the server answers `forbidden` for the rest, which the panel words as "You cannot move this thread there." That keeps the list correct for the common case without a server change. Two alternatives lost:
  - Carrying the moderated forums' slugs in `GET /me`, which already calls `perm.ModeratedForums`. That is a DTO change to the one response every page loads, for one picker.
  - Filtering the list by `moderate` per forum. That is the per-forum `can_moderate` the settled item above already rejected.
- **Long deadlines** (engineering choice, from TKT-01M3R33SR5 (Bound the post edit and delete windows before making Durations)). `threads.rename_window_seconds` has no upper bound. `renameWindow` clamps it to the longest `time.Duration`, so `rename_until` can be up to about 292 years ahead. That is still under 2^53 ms, so it is an exact JSON number. A browser fires a `setTimeout` longer than 2^31 − 1 ms (about 24.8 days) at once. So the timer that withdraws Rename is set for at most that long, and when it fires early it sets itself again. The helper is `delayUntil(at: number, now: number): number` in `web/client/src/components/thread/deadline.ts`, with the bound `maxTimeout = 2_147_483_647`. Whichever of this ticket and TKT-01M3PTT97X (Offer edit and delete only inside the author windows in the client) lands first adds it, and the second imports it.
- **The thread screen as it stands now.** PR #202 (TKT-01M3QQWA5Z) added `onFlag`, `mayFlag`, `PostMore`, `FlagDialog`, and `apiSend`'s `init.status`. PR #205 (TKT-01M3QJYV4Z) added the post source route. Neither touches the thread head or `threadActions`. `forumsUrl` is exported by `screens/ForumIndex.tsx`, which is already in the main bundle. The reader bundle is 47415 B of 60000 B gzip on main.


Revised on claim, 2026-09-30, against main at de1771d (TKT-01M3QGH66W's PR #224 merged, and TKT-01M3RQP0TG's #214 and TKT-01M3R33SR5's #218 before it). Everything under "Settled in grooming" above is carried unchanged. What the code as it stands changed in the approach is marked **(revised)**.

### Approach

1. **Server: `rename_until`.**
   - `internal/dto/dto.go`: `ThreadViewer` gains `RenameUntil *Millis` with `json:"rename_until"`, whose comment carries the rule.
   - `internal/service/threads/moderate.go`: `authorRenameCheck(a access, archived bool, window time.Duration, now time.Time) error` holds the author's checks in Rename's order (archived thread, `readOnlyForum`, locked, `reply`, window). `Rename` calls it for a viewer without `moderate`, and keeps the archived-thread conflict for a moderator, so the refusal order is unchanged. `renameUntil(a, archived, window, now) *dto.Millis` is `a.row.CreatedAt + window.Milliseconds()` when `mayOwnRename(a)` (a member, the thread's author, without `moderate`) and `authorRenameCheck` passes, and nil otherwise. **(revised)** `mayOwnRename` is shared by `renameUntil` and `View`, so the setting is read only for the one viewer it can matter to. The window is `renameWindow`'s, which on main already goes through `secondsDuration`.
   - `View` in `internal/service/threads/view.go` reads `renameWindow` only when `mayOwnRename(a)`, and sets `viewer.RenameUntil` inside `Threads.With`, where `th.Archived()` is known.
   - `internal/dto/fixtures_test.go`: the member fixture carries `rename_until`. Goldens regenerated in `internal/dto/testdata/golden` and `internal/web/api/testdata/golden` (only `thread.author.json` carries a time).
   - docs/07: `rename_until` in "Renaming and moving threads" and in "Thread response shape".
2. **Client types**: `web/client/src/api/dto.ts` `ThreadViewer` gains `rename_until: Millis | null`. No fixture builds a viewer by hand; `threadPage` in `testing.tsx` reads the member golden.
3. **Actions** (`web/client/src/shell/threadActions.ts`):
   - `PanelKind` is `ModerateKind | 'rename' | 'move'`.
   - The options gain `rename?: () => void`, which adds "Rename…" (id `rename`) after Reply and First unread.
   - **(revised)** The `moderate` option's `open` takes a `PanelKind`, and Move ("Move…", id `move`) is added after the lock and pin kinds whenever `moderate` is set, calling `open('move')`. A separate `move` callback would be set exactly when `moderate` is, so it carried nothing.
4. **`ThreadScreen`**:
   - Rename is offered when `viewer.can_moderate`, or while `rename_until` is in the future. A timer set with `delayUntil(rename_until, Date.now())` re-renders the screen, and is set again while the window is still open, so a timer that fired early (40 days ahead) sets itself again. The history route offers neither.
   - **(revised)** A `rename-window-closed` answer withdraws Rename from the actions for this thread at once (`closedOn`) and does not ask for the page again, so the panel keeps the typed title beside the catalogue message. Asking for the page again would drop the form, because the page's `rename_until` would be null.
   - **(revised)** The effect that replaces a stale slug in the address now compares with the header shown (`head`, the newer of the page's and the cache's by log length), not with `res.data.thread`. Otherwise the page answered before a rename would put the old slug back as soon as the rename replaced the address, until the page was answered again.
5. **`components/thread/ModerateDialog.tsx`** gains two kinds.
   - `rename`: a text input labelled "Title", prefilled with the current title, with the hint "Up to 200 characters." Send is disabled while the trimmed title is empty, has more than 200 code points (`[...t].length`, matching `checkTitle`'s rune count), or equals the current title. It sends `PATCH /api/v1/threads/{id}` with `{title}` trimmed.
   - `move`: a `<select>` labelled "Move to", built from `apiGet<ForumTree>(forumsUrl)` by `destinations(forums, current, admin)`: every listed forum but the current one, children after their parent and indented with an em space, archived forums only to an admin. The tree is `GET /api/v1/forums`, which lists only forums the viewer may read, so the list shows no forum the forum index would not. While it loads the select is disabled; a failed load shows `problemMessage` and a Retry. It sends `POST /api/v1/admin/threads/{id}/move` with `{forum}`.
   - On 200: `storeThreadHeader(id, header)`; after a move `forgetGets((u) => u.startsWith(forumsUrl))`, so the forum index and every thread list are fetched again; `route(canonicalThreadUrl(location, header), true)` when the slug changed; then the page is asked for again and the panel closes.
   - Refusals keep the panel open with the typed value (`moderateRefusal`): `rename-window-closed` its catalogue message; `forbidden` "You cannot move this thread there." for a move and the catalogue message for a rename; `conflict` "This thread changed while you were working, or is already there."; `validation-failed` "Enter a title of 1 to 200 characters."; `not-found` "That forum is no longer there." for a move. Conflict and the access refusals ask for the page again, as for lock and pin.
6. **CSS**: `.moderate-field select` joins the panel's input styles in `components/thread/thread.css`.
7. `components/thread/deadline.ts` adds `delayUntil` and `maxTimeout`, since TKT-01M3PTT97X has not landed. If it lands first, this ticket imports its file instead.
8. `go generate ./internal/web/spa` and commit `internal/web/spa/dist` once this lane holds the dist (docs/13 "Running two lanes"). Record the bundle change in the PR: +868 B (49019 B to 49887 B gzip) measured on de1771d.

### Tests

- **Server** (`internal/web/api/renameuntil_test.go`):
  - `TestRenameUntil`: the author inside the window gets `created_at + 86400000`, and a millisecond before the end; null at the end, where the author's rename is `rename-window-closed`; null for an anonymous reader, another member, a moderator by role, a group moderator, a moderator by role or group who is the author, and an inactive author; null in a locked thread, an archived forum, an archived thread, and without `reply` (where the rename is `forbidden`); null with the window at 0; and `created_at + math.MaxInt64/1e6` ms with the window at 10000000000 seconds.
  - `TestRenameUntilIsInTheViewerBlock`: the author and another member share one response cache insert and one hit, and only the author's viewer block carries `rename_until`; moving the clock to the end with no append serves null from the same entry and changes the ETag.
  - The rename and move tests of TKT-01M29CKRH9 stay green through the extracted `authorRenameCheck`, and the goldens and `web/client/src/api/contract.test.ts` pass.
- **Client:**
  - `shell/threadActions.test.ts`: Rename with `rename`, Move with `moderate`, neither without.
  - `components/thread/deadline.test.ts`: `at - now` below the bound, the bound 30 days ahead and at the longest window, 0 once passed.
  - `screens/ThreadScreen.test.tsx`: a moderator, and a group moderator, are offered rename and move, and a member without `can_moderate` or a window neither; the author is offered Rename while the window is open and not after it closes, never with `rename_until: null`, and still after `vi.advanceTimersByTime(2_147_483_647)` with the window 40 days ahead, with no `setTimeout` longer than 2147483647 ms; rename sends the trimmed title, disables Send for the same, an empty and a 201-character title, shows the new title at once and replaces the address keeping ordinal 61, the lens and `#post-61`; move lists the forums without the current one or an archived one, sends `{forum}`, shows the new forum and forgets the forum GETs; an admin sees archived forums; conflict, validation-failed, forbidden, not-found and rename-window-closed each keep the panel open with their words and the typed value, and rename-window-closed withdraws Rename.
  - `e2e/thread-rename-move.spec.ts`, at 390 px and 1440 px, with mona's stored session: rename and rename back, with the address following; move to the reading group and back; the move panel passes axe in each theme, one scan per test (#223).
- **Invariants:** no session is issued. The response cache key is unchanged (`TestRenameUntilIsInTheViewerBlock`). No route, audit action or permission changes.

### Out of scope

- Lock, unlock, pin and unpin, and the panel itself (TKT-01M3QGH66W).
- Slow mode's dialog (TKT-01M3R0PM0K).
- Retagging, which is Phase 4 (TKT-01M29CYSCP).
- Any change to who may move, and to the move route's gate: TKT-01M3RQP0TG.
- The `secondsDuration` helper and the other settings' clamps: TKT-01M3R33SR5.

## Notes

**agent:claude-code/phase2-groom** at 2026-09-30T04:04:40Z

Groomed 2026-09-30 (TKT-01M3M9RR26). Now depends on TKT-01M3QGH66W (Add lock, unlock, pin and unpin to the thread screen): main has no moderator controls on the thread screen, and that ticket builds the panel, the moderate actions and storeThreadHeader this one extends. Settled as engineering choices: Rename is offered on can_moderate (PATCH is not staff-gated) and Move only with a staff role; rename_until comes from one authorRenameCheck shared with Rename; the destination is a select built from GET /api/v1/forums, not ForumPicker, whose choices are new-thread links.

**agent:claude-code/phase2-groom** at 2026-09-30T09:18:54Z

Plan refreshed against main at a660ae5 on 2026-09-30. What changed: (1) Move is offered on viewer.can_moderate alone, by the maintainer decision of 2026-09-30 (docs/08) and TKT-01M3RQP0TG, which puts the move route behind the moderates gate. The Move half of 'Who is offered what' is carried verbatim and marked superseded. Criterion 2 was replaced by criterion 8, and the test is now 'a group moderator is offered rename and move'. (2) Engineering choice: the destination list stays built from GET /api/v1/forums. It is exact for staff, and a group moderator gets 'You cannot move this thread there.' for a forum they do not moderate. Carrying moderated slugs in /me lost as a change to every page load for one picker. (3) For TKT-01M3R33SR5's clamp, rename_until can be about 292 years ahead, and a setTimeout past 2^31-1 ms fires at once. So the timer goes through delayUntil in components/thread/deadline.ts, which this ticket or TKT-01M3PTT97X adds, whichever lands first. New criterion 9. (4) Corrected names: the contract test is web/client/src/api/contract.test.ts, the golden list names all five thread goldens, forumsUrl comes from screens/ForumIndex.tsx, apiSend is called with status 200, and the bundle figure is 47415 of 60000 B.

**agent:claude-code/phase2-run-qxv399** at 2026-09-30T12:36:53Z

Built and tested on a local branch, run/qxv399-rename-move, from main at de1771d, without the dist: two client lanes (reply autosave, then suspension controls) hold it first (docs/13 'Running two lanes'). Green locally: gofmt, go build, go vet, go test -race ./..., npx tsc --noEmit, vitest (52 files, 1577 tests), and the full Playwright suite (102 passed, 10 skipped) on the regenerated client. The reader bundle measured 49887 B gzip against 49019 B on de1771d, +868 B. Nothing is pushed. When the dist is free the commits are replayed onto origin/main, the client regenerated, and every check run again before the PR opens; the criteria are ticked then.

**agent:claude-code/phase2-run-qxv399** at 2026-09-30T13:33:20Z

Terva review 1588 on PR #231 (head 8b149cd, base 6d1e9ab, run 1499), finding 1, medium, 'Keep the rename form when a refusal changes the viewer's permissions': fixed in the next commit. A refused rename or move keeps its form after the fresh page withdraws the action, with Send disabled and Close focused, so the typed title stays. New test: ThreadScreen.test.tsx "a refused author's rename keeps the title when the window is withdrawn". Reader bundle 52105 B gzip.

## Summary

The thread screen renames and moves threads. The server serves rename_until in the per-viewer block: the author's window end in Unix ms while authorRenameCheck, which Rename also calls, passes, and null for everyone else. The client offers Rename on can_moderate and to the author inside that window, with the withdrawing timer bounded through delayUntil (components/thread/deadline.ts, new here), and Move on can_moderate, built on the lock and pin panel. Move lists the forums GET /api/v1/forums serves less the thread's own and, except to an admin, the archived ones. After either action the answer's header shows at once and the address takes its canonical slug; the slug check now reads the header shown, so the page answered before a rename does not undo it. Reader bundle 52095 B gzip, +884 B on main at 6d1e9ab. Tests: TestRenameUntil, TestRenameUntilIsInTheViewerBlock, ThreadScreen, threadActions and deadline unit tests, and e2e/thread-rename-move.spec.ts at both widths. All criteria met.
