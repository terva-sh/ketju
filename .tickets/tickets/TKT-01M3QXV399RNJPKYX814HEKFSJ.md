---
schema: 3
id: TKT-01M3QXV399RNJPKYX814HEKFSJ
title: Add rename and move to the thread screen
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
  - TKT-01M29CKRH92JGZ1WY58JXC4T2G
  - TKT-01M3QGH66WA5X7DBGZS8662S5T
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-30T01:11:09Z
updated_at: 2026-09-30T04:49:40Z
created_by:
  id: agent:claude-code/phase2-run-krh9
  name: ""
updated_by:
  id: agent:claude-code/phase2-run-orchestrator
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

- [ ] The thread viewer block carries rename_until, the end of the viewer's rename window in Unix ms or null, and the page goldens and dto.ts follow
- [ ] The thread actions offer Rename to a moderator, and to the author while rename_until has not passed, and Move to a viewer with can_moderate whose role is moderator or admin
- [ ] After a rename or a move the screen shows the answer's header and replaces the URL with the canonical one, keeping ordinal, /history, page, lens and hash
- [ ] rename-window-closed, forbidden and conflict answers show their catalogue message and keep the dialog open
- [ ] The thread-view budget holds, and the dist regenerated with go generate ./internal/web/spa is committed
- [ ] rename_until is set only for the thread's author without moderate while authorRenameCheck passes and the window is open, and is null for a moderator, another member, an anonymous reader, and a window of 0 (TestRenameUntil); Rename and the viewer block both call authorRenameCheck
- [ ] rename_until is in the uncached viewer block: two viewers share one cached posts entry, and the window passing with no append turns it null and changes the ETag (TestRenameUntilIsInTheViewerBlock)
- [ ] The move list leaves out the current forum and lists archived forums only to an admin, and a move forgets the cached /api/v1/forums GETs (ThreadScreen.test.tsx)

## Implementation plan

Groomed 2026-09-30 for Phase 2 (TKT-01M3M9RR26), against main at 9a8fc76. The server half is TKT-01M29CKRH9 (Implement thread rename and move), PR #193 at 052db1d. Its summary and notes 3 to 5 were read for this plan.

On main the thread screen has no moderator controls at all. TKT-01M3QGH66W (Add lock, unlock, pin and unpin to the thread screen, ready) builds them: the `moderate` option of `threadActions`, the inline panel `components/thread/ModerateDialog.tsx`, `storeThreadHeader` in `state/threads.ts`, and the `mayModerate` test (`viewer.can_moderate` and a role of moderator or admin). This ticket extends that work and depends on it.

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

### Approach

1. **Server: `rename_until`.**
   - `internal/dto/dto.go`: `ThreadViewer` gains `RenameUntil *Millis` with `json:"rename_until"`, whose comment carries the rule above.
   - `internal/service/threads/moderate.go`: extract `authorRenameCheck` from `Rename`'s `!a.canModerate` branch, keeping the refusal order. Add `renameUntil(a access, archived bool, window time.Duration, now time.Time) *dto.Millis`, which returns nil unless `a.viewer.Member() && !a.canModerate && a.row.AuthorID == a.viewer.UserID && authorRenameCheck(...) == nil`, and otherwise `a.row.CreatedAt + window` in ms. The window comes from `renameWindow`, which is clamped (TKT-01M3R33SR5 replaces the clamp with a shared helper). A clamped window yields a far-future time, never an overflow.
   - `View` in `internal/service/threads/view.go`: read `renameWindow` only when the viewer is the thread's author and does not moderate, so no other page view pays for the read. Set `viewer.RenameUntil` inside `Threads.With`, beside `nextPostAt`, where `th.Archived()` is known.
   - Update the fixtures in `internal/dto/fixtures_test.go` so one case sets `rename_until`. Regenerate the goldens with `go test ./internal/dto -update` and `go test ./internal/web/api -update`: `thread.author.json` carries a time, and the other goldens carry null.
   - docs/07: add `rename_until` to "Thread response shape" and to the rename section.
2. **Client types**: `web/client/src/api/dto.ts` `ThreadViewer` gains `rename_until: Millis | null`. The fixtures that build a viewer by hand gain it.
3. **Actions** (`web/client/src/shell/threadActions.ts`):
   - The options gain `rename?: () => void`, which adds "Rename…" (id `rename`).
   - TKT-01M3QGH66W's `moderate` option gains `move: () => void`, which adds "Move…" (id `move`).
   - `ThreadScreen` passes `rename` when `viewer.can_moderate`, or when `rename_until !== null && Date.now() < rename_until`. A timer set for `rename_until` re-renders the actions when it passes. The history route passes neither.
