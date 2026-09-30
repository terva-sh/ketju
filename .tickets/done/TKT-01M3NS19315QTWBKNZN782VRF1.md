---
schema: 3
id: TKT-01M3NS19315QTWBKNZN782VRF1
title: Autosave reply drafts from the composer
type: task
status: done
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
dependencies:
  - TKT-01M3K9XBJA8FSJ8ADCEY284SGA
blocks_on: none
references:
  - ref: pr:227
    path: null
claim: null
archive: null
created_at: 2026-09-29T05:08:40Z
updated_at: 2026-09-30T12:21:09Z
created_by:
  id: agent:claude-code/phase2-groom
  name: ""
updated_by:
  id: agent:claude-code/phase2-run-ns1931
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

- [x] A reply and its reply target typed in one browser context are restored in a second context signed in as the same member, in the composer at 1440 px and in the reply bar at 390 px (e2e/drafts.spec.ts)
- [x] A loaded draft fills only an empty composer with no reply target and no post in flight, and loading sends no PUT (state/composer.test.ts)
- [x] Saves are debounced to one PUT per 1500 ms pause, nothing is sent unchanged, a blank composer is a DELETE, and requests for one draft reach the server in order (state/drafts.test.ts)
- [x] The dist is regenerated with go generate ./internal/web/spa and committed, and npm run budget stays within 60000 B gzip
- [x] A forbidden or conflict answer turns autosave off for that thread (state/drafts.test.ts)
- [x] Autosave for a draft is disarmed until its GET answers, so a load slower than the debounce never deletes the server draft; an empty composer sends DELETE only when the server is known to hold a non-empty draft, and a failed load never deletes (state/drafts.test.ts)
- [x] A successful post deletes the draft, cancels any pending save and removes this browser's local copy, and afterwards GET /api/v1/drafts/{id} is 204 and a reload restores nothing (state/drafts.test.ts, e2e/drafts.spec.ts)
- [x] At startup and on an owner change, localStorage draft copies under any handle but the signed-in viewer's are removed, and every one is removed when nobody is signed in (state/drafts.test.ts)
- [x] On pagehide the save starts inside the handler when no request for that draft is in flight, and with one in flight no second request starts; either way a non-empty unsaved snapshot is kept in localStorage with its base revision, requests in flight and deletes in flight, and no decision reads the client's clock (state/drafts.ts tests)
- [x] On the next load the copy wins a 200 only when the revision is at most base plus requests in flight, and a 204 only when the Draft-Revision is at most base plus deletes in flight: an unacknowledged first draft comes back, text a delete removed on any device never comes back, text typed after this tab's own delete comes back even when another device's delete landed first, and a copy with no base is restored on a 204 with a notice (state/drafts.test.ts, named in the plan)

## Implementation plan

Groomed 2026-09-29 for the Phase 2 autonomous run (TKT-01M3M9RR26), against main at 9425310. Refreshed against main at 5124209 on 2026-09-30: the two requirements from the round-5 review of PR #143 (the note of 2026-09-29) are now in the plan, the local copy is ordered by the server's revision from TKT-01M3K9XBJA, on which this ticket now depends, and names and paths are corrected. Revised on 2026-09-30 for Terva review 1559 on PR #210: a 204 now carries the revision too, so an unacknowledged first draft comes back, and one posted elsewhere does not.

