---
schema: 3
id: TKT-01M3QGH66WA5X7DBGZS8662S5T
title: Add lock, unlock, pin and unpin to the thread screen
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
  - TKT-01M29CKRGSF2FPY73MNNFPQ1FV
  - TKT-01M3RQP0TGP43KRWGW11XSN2T7
blocks_on: none
references:
  - ref: pr:224
    path: null
claim: null
archive: null
created_at: 2026-09-29T21:18:33Z
updated_at: 2026-09-30T12:04:04Z
created_by:
  id: agent:claude-code/phase2-run-ckrgs
  name: ""
updated_by:
  id: agent:claude-code/phase2-run-qgh66w
  name: ""
extensions: {}
---

## Description

Split from TKT-01M29CKRGS (Implement thread lock, unlock, pin, and unpin), whose plan step 6 was the client. The server lane could not change `internal/web/spa/dist` while the client lane held it (docs/13 "Running two lanes"), so the routes landed without a screen.

The routes are in docs/07 "Locking and pinning threads": `POST /api/v1/admin/threads/{id}/lock` and `/unlock` take `{reason?}`, `/pin` takes `{scope, until?}` (Unix ms), and `/unpin` takes `{scope}`. Each answers 200 with `ThreadHeader`, the thread page's header as it now reads, and needs no dto change.

What the groomed plan asked of the client:
- `web/client/src/shell/threadActions.ts` gains Lock/Unlock and Pin/Unpin when `viewer.can_moderate`.
- A small dialog, `components/thread/ModerateDialog.tsx`, takes a reason, or a scope and an optional expiry.
- The answer's header replaces the cached one.
- The thread head shows the existing "This thread is locked." and a pinned note.
- Run `go generate ./internal/web/spa` and commit `dist/`.

## Acceptance criteria

- [x] Each action sends the body its route takes ({reason} or {}, {scope, until}, {scope} of the active pin), and the answer's header shows at once and invalidates the cached pages at the old log length (ThreadScreen.test.tsx, threads.test.ts)
- [x] The thread head shows a locked thread's notice and an active pin's scope and end, a 409 keeps the panel open and refetches, and a member's composer is absent when can_reply is false (ThreadScreen.test.tsx)
- [x] Nothing imports from screens/admin, the thread-view budget holds, and the dist regenerated with go generate ./internal/web/spa is committed
- [x] The thread actions offer Lock or Unlock, and Pin or Unpin, by the header's state, whenever viewer.can_moderate is true, whatever the role, and never when it is false (threadActions.test.ts, ThreadScreen.test.tsx 'a moderator sees the moderation actions')
- [x] The global scope, Unpin of a global pin, and Pin over a global pin are offered only when the viewer's role is moderator or admin (threadActions.test.ts 'a global pin is staff's', ThreadScreen.test.tsx 'a group moderator is not offered a global pin')

## Implementation plan