4. **`components/thread/ModerateDialog.tsx`** gains two kinds.
   - `rename`: a text input labelled "Title", prefilled with the current title. Send is disabled while the trimmed title is empty, or has more than 200 code points (`[...t].length`, matching `checkTitle`'s rune count), or equals the current title. It sends `apiSend<ThreadHeader>('PATCH', `/api/v1/threads/${id}`, {title})`.
   - `move`: a `<select>` labelled "Move to", built from `apiGet<ForumTree>(forumsUrl)`. Children are indented under their parent, the current forum (`header.forum.slug`) is left out, and archived forums are listed only when the viewer's role is `admin`. It sends `apiSend<ThreadHeader>('POST', `/api/v1/admin/threads/${id}/move`, {forum})`. While the tree loads the select is disabled; a failed load shows `problemMessage` and a Retry.
   - On 200: `storeThreadHeader(id, header)`, close the panel, and `route(canonicalThreadUrl(location, header) ?? current, true)`. After a move, also `forgetGets((u) => u.startsWith('/api/v1/forums'))`, so the forum index and both forums' thread lists are fetched again.
   - Refusals keep the panel open with the typed value. `rename-window-closed` shows its catalogue message from `api/problem.ts`, and Rename is then withdrawn from the actions for this page. `forbidden` says "You cannot move this thread there." for a move and uses `problemMessage` for a rename. `conflict` says "This thread changed while you were working, or is already there." and asks for the page again. `validation-failed` says "Enter a title of 1 to 200 characters." `not-found` says "That forum is no longer there." for a move.
5. **CSS** in `components/thread/thread.css`, reusing TKT-01M3QGH66W's panel styles.
6. Run `go generate ./internal/web/spa`, commit `internal/web/spa/dist`, and confirm that `npm run build`'s budget line is under 60000 B.

### Tests

- **Server:**
  - `TestRenameUntil` (new, `internal/web/api/rename_test.go`):
    - the author inside the window gets `created_at + window`;
    - the author past it, in a locked thread, in an archived forum, and without `reply` gets null;
    - a moderator, whether or not they are the author, gets null;
    - another member and an anonymous reader get null;
    - with `threads.rename_window_seconds` at 0 the author gets null.
  - `TestRenameUntilIsInTheViewerBlock` (new): the author and a member read the same page. The posts come from one response cache entry (no second render), and only the author's viewer block carries `rename_until`. Moving the clock past the window, with no append, turns it null and changes the ETag. This is the response-cache test for the new per-viewer field.
  - `TestRenameWindow`, `TestRenameWindowTooLargeForADuration` and the refusal tests stay green through the extracted `authorRenameCheck`.
  - The `internal/dto` golden and fixture tests pass, and `web/client/src/contract.test.ts` passes.
- **Client:**
  - `shell/threadActions.test.ts`: Rename appears with `rename` and Move with `moderate.move`, and neither appears without its option.
  - `screens/ThreadScreen.test.tsx`:
    - `a moderator is offered rename and move`: role `moderator` with `can_moderate`.
    - `a group moderator is offered rename but not move`: role `member` with `can_moderate`.
    - `the author is offered rename while the window is open`: with `rename_until` 60 s ahead, Rename shows, and after `vi.advanceTimersByTime(60_000)` it is gone. With `rename_until: null` it never shows.
    - `rename sends the title and follows the new slug`: PATCH with `{"title":"New title"}`. The answer's header (new slug, longer `log_length`) shows at once, and the address is replaced with one carrying the new slug that keeps ordinal 42, the lens and `#post-42`.
    - `move lists the forums and follows the header`: the current forum is not listed and an archived forum is not listed to a moderator. The POST body is `{"forum":"lounge"}`. The header's forum shows, and the forum GETs are forgotten.
    - `an admin may move into an archived forum`: the archived forum is listed.
    - `refusals keep the panel open`: `rename-window-closed` (403), `forbidden` (403), `conflict` (409) and `validation-failed` (422) each show their words, and the typed value is still in the field.
  - The thread-view bundle stays within 60000 B gzip, and `TestTheAdminBundleIsSeparate` passes, because nothing imports from `screens/admin`.
- **Invariants:** no session is issued. The response cache key is unchanged, because `rename_until` is in the uncached viewer block (`TestRenameUntilIsInTheViewerBlock`). The rename and move records move the log length, which `TestRenameMovesTheResponseCacheKey` (TKT-01M29CKRH9) already holds.

### Out of scope

- Lock, unlock, pin and unpin, and the panel itself (TKT-01M3QGH66W).
- Slow mode's dialog (TKT-01M3R0PM0K).
- Retagging, which is Phase 4 (TKT-01M29CYSCP).
- Any change to who may move: the staff gate stays.

## Notes

**agent:claude-code/phase2-groom** at 2026-09-30T04:04:40Z

Groomed 2026-09-30 (TKT-01M3M9RR26). Now depends on TKT-01M3QGH66W (Add lock, unlock, pin and unpin to the thread screen): main has no moderator controls on the thread screen, and that ticket builds the panel, the moderate actions and storeThreadHeader this one extends. Settled as engineering choices: Rename is offered on can_moderate (PATCH is not staff-gated) and Move only with a staff role; rename_until comes from one authorRenameCheck shared with Rename; the destination is a select built from GET /api/v1/forums, not ForumPicker, whose choices are new-thread links.
