---
schema: 3
id: TKT-01M3NS19315QTWBKNZN782VRF1
title: Autosave reply drafts from the composer
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:client
  - effort:S
assignees: []
milestone: phase-2
parent: TKT-01M29BN367F3YQZESM63VN26GP
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-29T05:08:40Z
updated_at: 2026-09-29T15:11:15Z
created_by:
  id: agent:claude-code/phase2-groom
  name: ""
updated_by:
  id: agent:claude-code/phase2-groom
  name: ""
extensions: {}
---

## Description

docs/06 says "Drafts autosave to the server (`drafts` table, one per user+thread)" and that the phone's reply bar shows "the first line of any draft". The server side landed with TKT-01M29CA9CE as `GET/PUT/DELETE /api/v1/drafts/{thread_id}`. The client never calls it. `composer/Composer.tsx:33` still says "Saving the draft is TKT-01M29CA9CE's; the source lasts the session", and `state/composer.ts` holds the source in memory only.

No ticket covers the client half. Found while planning TKT-01M3K9XBJA (Autosave new-thread drafts), whose new-thread counterpart is filed separately.

### What to build
- When the composer opens on a thread, load the draft and fill in the source and reply target.
- Save on a debounce while typing, the way `usePreview` debounces the preview (`Composer.tsx:306-325`), and on leaving the page.
- Delete the draft after a successful post.
- The reply bar shows the draft's first line.
- Rebuild the dist (`go generate ./internal/web/spa`) and stay within the 60000 B gzip budget.

## Acceptance criteria

- [ ] A reply and its reply target typed in one browser context are restored in a second context signed in as the same member, in the composer at 1440 px and in the reply bar at 390 px (e2e/drafts.spec.ts)
- [ ] A loaded draft fills only an empty composer with no reply target and no post in flight, and loading sends no PUT (state/composer.test.ts)
- [ ] Saves are debounced to one PUT per 1500 ms pause, nothing is sent unchanged, a blank composer is a DELETE, and requests for one draft reach the server in order (state/drafts.test.ts)
- [ ] A successful post deletes the draft and cancels any pending save, and afterwards GET /api/v1/drafts/{id} is 204 (state/drafts.test.ts, e2e/drafts.spec.ts)
- [ ] The dist is regenerated with go generate ./internal/web/spa and committed, and npm run budget stays within 60000 B gzip
- [ ] A forbidden or conflict answer turns autosave off for that thread (state/drafts.test.ts)
- [ ] On pagehide the save starts inside the handler when no request for that draft is in flight; with one in flight no second request starts, and the snapshot kept in localStorage is sent on the next load when it is newer than the server draft (state/drafts.test.ts)
- [ ] Autosave for a draft is disarmed until its GET answers, so a load slower than the debounce never deletes the server draft; an empty composer sends DELETE only when the server is known to hold a non-empty draft, and a failed load never deletes (state/drafts.test.ts)

## Implementation plan

Groomed 2026-09-29 for the Phase 2 autonomous run (TKT-01M3M9RR26), against main at 9425310.

What exists there:
- `GET/PUT/DELETE /api/v1/drafts/{thread_id}` (TKT-01M29CA9CE), answering `dto.Draft{thread_id, source, reply_to, updated_at}` or 204. The type is in `web/client/src/api/dto.ts`.
- `state/composer.ts` holds each thread's composer in memory (`composerFor`): `source`, `replyTo`, `caret`, `posting`, `failure`. `postReply` and `discardReply` clear it. `quotePost`, `replyToPost` and `replyToThread` write it. The owner check empties every composer when the signed-in handle changes.
- `composer/Composer.tsx:33` still says the draft is not saved. `ReplyBar.tsx` shows `firstLine(c.source.value)`, so it shows a draft once `source` holds it.
- `api/client.ts`'s `apiGet` treats any `ok` answer as JSON, so a 204 from the drafts route would surface as an `internal` problem.
- `state/readTracker.ts` already sends on `pagehide` with `fetch` and `keepalive`.

### Settled in grooming