Groomed 2026-09-30 for Phase 2 (TKT-01M3M9RR26), against main at f04a476. Refreshed on 2026-09-30 against main at a660ae5 (5124209 plus the run's tracking commits). The server half is merged: TKT-01M29CKRGS (Implement thread lock, unlock, pin, and unpin), PR #161. This ticket is its plan step 6. It now also depends on TKT-01M3RQP0TG (Let forum moderators by grant lock, pin, move and set slow mode), which moves the routes to the `moderates` gate.

### Settled in grooming

- **The routes** (docs/07 "Locking and pinning threads"): `POST /api/v1/admin/threads/{id}/lock` and `/unlock` take `{reason?}` or no body. `/pin` takes `{scope, until?}`, where `scope` is `forum` or `global` and `until` is Unix ms or null. `/unpin` takes `{scope}`, which must be the active pin's. Each answers 200 with `ThreadHeader` as the page now serves it, `no-store`. No DTO changes.
- **Who may act.** Each route is staff's by role (`staffGate` in `internal/web/api/admin.go`), and the threads service then requires `moderate` in the thread's forum. `viewer.can_moderate` on the page is the second check only. A member granted `moderate` through a group has `can_moderate` true and is refused by the gate. The client therefore offers the actions when `viewer.can_moderate` and the viewer's role is `moderator` or `admin`. This mirrors docs/07 and decides nothing new. Whether a group-granted forum moderator should lock and pin is a question for the design, not for this screen; the grooming report raises it.
- **Refusals, in order** (docs/07): the gate; `bad-request` for a body that is not the route's; `forbidden` without `moderate`; `conflict` on an archived thread; `conflict` for an action that changes nothing (locking a locked thread, unlocking an open one, unpinning a scope that is not the active pin's, pinning with the active pin's scope and `until`); `validation-failed` for a bad scope, an `until` not after now, or a reason over 500 characters.
- **Locking semantics** (docs/08 "Locking semantics"): a locked thread refuses members' replies and edits, and moderators may still post. The server already closes a member's composer: `viewer.can_reply` is false in a locked thread for anyone who does not moderate it (`View` in `internal/service/threads/view.go`), and `ThreadScreen` renders the composer only when `can_reply`.
- **Pin expiry needs no job**: a pin whose `until` has passed is null in the header (`header()` in `view.go`), so the client never compares the time itself.
- **Response cache**: each action appends a record, which moves the log length, which is in `respcache.Key` (`TestLockMovesTheResponseCacheKey`, TKT-01M29CKRGS). No new viewer class. On the client, the thread cache is keyed on log length too (`state/threads.ts`).
- **The reader bundle.** `threadActions.ts` and the thread screen are in the thread view, whose gzip budget is 60000 bytes (`web/client/budget.json`). Nothing here may import from `screens/admin/`, whose `ConfirmButton` and `Field` are in the admin chunk (`TestTheAdminBundleIsSeparate`). The built client is committed: run `go generate ./internal/web/spa` and commit `internal/web/spa/dist`.

Engineering choices made here, each with the alternative that lost:
- **The answer's header invalidates the cached pages.** The header's `log_length` is longer than the cached entry's. A new `storeThreadHeader(id, header)` in `state/threads.ts` replaces the entry with that length, the new header and no pages, and forgets the thread's held GETs, as `storeThreadPage` does for a longer page. `ThreadScreen` then asks for the page again with `res.retry`. Keeping the old pages under the new header lost: the cache's rule is that pages from two log lengths are never shown together.
- **`head` is the newer header.** `ThreadScreen` takes whichever of `res.data.thread` and `cachedThread(id).header` has the longer `log_length`, so the lock shows at once, before the page comes back.
- **An inline panel, not a modal `<dialog>`.** `components/thread/ModerateDialog.tsx` renders under the thread head as a labelled `role="group"` form, and focus moves to its first control and back to the thread head when it closes, as `ConfirmButton` does for the admin screens. A modal lost: happy-dom's `showModal` support cannot be relied on in the tests, and a modal needs focus trapping this panel does not.
- **Unpin needs no fields.** It sends the active pin's `scope` from the header. The panel only confirms.
- **The expiry is a `datetime-local` input** read as local time and sent as Unix ms. Empty sends `until: null`. Preset durations lost: docs/08 says "optional expiry", nothing more, and one input covers every case.


Added in the refresh of 2026-09-30:
- **Who may act, from 2026-09-30. This supersedes "Who may act" above.** Thread moderation follows the forum grant (maintainer decision 2026-09-30, docs/08 "Decided 2026-09-30"). After TKT-01M3RQP0TG the routes are behind `moderatesGate`, and the service decides `moderate` in the thread's forum at the log length the record lands at. So the client offers the actions on `viewer.can_moderate` alone, which is the same `perm.Can(moderate)` on the same forum, and a member granted `moderate` through a group is offered them. The role no longer enters the offer except for a global pin, below. docs/07 is corrected by TKT-01M3RQP0TG.
- **A global pin stays staff's by role** (TKT-01M3RQP0TG, engineering choice under the security rule, because a global pin heads every forum's list). The server refuses a viewer who is not a moderator or admin by role on three requests: a pin with scope `global`, an unpin of a global pin, and a pin that would replace an active global pin. The client offers each only when `viewer.value.user?.role` is `moderator` or `admin` (`staff` below), so it offers exactly what the server allows.
- **The thread screen as it stands now.** PR #202 (TKT-01M3QQWA5Z) added the flag item: `Post` takes `onFlag`, `mayFlag` decides who is offered it, a phone collapses link, flag and history under `PostMore`, and `ThreadBody` renders `FlagDialog`. `FlagDialog` is a modal built without `showModal`, using `trapTab` from `components/focus.ts`, and it returns focus to the control the post handed over. `apiSend` takes `init.status`, the one success status the caller accepts. The inline panel settled above still stands. It needs no focus trap, and it keeps the thread head, where the lock and pin notes change, in view.
- **The reader bundle is 47415 B of 60000 B gzip** on main after PR #202 (TKT-01M3NTSCJQ's merge log).