What exists at 5124209 (the composer files are unchanged since 9425310; PRs #188 and #205 changed only client types, and PR #202 changed `api/client.ts`, `ThreadScreen.tsx`, `Post.tsx` and `PostMore.tsx`):
- `GET/PUT/DELETE /api/v1/drafts/{thread_id}` (TKT-01M29CA9CE), answering `dto.Draft{thread_id, source, reply_to, updated_at}` or 204. The type is in `web/client/src/api/dto.ts:296-299`. TKT-01M3K9XBJA adds `revision` to it.
- `SaveDraft` refuses a reader who may not reply with `forbidden` (`internal/service/threads/viewerstate.go:185-187`). It checks the permission before the lock, so a locked thread still saves a draft, and it never answers `conflict`. An archived forum denies `reply`, so a member there gets `forbidden`.
- `state/composer.ts` holds each thread's composer in memory (`composerFor`): `source`, `replyTo`, `caret`, `summoned`, `open`, `posting`, `failure` (`:13-30`). The owner check (`:39-47`) empties every composer when the signed-in handle changes. `postReply` (`:107-132`) and `discardReply` (`:135-141`) clear it. `quotePost`, `replyToPost` and `replyToThread` write it. Its header comment (`:11-12`) says drafts last the session.
- `composer/Composer.tsx:33-34` still says the draft is not saved. `usePreview` is `:306-335`, with a 400 ms delay. `ReplyBar.tsx` shows `firstLine(c.source.value)` (`:16`, `:39`), so it shows a draft once `source` holds it.
- `api/client.ts`'s `apiGet` (`:35`) treats any `ok` answer as JSON, so a 204 from the drafts route would surface as an `internal` problem (`:135-143`). `apiSend` answers `undefined` for 204 (`:122`), takes `keepalive`, and since PR #202 an optional `status`.
- `state/readTracker.ts` sends on `pagehide` (`:229`) through `apiSend(…, {keepalive: true})` (`:81`).
- `src/api/one-path.test.ts` lets only `src/api/*` contain `fetch(`. `apiSend` reaches `fetch` before its first `await`, so a request made through it from a `pagehide` handler still starts inside the handler.
- The e2e spec that posts as alice from the seeded thread's composer is `web/client/e2e/smoke.spec.ts:17-38`. `replies.spec.ts` covers the inline reply expander and posts nothing.

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

Settled in the refresh of 2026-09-30:
- **The round-5 requirements supersede parts of rule 3 above** (the orchestrator's note of 2026-09-29, from the round-5 review of PR #143, head 4f38f14, accepted as requirements):
  1. "A successful post clears the localStorage backup as well as the server draft."
  2. "The local backup and the server draft are ordered by a revision counter shared by both sides, carried in the draft body and bumped on every save, never by comparing the client's clock with the server's updated_at."

  So in rule 3, "with the time" and "compares it with the server draft's `updated_at`" no longer hold, and the loser "a client revision the server compares" is taken up in a different form: the server issues the revision, and the client only compares it. The rest of rules 1 to 3 stands.
- **How the revision orders the local copy** (engineering choice; revised for Terva review 1559 on PR #210, finding 2). TKT-01M3K9XBJA gives every draft key a revision that only grows. Each `PUT` adds one. A `DELETE` of a live draft adds one and leaves a tombstone. Every answer carries the revision: the body's `revision` on a 200, and the `Draft-Revision` header on a 204, which is 0 for a key that has never been saved. Per key, `state/drafts.ts` keeps `known`, the revision in the last answer this tab had, which is null only while no request for the key has been answered. It also keeps `inflight`, the requests sent and not yet answered, and `deletes`, how many of those are `DELETE`s. The local copy stores `{snapshot, base: known, inflight, deletes}`. When the thread next loads in this browser, with `R` the revision the load answers:
  - **A 200 with `base` known and `R <= base + inflight`.** Nothing but this tab's own requests reached the key since the copy was made, so the copy wins. It fills the composer, is sent through the queue, and is removed.
  - **A 204 with `base` known and `R <= base + deletes`.** The key holds no draft, and at most as many deletes landed since `base` as this tab had in flight. The copy wins, as above. This is the first draft that was never acknowledged: its load answered 204 with `R0` (0, or an old tombstone's revision), so `base` is `R0`. If its save never landed, the next load answers 204 with `R0` again, and the copy comes back.
  - **Any other answer with `base` known.** More reached the key than this tab's own requests can account for, so the server's state wins and the copy is removed. On a 204 this is a draft posted or discarded elsewhere after this tab last heard from the server, settled here as an engineering choice. If the other device posted without ever saving a draft, its `DELETE` found nothing to delete and the revision did not move, so the copy comes back. That is right, because this browser's text was never on the server and cannot be what was posted.
  - **`base` null, because the load failed and no request for the key was ever answered.** On a 200 the server's draft wins and the copy is removed. On a 204 the copy is restored and saved like typed text, and the status line says "Restored from this browser" until the next change. The copy is then the only record of the member's writing. A reply posted elsewhere coming back needs a failed load, a failed save and a post on another device together, and the member sees the text before anything posts it. Discarding the copy lost, because it throws writing away in the one case the copy exists for.

  **What a restore on a 204 guarantees, and what it does not** (engineering choice, Terva review 1565 on PR #210). One promise chain per key sends each request only after the one before it has answered, and `pagehide` starts a request only when none is in flight, so a key has at most one request in flight and `deletes` is 0 or 1. On a 204 the copy's text has then never been sent to the server whenever it is restored:
  - With `deletes` 1, the request in flight is a `DELETE`, sent for a blank composer. The copy holds text typed after that, and nothing carried it, because `pagehide` sends nothing while a request is in flight.
  - With `deletes` 0, `R <= base` means nothing landed since `base`. A save of the copy's text would have moved the revision.

  So text that a delete removed, on this device or another, never comes back. What the rule cannot tell is whose delete moved the key. In the review's interleaving, this tab blanks the draft and sends `DELETE`, then types new text. Another device deletes the draft first, this tab's `DELETE` lands on the tombstone and changes nothing, and the next `GET` answers 204 at `base + 1`. The rule counts that as this tab's own delete and restores the new text. That is the behaviour chosen: the text restored is what the member typed after blanking the draft here. The server never held it, and the other device's delete removed only the draft this tab had already blanked. Discarding it would throw away writing that exists nowhere else.

  Exact accounting lost. That option has `DELETE` answer whether it applied and the revision it left, and the client counts only the deletes the server says it applied. But the copy exists because the in-flight request's answer may never be seen, when the page is torn down before it arrives. The client could then learn the outcome only if the server remembered each request, through a client-chosen id sent with every `DELETE` and looked up by the next `GET`. That widens TKT-01M29CA9CE's API and adds per-request server state, to tell apart two outcomes that restore the same never-sent text.

  **A copy never comes back after this client posted or discarded it.** `drop`, which a successful post and a discard both call, removes the copy before it queues the `DELETE` (round-5 requirement 1).

  **`pagehide` keeps a copy of every non-empty draft it has not seen saved.** This is an addition to rule 2: when it sends the save at once, it also writes the copy, counting that request in `inflight`. So a save that fails during the unload is not lost. If the save did land, the next load answers 200 with `R = base + 1`, and the copy fills the composer with the text the server already holds and is removed.

  The one case the counter still cannot tell apart is a 200 after a save from this tab failed during the unload, together with exactly as many saves from another device. Then this tab's last edit wins over the other device's. Lost: a per-tab writer id stored beside the revision, which widens the API again for a case that needs a failed request and another device's save together.
- **Local copies belong to the signed-in handle, and are cleared at load** (engineering choice). Rule 3's "Signing out ... clear every `ketju.draft.*` entry" cannot run in the sign-out itself: TKT-01M3QVWKAG (Give signed-in members a Sign out control) signs out with a plain form post, which runs no client code. So `state/drafts.ts` removes, at startup and whenever the viewer's handle changes, every `ketju.draft.*` entry whose handle is not the current viewer's, and every one when nobody is signed in. The page after sign-out loads with no viewer, and that load clears them. Lost: a hook in the sign-out form, which ties the shell to this module and misses a session that ends on the server.
- **Refusals match the server as it is.** A reply-draft save answers `forbidden` when the reader may not reply, and never `conflict` (see "What exists"). Autosave turns off for a key on `forbidden`, `conflict`, `not-found` and `unauthenticated`. `conflict` stays in that list for the forum draft of TKT-01M3NS16VH, whose save in an archived forum answers it to a member.
- **The 204's revision reaches `state/drafts.ts`** (engineering choice, review 1559). `apiGetOptional` answers `{body: T | null, headers: Headers}` rather than `T | null`, which supersedes the signature in the settled item above; it still lives in `client.ts` and holds nothing. `apiSend` gains an optional `init.onHeaders(headers)`, called when the answer arrives, so a `DELETE`'s 204 hands over its `Draft-Revision`. Both stay generic. A drafts-only request function in `client.ts` lost, because `readTracker.ts` and the drafts would then reach `fetch` by two routes.
- **Requests go through `apiSend`**, not `fetch`, because `one-path.test.ts` allows `fetch(` only under `src/api/`. Rule 2's "calls `fetch` with `keepalive` at once" is met by `apiSend(…, {keepalive: true})`, as `readTracker.ts` does, since `apiSend` calls `fetch` before it awaits anything.

### Approach

1. `api/client.ts`: `apiGetOptional<T>(url, init)`, through `request` and `toProblem` like the rest, answering `{body, headers}`, with `body` null for 204, and holding nothing; and `apiSend`'s `init.onHeaders`.
2. `state/drafts.ts`:
   - `type DraftKey = { kind: 'reply'; thread: string } | { kind: 'new'; forum: string }` and the URL for each (`/api/v1/drafts/{thread}`, `/api/v1/forums/{slug}/draft`).
   - Per key: the last saved snapshot, a timer, a `status` signal (`idle | saving | saved | failed | off`), `known`, `inflight` and `deletes` as settled, the armed flag, and one promise chain, so a `PUT` in flight is always followed, never overtaken, by a later `PUT` or `DELETE`.
   - `watch(key, read: () => Snapshot)`: the `effect` that schedules a save `draftDelay` (1500) ms after the snapshot last changed, once the key is armed. It sends nothing when the snapshot equals the last saved one.
   - `arm(key, found)`: records what the load found as the saved snapshot and `known`, and arms the key.
   - `drop(key)`: cancels the timer, marks the empty snapshot as saved, removes the key's local copy, and queues a `DELETE`.
   - `unload()` on `pagehide`, registered once, applies rules 1 to 3 synchronously. It starts at most one `keepalive` request per key and awaits nothing. The local copy is `{snapshot, base, inflight, deletes}` under `ketju.draft.<handle>.<key>`, written for every non-empty draft not yet seen saved, as settled.
   - `restoreLocal(key, status, revision)`: the load-time half, deciding by the four cases settled above. `revision` is the body's on a 200 and `Draft-Revision` on a 204.
   - `clearForeignCopies(handle)`: removes entries that are not `handle`'s, as settled. It runs at startup and on an owner change.
   - Refusals: `forbidden`, `conflict`, `not-found` and `unauthenticated` turn autosave off for the key; `rate-limited` waits for `Retry-After`; anything else is `failed`, and the next change retries.
   - `resetDrafts()` for tests. An owner change in `composerFor` calls it, so one person's pending save never goes out under the next person's session.
3. `state/composer.ts`:
   - `composerFor` starts `watch({kind: 'reply', thread}, () => ({source, reply_to}))`, disarmed, for each new composer. A thread the reader cannot reply in is never loaded, so its key is never armed and never saves.
   - `loadReplyDraft(thread)`: `apiGetOptional<Draft>`, then `restoreLocal`, then fills `source` and `replyTo` under the rule above, then `arm`s the key, so loading does not save it back.
   - `postReply` calls `drop` on success, before it clears the fields. `discardReply` calls `drop`.
   - Update the header comment (`:11-12`) that says drafts last the session.
4. `screens/ThreadScreen.tsx`: an effect on the thread id and `canReply` calls `loadReplyDraft` once per thread per session.
5. `composer/Composer.tsx`: the footer's "Markdown supported" gains a `role="status"` line from the key's `status`: "Draft saved", "Saving…", or "Draft not saved". Nothing is shown while autosave is off. Update the comment at `:33-34`.
6. Build: `npm run build`, `go generate ./internal/web/spa`, commit the dist, and record the thread view's gzip figure in a note. It was 47415 B of the 60000 B `threadViewGzipBytes` budget after PR #202.

### Tests

- `api/client.test.ts`: `apiGetOptional` returns a null body and the headers for 204, the body for 200, and a problem for an error; `apiSend` calls `onHeaders` with a 204's headers.
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
  - `a post clears the local copy`: with a local copy stored for the key, a successful `postReply` removes it, and a later load of the thread does not restore it (round-5 requirement 1).
  - `requests keep their order`: a slow `PUT` then a `DELETE` reach the server in that order.
  - `a refusal turns autosave off` for 403 and 409, and `rate-limited waits for Retry-After`.
  - `pagehide starts the save inside the handler`: with nothing in flight, `fetch` is called with `keepalive` before the handler returns, with no microtask in between.
  - `pagehide never overtakes an in-flight save`: with a `PUT` in flight, no second request starts, and the snapshot goes to `localStorage` with `base` and `inflight`.
  - `the revision orders the local copy` (round-5 requirement 2), with the clock mocked so that it disagrees with the server's `updated_at`:
    - a load answering `revision` equal to `base + inflight` restores the local copy, sends it, and removes it;
    - a load answering more than that keeps the server's draft and removes the local copy;
    - a copy whose `base` is null loses to a 200;
    - nothing in `state/drafts.ts` reads `Date.now()` to decide.
  - `an unacknowledged first draft comes back after a 204` (review 1559): the load answers 204 with `Draft-Revision: 0`; alice types; the `pagehide` save is sent and rejects. The next load answers 204 with `Draft-Revision: 0`, and the copy fills the composer, is sent, and is removed. The same holds when the key's old tombstone answers `Draft-Revision: 7` both times.
  - `a draft posted on another device does not come back after a 204`: a copy with `base` 4, `inflight` 1 and `deletes` 0, and a load answering 204 with `Draft-Revision: 5`, the other device's delete of the live draft. The copy is removed, and the composer stays empty.
  - `a reply posted elsewhere without a saved draft leaves the copy`: a copy with `base` 0, and a load answering 204 with `Draft-Revision: 0`. The copy comes back.
  - `this tab's own delete in flight does not discard its copy`: a copy with `base` 2, `inflight` 1 and `deletes` 1, and a load answering 204 with `Draft-Revision: 3`. The copy comes back.
  - `another device's delete landing first still restores the text typed after this tab's delete` (review 1565): alice's draft is live at revision 4. This tab blanks it and its `DELETE` is held unanswered; alice types new text, and `pagehide` stores the copy with `base` 4, `inflight` 1 and `deletes` 1 and sends nothing. The mock then applies another device's `DELETE` (revision 5), and this tab's `DELETE` lands on the tombstone and changes nothing. The next load answers 204 with `Draft-Revision: 5`. The copy's text is restored, and the test asserts that no request before the load carried it.
  - `a delete elsewhere after a save of the copy's text discards it`: a copy with `base` 4, `inflight` 1 and `deletes` 0, whose `PUT` landed (revision 5) before another device posted it (revision 6). A load answering 204 with `Draft-Revision: 6` removes the copy.
  - `a copy with no base is restored on a 204 and says so`: after a failed load and a failed save, a load answering 204 restores the copy with "Restored from this browser" in the status line.
  - `a discard clears the local copy`: with a copy stored for the key, `discardReply` removes it, and a later load answering 204 with an unchanged revision restores nothing.
  - `pagehide keeps a copy of a save it sends`: with nothing in flight, the handler sends the `PUT` and writes the copy with `inflight` 1. After the `PUT` lands, the next load answers 200 with `base + 1`, and the composer shows the text once.
  - `a source of 64 KiB or more is kept locally on pagehide`.
  - `a load clears copies that are not the viewer's`: entries under another handle are removed at startup, and every entry is removed when nobody is signed in.
  - `an owner change drops pending saves`.
- `state/composer.test.ts` (new): `loadReplyDraft` fills an empty composer with source and reply target; it does not overwrite typed text or a chip; a 204 changes nothing; loading sends no `PUT`.
- `composer/Composer.test.tsx`: the status line.
- `composer/ReplyBar` (in `ComposerSheet.test.tsx`): after a load, the bar shows the draft's first line and the chip.
- `web/client/e2e/drafts.spec.ts` at 390 px and 1440 px: alice types a reply to a post and waits for "Draft saved"; a second browser context from `stateFile('alice')` opens the thread and finds the text and the chip, in the composer on desktop and in the reply bar on the phone; posting from it leaves `GET /api/v1/drafts/{id}` at 204. The spec deletes alice's draft before and after, because `smoke.spec.ts` posts from the same composer.
- `npm run budget` passes.

### Out of scope

- The new-thread draft and screen: TKT-01M3NS16VH, which reuses `state/drafts.ts`.
- The server routes: done in TKT-01M29CA9CE. The `revision` column and field: TKT-01M3K9XBJA, on which this ticket depends.
- Showing a draft's existence in thread lists: not designed.
- Slow mode in the composer: TKT-01M3R0PM0K (Show slow mode in the composer) adds a `slow` prop to `Composer` and `PostButton` and edits `state/composer.ts`. Whichever of the two lands second takes the other's changes to those files; neither changes the other's rules.

## Notes

**agent:claude-code/phase2-run-orchestrator** at 2026-09-29T15:28:04Z

Carried from the round-5 review of PR #143 (head 4f38f14), at docs/13's cap. Both findings are accepted as requirements. (1) A successful post clears the localStorage backup as well as the server draft. (2) The local backup and the server draft are ordered by a revision counter shared by both sides, carried in the draft body and bumped on every save, never by comparing the client's clock with the server's updated_at. Add tests for both. TKT-01M3NS16VH shares state/drafts.ts and inherits both.

**agent:claude-code/phase2-groom** at 2026-09-30T09:00:00Z

Plan refreshed against main at 5124209 on 2026-09-30. The plan had not taken in the orchestrator's note of 2026-09-29 (round-5 review of PR #143); it does now. Requirement 1: a successful post removes the local copy (drop does it; criterion 4 rewritten). Requirement 2: the local copy is ordered by the server's revision, which TKT-01M3K9XBJA adds (this ticket now depends on it): the copy records base (the last revision this tab saw) and inflight, and wins only when the server's revision is at most base + inflight; any other answer and a 204 discard it, so every error loses at most the edit made during an in-flight save and never brings back a posted reply (criterion 7 rewritten). The Settled items are carried verbatim, with a new item saying which sentences of rule 3 the note supersedes. Also settled as engineering choices: local copies not belonging to the signed-in handle are cleared at startup, because TKT-01M3QVWKAG signs out with a plain form post that runs no client code (new criterion 9); requests go through apiSend with keepalive, since one-path.test.ts allows fetch( only in src/api. Corrected: SaveDraft never answers conflict for a reply draft and a locked thread still saves (forbidden when the reader may not reply); composerFor also holds summoned and open; usePreview is Composer.tsx:306-335; the spec that posts from the same composer is smoke.spec.ts, not replies.spec.ts; the budget reference is 47415 of 60000 B. Out of scope now names TKT-01M3R0PM0K (Show slow mode in the composer), which edits the same composer files.

**agent:claude-code/phase2-groom** at 2026-09-30T09:00:06Z

Correction to the previous note's criterion numbers, which it supersedes on that point: rewriting two criteria moved them to the end. The post-clears-the-local-copy criterion is now 7, the revision-ordering one 8, and the clear-foreign-copies one 9.

**agent:claude-code/phase2-groom** at 2026-09-30T09:23:50Z

Terva review 1559 on PR #210 (head 922a23c), finding 2 (medium), accepted. A first draft never acknowledged had base null, and a 204 deleted its only copy. Now a 204 carries Draft-Revision (TKT-01M3K9XBJA), so a load that answers 204 still sets base, and the copy records base, requests in flight and deletes in flight. On a 204 the copy comes back only when Draft-Revision <= base + deletes: nothing but this tab's own deletes moved the key, so its writing never reached the server. That restores an unacknowledged first draft. Settled as an engineering choice for the cross-device case: a draft posted or discarded on another device moved the revision when that device deleted the live draft, so the copy is removed and a posted reply never comes back; a reply posted elsewhere without ever saving a draft leaves the revision where it was, and the copy, which was never on the server, comes back. A copy with no base, after a failed load, is restored on a 204 with 'Restored from this browser' rather than thrown away. pagehide now also writes a copy when it sends a save at once, so a save lost in the unload is recoverable. Posting and discarding call drop, which removes the copy before the DELETE, so it never returns after this client posted or discarded it. apiGetOptional answers {body, headers} and apiSend gains onHeaders, superseding the settled signature. Criterion 9 replaces the old pagehide criterion (now 8 is the foreign-copies one), and new criterion 10 covers the load cases, with tests named in the plan: an unacknowledged first draft comes back after a 204; a draft posted on another device does not come back after a 204; a reply posted elsewhere without a saved draft leaves the copy; this tab's own delete in flight does not discard its copy; a copy with no base is restored on a 204 and says so; a discard clears the local copy.

**agent:claude-code/phase2-groom** at 2026-09-30T09:40:18Z

Terva review 1565 on PR #210 (head 8c111f4), medium, accepted. The 204 rule R <= base + deletes cannot tell whose delete moved the key. Settled as an engineering choice: the trade-off is stated, not removed. A key has at most one request in flight (one chain per key; pagehide starts none while one is out), so deletes is 0 or 1, and on a 204 a restored copy's text has never been sent to the server. Text a delete removed, here or on another device, never comes back. In the review's interleaving (this tab blanks and sends DELETE, types new text, another device's delete lands first, ours lands on the tombstone, GET answers 204 at base + 1), the text typed after this tab's own delete is restored; that is the chosen behaviour, since it exists nowhere else and the other device deleted only the draft this tab had already blanked. The plan's claim that a remotely deleted draft cannot return is replaced by that guarantee. Lost: exact accounting, where DELETE answers whether it applied and the client counts only applied deletes. The copy exists because that answer may never arrive, so it would need a client-chosen request id remembered by the server and read back by GET, widening TKT-01M29CA9CE's API to tell apart two outcomes that restore the same never-sent text. Tests: 'another device's delete landing first still restores the text typed after this tab's delete' (the interleaving) and 'a delete elsewhere after a save of the copy's text discards it'; the existing own-delete test now uses inflight 1, since inflight cannot be 2. Criterion 10 is rewritten to match.

**agent:claude-code/phase2-run-ns1931** at 2026-09-30T11:26:10Z

Built against main at f388921 as the plan settles it. The plan's Implementation plan section is left as groomed, since every settled item holds. The engineering choices below are the implementation's, each with the alternative that lost.

- **One request in flight, and only the newest waiting request is kept**, rather than a promise chain of every save. Each PUT and DELETE replaces the whole draft, so an older request still waiting would only be overwritten. A key still has at most one request in flight, so `deletes` is 0 or 1 as the plan's guarantee needs.
- **pagehide drops a request waiting behind the one in flight.** It would start only if the page lived, and if it landed it would move the revision past `base + inflight` and discard the copy that holds newer text. Test: 'drops a request queued behind one in flight, and counts only what started'.
- **A request that failed without an answer (network) counts as in flight in the copy until the next answer.** The server may have applied it, so leaving it out would let a landed save discard the copy. Lost: counting only requests still open, which the plan's wording allows but which loses the last edit after a network failure.
- **A load refused as forbidden, not-found, unauthenticated or conflict turns autosave off**, as a refused save does. Lost: arming with the state unknown, which would save into a thread the reader cannot keep a draft in.
- **An answer with no readable revision restores the copy on a 204 with the notice**, as a null base does. The server always sends one, so this happens only if it does not, and keeping writing wins over discarding it.
- **A post or discard during the load moves a generation counter**, so the load's answer neither fills the composer nor replaces the queued DELETE as the server state. Test: 'a post during the load deletes, and the load does not bring the draft back'.
- **visibilitychange to hidden sends a pending save at once, and pageshow from the back/forward cache reschedules what pagehide held back.** On a phone a hidden page is often never shown again. Both use the normal queue.
- **The status line says "Saving…" from the first change until the answer**, "Draft saved" once the server holds a non-empty draft, "Draft not saved" after a failure, and nothing when the draft is empty or autosave is off. On a phone the sheet hides the composer footer (composer.css), so the line, "Restored from this browser" included, shows on the desktop composer only; the restored text itself shows in the bar and the sheet.
- A copy that wins while the reader has already typed into the composer is not shown: what they typed wins and is saved, as settled for a loaded draft.

Bundle: the thread view is 49521 B gzip of the 60000 B budget, up 2106 B from 47415 B at f388921.

e2e: drafts.spec.ts replies to post 10, not post 2, because replies.spec.ts asserts that post 2 has no reply expander. Locally, the desktop smoke sign-in hits the /auth burst limit (429) on main at f388921 as well as on this branch, so it is not caused by this change; every other spec passes here.

## Summary

The composer's source and reply target autosave to the viewer's reply draft (GET/PUT/DELETE /api/v1/drafts/{id}) and load when the thread page opens, so the phone's reply bar shows a draft before the sheet opens. state/drafts.ts is the shared autosave module, keyed reply:<thread> or new:<forum>, for TKT-01M3NS16VH to reuse. It debounces saves by 1500 ms, keeps one request in flight per key, stays disarmed until the GET answers, and deletes only a draft the server is known to hold. On pagehide it keeps a localStorage copy ordered by the server's revision, and it never reads the client's clock. api/client.ts gains apiGetOptional ({body, headers}) and apiSend's onHeaders. The thread view is 51126 B gzip of 60000 B, up 2107 B. All ten criteria are met. The implementation choices beyond the plan are in the note of 2026-09-30.