- **Drafts autosave to the server**, one per user and thread, so a half-written reply survives a crash and appears on another device (docs/06 "Composer"; docs/04 `drafts`).
- **The phone's reply bar shows the reply-to chip and the first line of any draft** (docs/06 "Mobile").
- **The API's rules** (docs/07 "Drafts"): `GET` is 200 or 204; `PUT {source, reply_to?}` replaces the viewer's one draft whichever device sent it; `DELETE` is 204 whether or not there was one; saving needs `reply` in the forum; all three are `no-store`, and `PUT` and `DELETE` are charged to the write limiter.
- **Nothing changes the composer while a post is in flight** (reviews on PR #81, `state/composer.ts`). A draft load or save must keep that rule.
- **The built client is committed.** Run `go generate ./internal/web/spa` after any client change, and `npm run budget` stays within 60000 B gzip.

Engineering decisions taken here, with the alternative that lost:
- **One autosave module for both kinds of draft**, `state/drafts.ts`, keyed `reply:<thread>` or `new:<forum>`. TKT-01M3NS16VH (Build the new-thread screen with draft autosave) reuses it for the forum draft, and depends on this ticket. Lost: autosave inside `Composer.tsx`, which the new-thread screen would copy, and which stops when the phone's sheet unmounts.
- **Saves are driven by the state, not the textarea.** A signals `effect` on each composer's `source` and `replyTo` schedules the save, so a quote, a reply chip, a discard and typing are all saved the same way. Lost: calls from each handler, where the next handler added would forget one.
- **Debounce of 1500 ms.** The preview already spends one write-limiter token per 400 ms pause (2 per second, burst 60, `internal/web/ratelimit.go`), and saving on each preview would double that.
- **Leaving the page never waits on a promise, and never sends two requests for one draft at once** (Terva review 1372, PR #143, head 6d6aeea: a flush queued behind an in-flight `PUT` may never start before the page is torn down). Three rules together:
  1. Every draft request is sent with `keepalive: true` when its body is under 64 KiB, the browser's `keepalive` limit, so a request already in flight survives the unload.
  2. On `pagehide`, for each draft whose snapshot differs from the last one sent, the handler runs synchronously. If no request for that draft is in flight, it calls `fetch` with `keepalive` at once, inside the handler.
  3. If one is in flight, sending a second could overtake it, because the server gives two requests for one draft no order, and the older text would win. So the handler writes the unsent snapshot to `localStorage` under `ketju.draft.<handle>.<key>` instead, with the time. The handle is the owner `composerFor` already uses, since `/me` carries no user id. The next `loadReplyDraft` for that key in this browser compares it with the server draft's `updated_at`. When the local copy is newer, it fills the composer from it, sends it through the queue, and removes it. A source of 64 KiB or more takes the same path. Signing out and an owner change clear every `ketju.draft.*` entry, so the next person to use the browser never sees it.

  What is lost is bounded: the last edit made while a save was in flight reaches another device only after this browser next opens the thread. Lost: waiting on the chain in `pagehide`, the flaw the review found; aborting the in-flight request and sending the final one, since an abort does not unsend a body the server may still apply after the newer one; and a client revision the server compares, which changes the drafts API (TKT-01M29CA9CE, done) and its schema for a window of about 1.5 s.
- **A loaded draft never overwrites what the reader typed.** It fills the composer only if the source is empty, no reply target is set, and no post is in flight. Otherwise the local text wins and is saved over the server copy on its next save.
- **Empty means delete, but only a draft the server is known to hold.** When the source is blank and there is no reply target, the save is a `DELETE`, but only when the last server state this tab knows of is non-empty: a draft that was loaded or saved non-empty. An untouched composer, or one whose server state is unknown, sends nothing when it is empty.
- **Autosave is disarmed until the draft has loaded** (Terva review 1376, PR #143, head d2e5269: a load slower than the debounce let the initial empty composer `DELETE` the draft it was about to show). `watch` creates a key disarmed. While a key is disarmed, changes schedule nothing, and `pagehide` sends nothing for it. The load arms it when the `GET` answers 200 or 204, after filling the composer, and records what it found as the known server state. If the member typed in the meantime, arming schedules one save of what they typed; the local text wins, as settled above. If the load fails, the key is armed with the server state unknown. A non-empty save may then go out, since `PUT` replaces the whole draft with what the member wrote, but an empty composer sends no `DELETE` until a later load or save makes the state known. Lost: delaying the first save by a fixed time, which a slow network still beats; and skipping the load when the composer is non-empty, which leaves the server state unknown.
- **The draft is loaded by the thread page, not by the composer**, when `viewer.can_reply` is true, once per thread per session. The reply bar then shows it on a phone before the sheet ever opens. Lost: putting the draft into the thread response's viewer block, which would save a request but change the thread endpoint's contract and its goldens.
- **`apiGetOptional<T>(url, init): Promise<T | null>`** in `api/client.ts` answers `null` for 204 and holds nothing, since drafts are `no-store`. It lives in `client.ts` so `one-path.test.ts` still finds every request there.

### Approach

1. `api/client.ts`: `apiGetOptional`, through `request` and `toProblem` like the rest.
2. `state/drafts.ts`:
   - `type DraftKey = { kind: 'reply'; thread: string } | { kind: 'new'; forum: string }` and the URL for each (`/api/v1/drafts/{thread}`, `/api/v1/forums/{slug}/draft`).
   - Per key: the last saved snapshot, a timer, a `status` signal (`idle | saving | saved | failed | off`), and one promise chain, so a `PUT` in flight is always followed, never overtaken, by a later `PUT` or `DELETE`.
   - `watch(key, read: () => Snapshot)`: the `effect` that schedules a save `draftDelay` (1500) ms after the snapshot last changed. It sends nothing when the snapshot equals the last saved one.
   - `drop(key)`: cancels the timer, marks the empty snapshot as saved, and queues a `DELETE`.
   - Each key records whether a request is in flight. `unload()` on `pagehide`, registered once, applies the three rules above synchronously. It starts at most one `keepalive` fetch per key and awaits nothing. `restoreLocal(key, serverUpdatedAt)` is the load-time half.
   - Refusals: `forbidden`, `conflict` (a locked or archived thread), `not-found` and `unauthenticated` turn autosave off for the key; `rate-limited` waits for `Retry-After`; anything else is `failed` and the next change retries.
   - `resetDrafts()` for tests. An owner change in `composerFor` calls it, so one person's pending save never goes out under the next person's session.
3. `state/composer.ts`:
   - `composerFor` starts `watch({kind: 'reply', thread}, () => ({source, reply_to}))`, disarmed, for each new composer. A thread the reader cannot reply in is never loaded, so its key is never armed and never saves.
   - `loadReplyDraft(thread)`: `apiGetOptional<Draft>`, then fills `source` and `replyTo` under the rule above and records the loaded snapshot as saved and as the known server state, then arms the key (`arm(key, known)`), so loading does not save it back.
   - `postReply` calls `drop` on success, before it clears the fields. `discardReply` calls `drop`.
   - Update the header comment that says drafts last the session.
4. `screens/ThreadScreen.tsx`: an effect on the thread id and `canReply` calls `loadReplyDraft` once per thread per session.
5. `composer/Composer.tsx`: the footer's "Markdown supported" gains a `role="status"` line from the key's `status`: "Draft saved", "Saving…", or "Draft not saved". Nothing is shown while autosave is off. Update the comment at line 33.
6. Build: `npm run build`, `go generate ./internal/web/spa`, commit the dist, and record `npm run budget`'s figure in a note.

### Tests

- `api/client.test.ts`: `apiGetOptional` returns `null` for 204, the body for 200, and a problem for an error.
- `state/drafts.test.ts`, with fake timers and a fetch mock:
  - `saves once after typing stops`: ten changes inside the delay send one `PUT`.
  - `sends nothing unchanged`.
  - `an empty composer never deletes before its draft loads`: the `GET` answers after 3000 ms. No request is sent before it answers, and the loaded draft then fills the composer and is still on the server.
  - `typing before the load finishes is saved once, after it`.
  - `a failed load never deletes`: after a failed `GET`, an empty composer sends nothing, and a non-empty one sends a `PUT`.
  - `an untouched empty composer sends nothing`: after a 204 load, no `DELETE` follows.
  - `a disarmed key sends nothing on pagehide`.
  - `clearing deletes`.
  - `a post deletes and cancels the pending save`: a change, then a successful `postReply` before the delay ends, sends one `DELETE` and no `PUT`.
  - `requests keep their order`: a slow `PUT` then a `DELETE` reach the server in that order.
  - `a refusal turns autosave off` for 403 and 409, and `rate-limited waits for Retry-After`.
  - `pagehide starts the save inside the handler`: with nothing in flight, `fetch` is called with `keepalive` before the handler returns, with no microtask in between.
  - `pagehide never overtakes an in-flight save`: with a `PUT` in flight, no second request starts. The snapshot goes to `localStorage`, and a later `loadReplyDraft` with an older server `updated_at` fills the composer from it, sends it, and clears it. With a newer server draft, the local copy is discarded.
  - `a source of 64 KiB or more is kept locally on pagehide`.
  - `signing out clears local drafts`.
  - `an owner change drops pending saves`.
- `state/composer.test.ts` (new): `loadReplyDraft` fills an empty composer with source and reply target; it does not overwrite typed text or a chip; a 204 changes nothing; loading sends no `PUT`.
- `composer/Composer.test.tsx`: the status line.
- `composer/ReplyBar` (in `ComposerSheet.test.tsx`): after a load, the bar shows the draft's first line and the chip.
- `e2e/drafts.spec.ts` at 390 px and 1440 px: alice types a reply to a post and waits for "Draft saved"; a second browser context from `stateFile('alice')` opens the thread and finds the text and the chip, in the composer on desktop and in the reply bar on the phone; posting from it leaves `GET /api/v1/drafts/{id}` at 204. The spec deletes alice's draft before and after, because `replies.spec.ts` posts from the same composer.
- `npm run budget` passes.

### Out of scope

- The new-thread draft and screen: TKT-01M3NS16VH, which reuses `state/drafts.ts`.
- The server routes: done in TKT-01M29CA9CE.
- Showing a draft's existence in thread lists: not designed.