### Approach

1. **Actions** (`web/client/src/shell/threadActions.ts`): the options gain `moderate?: { locked: boolean; pinned: Pinned | null; staff: boolean; open: (kind: ModerateKind) => void }`, where `ModerateKind` is `'lock' | 'unlock' | 'pin' | 'unpin'`. When set, the list gains Lock or Unlock by `locked`. It gains Pin, or Unpin plus Pin (to change the pin), by `pinned`, except that when `pinned.scope` is `global` Unpin and Pin are offered only with `staff`. The ids are `lock`, `unlock`, `pin` and `unpin`, after Reply and First unread. The desktop toolbar and the phone's more menu both render the list. Rewrite the file comment's "/me carries no permission to gate the moderator ones on yet": the page's `viewer.can_moderate` gates them.
2. **ThreadScreen** (`web/client/src/screens/ThreadScreen.tsx`):
   - `mayModerate = !history && (res.data?.viewer.can_moderate ?? false)`, and `staff` is `viewer.value.user?.role` being `moderator` or `admin`.
   - A `moderating` state holds the open kind. `threadActions` gets `moderate` when `mayModerate`. The `useMemo` dependency list gains `mayModerate`, `staff`, `head.locked` and `head.pinned`.
   - `head` is the newer header, as settled above. Today it is `res.data?.thread ?? cachedThread(params.id)?.header`.
   - The thread head already shows `<p class="locked">This thread is locked.</p>`; it gains `data-thread-state="locked"`. For an active pin it gains a note: "Pinned in this forum" or "Pinned on every forum", followed by "until {date and time}" when `until` is set, as a `<p data-thread-state="pinned">`.
   - `ModerateDialog` renders under the head, above `ErrorPanel`, when `moderating` is set.
3. **`components/thread/ModerateDialog.tsx`**:
   - Lock and Unlock: an optional reason `<textarea maxlength=500>`, and the action's button.
   - Pin: a scope choice (radio, `forum` checked) and an optional expiry. The `global` radio is rendered only with `staff`.
   - Unpin: "Unpin this thread?" and the button.
   - Each sends `apiSend<ThreadHeader>('POST', `/api/v1/admin/threads/${id}/${kind}`, body, { status: 200 })`, where a lock or unlock with an empty reason sends `{}`. On success it calls `storeThreadHeader`, closes, and asks the page again.
   - A `conflict` says "This thread changed while you were working." It stores nothing, asks for the page again, and stays open so the moderator sees the new state. A `forbidden` says "You can no longer moderate this thread." and asks for the page again, because a grant revoked or a move since the page loaded is the likely cause. Other refusals show `problemMessage`'s words. While a request is in flight the buttons are disabled, and a synchronous in-flight ref stops a second press, as `FlagDialog` does.
4. **`state/threads.ts`**: `storeThreadHeader(id, header)`, as settled above. It stores nothing for a header shorter than the entry, the same rule as `storeThreadPage`.
5. **CSS** in `components/thread/thread.css`, beside `.thread-head`.
6. Run `go generate ./internal/web/spa` and commit `internal/web/spa/dist`. `npm run build` checks the budget. Record the new figure against 47415 B in the PR.

### Tests

- `shell/threadActions.test.ts` (new):
  - `offers no moderation without moderate`: without `moderate`, no lock, unlock, pin or unpin.
  - `lock or unlock by state`, and `pin, or unpin and pin, by state`.
  - `a global pin is staff's`: with `pinned.scope` `global` and `staff: false`, neither Unpin nor Pin is offered. With `staff: true`, both are.
