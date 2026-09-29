---
schema: 3
id: TKT-01M3QGH66WA5X7DBGZS8662S5T
title: Add lock, unlock, pin and unpin to the thread screen
type: task
status: ready
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
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-29T21:18:33Z
updated_at: 2026-09-29T22:49:17Z
created_by:
  id: agent:claude-code/phase2-run-ckrgs
  name: ""
updated_by:
  id: agent:claude-code/phase2-run-orchestrator
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

- [ ] The thread actions offer Lock or Unlock, and Pin or Unpin, by the header's state, only when viewer.can_moderate and the viewer's role is moderator or admin (threadActions.test.ts, ThreadScreen.test.tsx)
- [ ] Each action sends the body its route takes ({reason} or {}, {scope, until}, {scope} of the active pin), and the answer's header shows at once and invalidates the cached pages at the old log length (ThreadScreen.test.tsx, threads.test.ts)
- [ ] The thread head shows a locked thread's notice and an active pin's scope and end, a 409 keeps the panel open and refetches, and a member's composer is absent when can_reply is false (ThreadScreen.test.tsx)
- [ ] Nothing imports from screens/admin, the thread-view budget holds, and the dist regenerated with go generate ./internal/web/spa is committed

## Implementation plan

Groomed 2026-09-30 for Phase 2 (TKT-01M3M9RR26), against main at f04a476. The server half is merged: TKT-01M29CKRGS (Implement thread lock, unlock, pin, and unpin), PR #161. This ticket is its plan step 6.

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

### Approach

1. **Actions** (`web/client/src/shell/threadActions.ts`): the options gain `moderate?: { locked: boolean; pinned: Pinned | null; open: (kind: ModerateKind) => void }`, where `ModerateKind` is `'lock' | 'unlock' | 'pin' | 'unpin'`. When set, the list gains Lock or Unlock by `locked`, and Pin, or Unpin plus Pin (to change the pin), by `pinned`. The ids are `lock`, `unlock`, `pin` and `unpin`, after Reply and First unread. The desktop toolbar and the phone's more menu both render the list.
2. **ThreadScreen** (`web/client/src/screens/ThreadScreen.tsx`):
   - `mayModerate = res.data?.viewer.can_moderate && (role is moderator or admin)`, with the role read from `viewer.value.user?.role`.
   - A `moderating` state holds the open kind. `threadActions` gets `moderate` when `mayModerate` and the history route is not showing.
   - `head` is the newer header, as above.
   - The thread head shows "This thread is locked." (exists), and for an active pin a note: "Pinned in this forum" or "Pinned on every forum", followed by "until {date and time}" when `until` is set. Both notes are `<p>` elements with `data-thread-state`.
   - `ModerateDialog` renders under the head when `moderating` is set.
3. **`components/thread/ModerateDialog.tsx`**:
   - Lock and Unlock: an optional reason `<textarea maxlength=500>`, and the action's button.
   - Pin: a scope choice (radio, `forum` checked) and an optional expiry.
   - Unpin: "Unpin this thread?" and the button.
   - Each sends `apiSend<ThreadHeader>('POST', `/api/v1/admin/threads/${id}/${kind}`, body)`, where a lock or unlock with an empty reason sends `{}`. On success it calls `storeThreadHeader`, closes, and asks the page again.
   - A `conflict` says "This thread changed while you were working." It stores nothing, asks for the page again, and stays open so the moderator sees the new state. Other refusals show `problemMessage`'s words. While a request is in flight the buttons are disabled.
4. **`state/threads.ts`**: `storeThreadHeader(id, header)`, as above. It stores nothing for a header shorter than the entry, the same rule as `storeThreadPage`.
5. **CSS** in `components/thread/thread.css`, beside `.thread-head`.
6. Run `go generate ./internal/web/spa` and commit `internal/web/spa/dist`. `npm run build` checks the budget.

### Tests

- `shell/threadActions.test.ts` (new):
  - `offers no moderation without moderate`: without `moderate`, no lock, unlock, pin or unpin.
  - `lock or unlock by state`, and `pin, or unpin and pin, by state`.
- `screens/ThreadScreen.test.tsx`:
  - `a moderator sees the moderation actions`: `can_moderate: true` with role `moderator`. A member with `can_moderate: true` (a group grant) sees none, and neither does a member with `can_moderate: false`.
  - `lock sends the body and shows the lock at once`: Lock with the reason "off topic" sends `POST /api/v1/admin/threads/{id}/lock` with `{"reason":"off topic"}`. The answer's header (`locked: true`, a longer `log_length`) shows "This thread is locked." before the page is answered again, and the page is requested again.
  - `pin sends scope and until`: `{"scope":"global","until":<ms>}`, and `{"scope":"forum","until":null}` with no expiry. The pinned note shows the scope and the end.
  - `unpin sends the active scope`: `{"scope":"global"}` for a global pin.
  - `a conflict keeps the panel open`: a 409 shows the changed-meanwhile words and asks for the page again.
  - `a locked thread closes a member's composer`: a page with `locked: true` and `can_reply: false` renders no composer, and one with `can_moderate: true` and `can_reply: true` renders it.
- `state/threads.test.ts`: `storeThreadHeader` with a longer log length drops the entry's pages and forgets the held GETs. With a shorter one it changes nothing.
- The thread-view budget (`npm run build`) passes, and `TestTheAdminBundleIsSeparate` passes, because nothing imports from `screens/admin`.
- Server invariants are TKT-01M29CKRGS's and unchanged: `TestLockMovesTheResponseCacheKey`, `TestAuditRowsComeFromTheLog`.

### Out of scope

- Any server change, including whether a group-granted moderator may use these routes.
- Pinned threads in the thread list, which `ThreadRow.pinned` already carries: the thread list screen.
- Rename and move: TKT-01M29CKRH9.
- Archiving a thread.

## Notes

**agent:claude-code/phase2-groom** at 2026-09-29T22:07:20Z

Planned in Phase 2 grooming (TKT-01M3M9RR26), 2026-09-30. The routes are behind the staff gate, by role, before the service checks moderate in the forum. viewer.can_moderate is also true for a member granted moderate through a group, whom the gate refuses. The plan offers the actions only when can_moderate and the role is moderator or admin, which mirrors docs/07. Whether a group-granted moderator should be able to lock and pin is left to the design; the grooming report raises it.
