---
schema: 3
id: TKT-01M3NS19315QTWBKNZN782VRF1
title: Autosave reply drafts from the composer
type: task
status: ready
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
references: []
claim: null
archive: null
created_at: 2026-09-29T05:08:40Z
updated_at: 2026-09-30T09:00:06Z
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
- [ ] The dist is regenerated with go generate ./internal/web/spa and committed, and npm run budget stays within 60000 B gzip
- [ ] A forbidden or conflict answer turns autosave off for that thread (state/drafts.test.ts)
- [ ] Autosave for a draft is disarmed until its GET answers, so a load slower than the debounce never deletes the server draft; an empty composer sends DELETE only when the server is known to hold a non-empty draft, and a failed load never deletes (state/drafts.test.ts)
- [ ] A successful post deletes the draft, cancels any pending save and removes this browser's local copy, and afterwards GET /api/v1/drafts/{id} is 204 and a reload restores nothing (state/drafts.test.ts, e2e/drafts.spec.ts)
- [ ] On pagehide the save starts inside the handler when no request for that draft is in flight; with one in flight no second request starts and the snapshot is kept in localStorage with its base revision, and the next load sends it only when the server's revision is at most that base plus the requests then in flight, discarding it on any other answer or a 204, with no decision read from the client's clock (state/drafts.test.ts)
- [ ] At startup and on an owner change, localStorage draft copies under any handle but the signed-in viewer's are removed, and every one is removed when nobody is signed in (state/drafts.test.ts)

## Implementation plan

Groomed 2026-09-29 for the Phase 2 autonomous run (TKT-01M3M9RR26), against main at 9425310. Refreshed against main at 5124209 on 2026-09-30: the two requirements from the round-5 review of PR #143 (the note of 2026-09-29) are now in the plan, the local copy is ordered by the server's revision from TKT-01M3K9XBJA, on which this ticket now depends, and names and paths are corrected.

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
- **How the revision orders the local copy** (engineering choice). TKT-01M3K9XBJA gives every draft a `revision`. A `PUT` that updates a row adds one, and a `PUT` that inserts one starts it at the server's time in milliseconds, so a draft deleted and started again is newer than any copy of the old one. Per key, `state/drafts.ts` keeps `known`, the revision in the last answer this tab had (null after a 204 or a failure), and `inflight`, the number of requests sent and not yet answered. The local copy stores the snapshot with `base = known` and `inflight`. When the thread next loads in this browser:
  - a 200 with `revision <= base + inflight`, where `base` is not null, means nothing but this tab's own requests reached the server since the copy was made, so the local copy wins. It fills the composer, is sent through the queue, and is removed;
  - any other 200, or a copy whose `base` is null, means the server's draft may be newer, so the server's copy wins and the local copy is removed;
  - a 204 removes the local copy, because the draft was posted or discarded, perhaps on another device, and bringing it back would bring back a reply already posted.

  Every error goes the safe way: the worst case loses the edit made while a save was in flight, the bound rule 3 already accepts. The one case the counter cannot tell apart is a save from this tab that failed during the unload, together with exactly as many saves from another device before this browser reopens the thread. Then this tab's last edit wins over the other device's. Lost: a per-tab writer id stored beside the revision, which widens the API again for a case that needs a failed request and another device's save together.
- **Local copies belong to the signed-in handle, and are cleared at load** (engineering choice). Rule 3's "Signing out ... clear every `ketju.draft.*` entry" cannot run in the sign-out itself: TKT-01M3QVWKAG (Give signed-in members a Sign out control) signs out with a plain form post, which runs no client code. So `state/drafts.ts` removes, at startup and whenever the viewer's handle changes, every `ketju.draft.*` entry whose handle is not the current viewer's, and every one when nobody is signed in. The page after sign-out loads with no viewer, and that load clears them. Lost: a hook in the sign-out form, which ties the shell to this module and misses a session that ends on the server.
- **Refusals match the server as it is.** A reply-draft save answers `forbidden` when the reader may not reply, and never `conflict` (see "What exists"). Autosave turns off for a key on `forbidden`, `conflict`, `not-found` and `unauthenticated`. `conflict` stays in that list for the forum draft of TKT-01M3NS16VH, whose save in an archived forum answers it to a member.
- **Requests go through `apiSend`**, not `fetch`, because `one-path.test.ts` allows `fetch(` only under `src/api/`. Rule 2's "calls `fetch` with `keepalive` at once" is met by `apiSend(…, {keepalive: true})`, as `readTracker.ts` does, since `apiSend` calls `fetch` before it awaits anything.

### Approach

1. `api/client.ts`: `apiGetOptional<T>(url, init)`, through `request` and `toProblem` like the rest, answering `null` for 204 and holding nothing.
2. `state/drafts.ts`:
   - `type DraftKey = { kind: 'reply'; thread: string } | { kind: 'new'; forum: string }` and the URL for each (`/api/v1/drafts/{thread}`, `/api/v1/forums/{slug}/draft`).
   - Per key: the last saved snapshot, a timer, a `status` signal (`idle | saving | saved | failed | off`), `known` and `inflight` as settled, the armed flag, and one promise chain, so a `PUT` in flight is always followed, never overtaken, by a later `PUT` or `DELETE`.
   - `watch(key, read: () => Snapshot)`: the `effect` that schedules a save `draftDelay` (1500) ms after the snapshot last changed, once the key is armed. It sends nothing when the snapshot equals the last saved one.
   - `arm(key, found)`: records what the load found as the saved snapshot and `known`, and arms the key.
   - `drop(key)`: cancels the timer, marks the empty snapshot as saved, removes the key's local copy, and queues a `DELETE`.
   - `unload()` on `pagehide`, registered once, applies rules 1 to 3 synchronously. It starts at most one `keepalive` request per key and awaits nothing. The local copy is `{snapshot, base, inflight}` under `ketju.draft.<handle>.<key>`.
   - `restoreLocal(key, answer)`: the load-time half, deciding as settled.
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
  - `a post clears the local copy`: with a local copy stored for the key, a successful `postReply` removes it, and a later load of the thread does not restore it (round-5 requirement 1).
  - `requests keep their order`: a slow `PUT` then a `DELETE` reach the server in that order.
  - `a refusal turns autosave off` for 403 and 409, and `rate-limited waits for Retry-After`.
  - `pagehide starts the save inside the handler`: with nothing in flight, `fetch` is called with `keepalive` before the handler returns, with no microtask in between.
  - `pagehide never overtakes an in-flight save`: with a `PUT` in flight, no second request starts, and the snapshot goes to `localStorage` with `base` and `inflight`.
  - `the revision orders the local copy` (round-5 requirement 2), with the clock mocked so that it disagrees with the server's `updated_at`:
    - a load answering `revision` equal to `base + inflight` restores the local copy, sends it, and removes it;
    - a load answering more than that keeps the server's draft and removes the local copy;
    - a load answering 204 removes the local copy and fills nothing;
    - a copy whose `base` is null never wins;
    - nothing in `state/drafts.ts` reads `Date.now()` to decide.
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