- `screens/ThreadScreen.test.tsx`:
  - `a moderator sees the moderation actions`: `can_moderate: true` with role `moderator`. A member with `can_moderate: true` (a group grant) sees them too, and a member with `can_moderate: false` sees none.
  - `a group moderator is not offered a global pin`: a member with `can_moderate: true` opens Pin and sees no `global` radio. A moderator by role sees it.
  - `lock sends the body and shows the lock at once`: Lock with the reason "off topic" sends `POST /api/v1/admin/threads/{id}/lock` with `{"reason":"off topic"}`. The answer's header (`locked: true`, a longer `log_length`) shows "This thread is locked." before the page is answered again, and the page is requested again.
  - `pin sends scope and until`: `{"scope":"global","until":<ms>}`, and `{"scope":"forum","until":null}` with no expiry. The pinned note shows the scope and the end.
  - `unpin sends the active scope`: `{"scope":"global"}` for a global pin, as a moderator by role.
  - `a conflict keeps the panel open`: a 409 shows the changed-meanwhile words and asks for the page again. A 403 shows the no-longer words and asks for the page again.
  - `a locked thread closes a member's composer`: a page with `locked: true` and `can_reply: false` renders no composer, and one with `can_moderate: true` and `can_reply: true` renders it.
- `state/threads.test.ts`: `storeThreadHeader` with a longer log length drops the entry's pages and forgets the held GETs. With a shorter one it changes nothing.
- The thread-view budget (`npm run build`) passes, and `TestTheAdminBundleIsSeparate` passes, because nothing imports from `screens/admin`.
- Server invariants are TKT-01M29CKRGS's and TKT-01M3RQP0TG's and unchanged here: `TestLockMovesTheResponseCacheKey`, `TestAuditRowsComeFromTheLog`, `TestThreadModerationGate` and `TestGlobalPinStaysStaffs`.

### Out of scope

- Any server change. Who may use these routes, and the global-pin rule, are TKT-01M3RQP0TG's.
- Pinned threads in the thread list, which `ThreadRow.pinned` already carries: the thread list screen.
- Rename and move: TKT-01M3QXV399. Slow mode's dialog: TKT-01M3R0PM0K.
- Archiving a thread.


### Revised on claim (2026-09-30, main at f388921)

Read against main after #214 (moderatesGate, `staffPin`) and #218. The approach above stands, with these additions:
- **The panel never offers what the state no longer allows.** `threadActions.ts` exports `moderateKinds(state)`, the kinds offered for a header state, and both the actions and the panel read it. After a `conflict` or `forbidden` the page is asked for again. If the open kind is no longer among the kinds offered, for example Lock once another moderator has locked, Pin once a global pin is active for a group moderator, or any kind once `can_moderate` is false, the panel keeps its words and a Close button and drops its form. Keeping the form lost: its button would send a request the server refuses.
- **Order of the actions:** Reply, First unread, Lock or Unlock, Pin, Unpin, Copy link. Pin keeps its label when a pin is active, because it replaces the pin.
- **Focus:** the panel's first control takes focus when it opens. When it closes, focus goes to the thread's `h1`, which gets `tabIndex=-1`, because on a phone the item that opened the panel is gone. Escape closes the panel.
- **An e2e spec**, `e2e/thread-moderation.spec.ts`: mona, a moderator by role, locks and unlocks a side thread in the garden forum, then pins it in its forum and unpins it, against the real server at both widths. Each project leaves the thread as it found it, and no other spec reads that thread.

## Notes

**agent:claude-code/phase2-groom** at 2026-09-29T22:07:20Z

Planned in Phase 2 grooming (TKT-01M3M9RR26), 2026-09-30. The routes are behind the staff gate, by role, before the service checks moderate in the forum. viewer.can_moderate is also true for a member granted moderate through a group, whom the gate refuses. The plan offers the actions only when can_moderate and the role is moderator or admin, which mirrors docs/07. Whether a group-granted moderator should be able to lock and pin is left to the design; the grooming report raises it.

**agent:claude-code/phase2-groom** at 2026-09-30T09:17:52Z

Plan refreshed against main at a660ae5 on 2026-09-30. What changed: (1) By the maintainer decision of 2026-09-30 (docs/08) and TKT-01M3RQP0TG, the actions are offered on viewer.can_moderate alone. A group-granted moderator now gets them, and the role test is gone. Criterion 1 was replaced by criterion 4. The 'Who may act' item is carried verbatim and marked superseded. (2) A global pin stays staff's by role (TKT-01M3RQP0TG), so the global radio, and Unpin and Pin over a global pin, need a role of moderator or admin. New criterion 5. (3) The plan now names what #202 put on the thread screen (onFlag, mayFlag, PostMore, FlagDialog with trapTab, apiSend's status option), uses apiSend with status 200, adds the forbidden words, and uses the bundle figure of 47415 of 60000 B. (4) Out of scope no longer defers who may act to the design: that is TKT-01M3RQP0TG's.

**agent:claude-code/phase2-run-qgh66w** at 2026-09-30T11:15:20Z

Built on a local branch from main at f388921, not yet pushed: the client dist is held by another lane (PR #213), and docs/13 allows one open dist PR at a time. Criterion 3 stays unticked until the dist is regenerated and committed after replaying onto main.

Checks run locally: `npx tsc --noEmit -p .`; Vitest 49 files, 1515 tests; `npm run build`, whose reader bundle measured 49007 B gzip against 47415 B on main, +1592 B, 81.7% of 60000 B. Playwright passed on port 8797 with the new `e2e/thread-moderation.spec.ts` at both widths. The desktop smoke spec failed once in a full local run, on the /auth rate limit (a 429 on sign-in, 10 per burst and one every 6 s, internal/web/ratelimit.go). That spec signs in again, and this change adds no sign-in. The desktop project run alone then passed, 38 passed and 6 skipped.

Mutations tried against the tests are listed in the commit body. Every one was caught except removing the panel's thread check alone, which the reset on navigation also covers. Removing both is caught.

Two choices made beyond the plan and its "Revised on claim", recorded here:
- The pin panel sends scope `forum` whenever the viewer is not staff, even if the global radio was chosen before the role went. The test 'pins in the forum once the viewer is no longer staff' covers it.
- A refusal of type not-found, gone or unauthenticated also asks for the page again, as conflict and forbidden do, because each changes what the page may show.

**agent:claude-code/phase2-run-qgh66w** at 2026-09-30T11:34:09Z

The panel was tried as a lazy chunk and then dropped. At the coordinator's request, `ModerateDialog` was moved to `src/moderate/` and imported dynamically once `can_moderate` was true, and `scripts/bundle.mjs` stopped counting that chunk, as it already skips the admin screens. The reader bundle then measured 48872 B gzip, against 49007 B with the panel bundled statically and 47415 B on main, a saving of 135 B. The saving was small because Rolldown moved the modules the panel shares with the entry (preact, hooks, signals, the API client) into a chunk of their own, and the second gzip stream cost about 700 B. Rolldown's chunkOptimization variants, `preserveEntrySignatures: false`, and a codeSplitting group for the panel measured the same or larger. The coordinator judged 135 B not worth an extra chunk plus a change to what the budget counts. The unpushed commits were removed, and the panel stays eagerly loaded at +1592 B. `scripts/bundle.mjs` and docs/07 are as on main.

## Summary

The thread screen offers Lock or Unlock, Pin and Unpin on `viewer.can_moderate`, whatever the role, so a forum moderator by grant is offered them too. A global pin stays a moderator's or admin's by role. `moderateKinds` (`web/client/src/shell/threadActions.ts`) offers neither Pin nor Unpin over one to anyone else, and the pin panel offers the global scope only to staff. So the page offers exactly what the server's `staffPin` allows.

`components/thread/ModerateDialog.tsx` is an inline panel under the thread head. It sends each route's body: `{reason}` or `{}`, `{scope, until}`, and `{scope}` of the active pin. Its answer goes through `storeThreadHeader` (`state/threads.ts`), which replaces the cached entry at the new log length, drops the old pages and held GETs, and shows the header at once. The page is then asked for again. After a conflict or refusal, the panel keeps its words and asks for the page again. It drops its form once the action is no longer offered. The thread head shows the locked note and a pinned note with its scope and end.

The panel is loaded eagerly. A lazy chunk was measured and dropped (see the notes). The reader bundle is 49019 B gzip of 60000 B, +1593 B over main's 47426 B. There is no server change. New tests are in `threadActions.test.ts`, `ThreadScreen.test.tsx`, `threads.test.ts`, and the e2e `thread-moderation.spec.ts` at both widths.
