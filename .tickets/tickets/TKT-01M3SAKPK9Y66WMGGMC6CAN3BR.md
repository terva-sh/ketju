---
schema: 3
id: TKT-01M3SAKPK9Y66WMGGMC6CAN3BR
title: Guard a reply post answered after the viewer changed
type: bug
status: ready
status_reason: null
priority: normal
due_on: null
labels:
  - area:client
  - effort:S
assignees: []
milestone: phase-2
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-30T14:13:32Z
updated_at: 2026-09-30T19:35:26Z
created_by:
  id: agent:claude-code/phase2-run-ns16vh
  name: ""
updated_by:
  id: agent:claude-code/phase2-run-orchestrator
  name: ""
extensions: {}
---

## Description

Terva review 1591 on PR #235 (head 0a72954) found this in the new-thread composer, and TKT-01M3NS16VH fixed it there. The reply composer has the same shape.

`postReply` in `web/client/src/state/composer.ts` awaits the POST and then calls `drop({kind: 'reply', thread})` by key. Suppose the viewer changes in the same tab while the POST is out, and the next viewer opens the same thread. `composerFor`'s owner check resets the draft keys, and the next viewer's load creates a new entry under that key. The old answer then drops the new entry, which queues a DELETE of the next viewer's reply draft under their session.

Signing in and out loads a page today, which empties the module state, so this only happens if that changes. The fix is the one `createThread` now has (`state/newThread.ts`, `current`): capture the owner before the await, and after it drop nothing and answer null when the owner or the composer is no longer the one that posted.

## Acceptance criteria

- [ ] postReply in web/client/src/state/composer.ts captures the owner before the POST and, after it, answers null without calling drop, clearing the composer or starting a slow-mode wait when current(thread, c, who) is false; current checks viewer.peek()'s handle, owner and composers.get(thread) === c, as createThread's current in state/newThread.ts does
- [ ] postReply's catch answers null before extend and before setting failure when current(thread, c, who) is false
- [ ] Security: state/drafts.test.ts 'a post answered after the viewer changed drops nothing and answers null' shows no DELETE is sent and the next viewer's server draft and composer text are unchanged
- [ ] Security: state/drafts.test.ts 'a post answered after the viewer changed and back does not clear the new composer' covers a change and a change back to the same handle with a composerFor lookup while the other viewer is current, and 'a post answered after a change and back with no lookup between clears the draft as usual' pins the accepted case
- [ ] state/drafts.test.ts 'a post answered after the viewer changed, before any composer was asked for, answers null' and 'a refusal answered after the viewer changed sets nothing' pass, and neither starts a slow-mode wait
- [ ] composer/Post.test.tsx 'a reply answered after the viewer changed does not move the page' shows location.pathname unchanged after a 201
- [ ] state/drafts.ts is unchanged in the diff, and every existing test in state/drafts.test.ts, state/composer.test.ts, composer/Post.test.tsx, composer/Composer.test.tsx and composer/ComposerSheet.test.tsx passes unchanged
- [ ] The dist is regenerated with go generate ./internal/web/spa and committed, and npm run budget passes

## Implementation plan

Groomed 2026-09-30 against main at 13709f0 (TKT-01M3M9RR26).

What exists at 13709f0:
- `postReply(thread, slow?)` in `web/client/src/state/composer.ts` takes `c = composerFor(thread)`, awaits `apiSend('POST', /api/v1/threads/{id}/posts, …)`, and then, with no check, calls `drop(draftKey(thread))`, clears `c.source`, `c.replyTo`, `c.caret` and `c.open`, starts the slow-mode wait with `extend(c, …)`, and answers the `CreatedPost`. Its `catch` calls `extend(c, …)` for a rate-limited refusal and sets `c.failure`.
- `composerFor` calls `checkOwner()`, which runs `forget()` (clears every composer, its timer, and every draft key through `resetDrafts()`) when `viewer.value.user?.handle` is not the module's `owner`. `owner` is module-private.
- `drop(key)` in `state/drafts.ts` acts on whatever entry is under the key's id now. After a viewer change that is the next viewer's entry, created by `watch` when their `composerFor` ran, with `owner` set to their handle. `drop` removes that viewer's local copy (`ketju.draft.<handle>.reply:<thread>`) and queues a DELETE of their draft under their session.
- The answer goes to `Composer.tsx` (`PostButton`, `postReply(thread, slow).then((res) => res && onPosted(res))`) and to `ComposerSheet.tsx`. `onPosted` is `posted` in `screens/ThreadScreen.tsx`, which routes to the new post's permalink when the reader is still on the thread. A non-null answer after a viewer change therefore moves the next viewer's screen to the previous viewer's post.
- `createThread` in `state/newThread.ts` already has the fix (PR #235, TKT-01M3NS16VH, Terva review 1591 finding 2, fixed in bf430bf). It captures `who = owner` before the await, and after it answers null without dropping or clearing when `current(forum, n, who)` is false. `current` compares `viewer.peek()`'s handle, the module's `owner`, and `states.get(forum) === n`. The test is 'a create answered after the viewer changed drops nothing and answers null' in `state/newThread.test.ts`.

### Settled in grooming

- **This is a security fix** (maintainer's framing, 2026-09-30, from the grooming brief for this ticket). The stale answer can delete the next viewer's reply draft under their session and remove their local copy. It also routes the next viewer's screen to the post the previous viewer made. Under docs/13 "The extension of 2026-09-30" the run always fixes a security finding, so a review finding on this change is fixed, not recorded as a disagreement.
- **The fix is the #235 pattern** (TKT-01M3NS16VH's summary and review 1591 finding 2 in its notes). Capture the owner before the await. After the await, when the owner or the composer is no longer the one that posted, drop nothing, clear nothing, start no wait, and answer null. `checkOwner` in `state/composer.ts` stays the one place that resets composers and draft keys on a viewer change; this ticket reuses it and does not add a second reset.
- **`state/drafts.ts` is not changed.** Its rules were settled in PR #227 (TKT-01M3NS1931). Terva review 1585 finding 1 (high), 'Preserve deletion recovery when a post races the initial GET', was recorded there as `disagreement: pending maintainer`. The maintainer accepted that disagreement on 2026-09-30: an empty local copy with no base does not delete on a 200, and the server's draft wins. Do not reopen it. This ticket changes only who may call `drop` after a post answers, not what `drop`, `load` or `restoreLocal` do.
- **Signing in and out loads a page today**, which empties the module state (the comment on `owner` in `state/composer.ts`). The guard holds without relying on that, as `checkOwner` already does.

Engineering decisions taken here, with the alternative that lost:
- **A private `current(thread, c, who)` in `state/composer.ts`**, the same three checks as `newThread.ts`'s `current`: `(viewer.peek().user?.handle ?? '') === who`, `owner === who`, and `composers.get(thread) === c`. All three are needed:
  - The viewer check catches a change that no `composerFor` call has followed yet. `owner` is still the old handle and `c` is still in the map, but the answer must still be null so the next viewer's screen is not routed.
  - The owner check catches a change that a `composerFor` or `newThreadFor` call has followed.
  - The identity check catches a change and a change back, such as alice to anonymous to alice, when anything looked up a composer while the other viewer was current. That lookup ran `checkOwner()`, `forget()` replaced the composer and the draft entry, and the handle matching again does not make the new ones the ones that posted.
  - A change away and back with no lookup in between is accepted, not guarded (review 1609, PR #242). Nothing ran as the other viewer, so the composer, its draft entry and the POST are all alice's. The POST went out under alice's session, and clearing her draft when it lands is the correct outcome. A viewer generation counter bumped on every handle change, the alternative the review offered, lost: it would answer null here, and alice's draft would survive holding text she has already posted, which invites a second post. No other viewer's text or draft is reachable in this sequence, so there is nothing for the guard to protect.

  It reads `viewer.peek()` so the check subscribes to nothing. Two alternatives lost. Calling `composerFor(thread)` after the await and comparing identity would create a composer and `watch` a draft key for the next viewer as a side effect of an answer. Exporting one shared `current` from `composer.ts` for both modules would not help, because each module checks its own map (`composers`, `states`).
- **The guard is in `postReply`, not in `drafts.ts`'s `drop`.** A `drop(key, owner)` that refuses an entry under another owner would protect the DELETE but not the answer: `postReply` would still return the post, and `ThreadScreen`'s `posted` would still route the next viewer. It would also change the module whose rules the review 1585 decision settled.
- **The refusal path is guarded too.** In `catch`, when `current` is false, `postReply` answers null before `extend` and before setting `failure`. A rate-limited answer would otherwise start a one-second `hold` timer on a composer that `forget()` has already cleared. That composer is invisible, so this is cleanup rather than a leak anyone sees, but it keeps both paths the same shape as `createThread`. The `finally` that resets `c.posting` stays, because it touches only the composer that posted.

### Approach

1. `web/client/src/state/composer.ts`, `postReply`: after `const c = composerFor(thread);` (which has run `checkOwner()`), capture `const who = owner;` before the early return and the await.
2. After `await apiSend(…)`, add `if (!current(thread, c, who)) return null;` before `drop(draftKey(thread))`. Nothing below it runs for a stale answer: no `drop`, no clearing of `source`, `replyTo`, `caret` or `open`, and no `extend`.
3. In `catch`, add the same check first: `if (!current(thread, c, who)) return null;`, before the rate-limited `extend` and before `c.failure.value = err`.
4. Add `function current(thread: string, c: Composer, who: string | null): boolean` below `postReply`, with a comment in the style of `newThread.ts`'s: whether `c` is still `thread`'s composer for the viewer who was `who`, read without starting a new one.
5. Extend `postReply`'s doc comment with the paragraph `createThread` carries: an answer that arrives after the viewer has changed belongs to nobody on screen. It drops no draft, because the key is now the next person's, and it answers null, so the screen goes nowhere. Cite this ticket and Terva review 1591.
6. Build: `npm run build` in `web/client`, then `go generate ./internal/web/spa`, and commit the regenerated dist (the built client is committed, and CI fails otherwise). `npm run budget` stays within the reader's 70000 B gzip budget (docs/08 "Decided 2026-09-30").

### Tests

In `web/client/src/state/drafts.test.ts`, `describe('posting and discarding')`. Use its fake server and a `hold('POST')` gate, as 'a post while a save is in flight deletes after it' does with `hold('PUT')`:
- **'a post answered after the viewer changed drops nothing and answers null'** (security). Alice loads, types, and calls `postReply(T)` with the POST held. The viewer becomes bob (`viewer.value = {...member, user: {...member.user, handle: 'bob'}}`). The server holds bob's draft (`server = {revision: 7, live: {source: 'Bob’s', reply_to: null}}`), and `loadReplyDraft(T)` shows it in `composerFor(T)`. Then `land` the POST. Expect the promise to answer null, `writes()` to be `[]` (no DELETE), `server.live` to still be bob's draft, and bob's composer to still show 'Bob’s' after `wait(draftDelay * 2)`.
- **'a post answered after the viewer changed and back does not clear the new composer'** (security, the identity check). Alice posts with the POST held. The viewer becomes anonymous and `composerFor(T)` runs as anonymous, which forgets alice's composers. The viewer becomes alice again, `composerFor(T)` gets new text and `loadReplyDraft(T)` runs. Land the POST. Expect null, the new composer's text kept, and no DELETE. Removing the identity check makes this test fail.
- **'a post answered after a change and back with no lookup between clears the draft as usual'** (the accepted case, review 1609). Alice posts with the POST held. The viewer becomes anonymous and then alice again with no `composerFor` call. Land the POST. Expect the post answered, the composer cleared and the draft deleted, as for a post with no change.
- **'a post answered after the viewer changed, before any composer was asked for, answers null'** (the viewer check). Alice posts with `slow = {seconds: 30, nextPostAt: null, exempt: false}` and the POST held. The viewer becomes bob and nothing calls `composerFor`. Land the POST. Expect null, no DELETE, and `c.slowUntil.value` still null on the composer alice posted from.
- **'a refusal answered after the viewer changed sets nothing'**. As above, but the POST answers 429 `rate-limited` with `Retry-After: 30`. Expect null, and `c.failure.value` and `c.slowUntil.value` both null on the composer alice posted from.
- Every existing test in `state/drafts.test.ts`, `state/composer.test.ts`, `composer/Post.test.tsx`, `composer/Composer.test.tsx` and `composer/ComposerSheet.test.tsx` passes unchanged. That includes 'a post during the load deletes, and the load does not bring the draft back', the in-page half of review 1585.

In `web/client/src/composer/Post.test.tsx`, `describe('posting a reply')`:
- **'a reply answered after the viewer changed does not move the page'**. Render the thread with `serve(answer)` holding the POST, type, and click Post. Change the viewer's handle, then answer 201 with `s.ok()`. Expect `location.pathname` to still be the thread's path, not `/t/{id}/{slug}/3`.

### Out of scope

- Any change to `state/drafts.ts`: `drop`, `load`, `restoreLocal`, `unload` and `startDrafts` stay as PR #227 left them, including the rule for an empty copy with no base that review 1585 argued over.
- Making sign-in or sign-out a same-page transition. Nothing plans it. This ticket makes the reply path safe if one comes.
- `createThread` and `discardNewThread` in `state/newThread.ts`, already guarded by TKT-01M3NS16VH.
- `discardReply`, `quotePost`, `replyToPost` and `replyToThread`. Each is synchronous and calls `composerFor` first, so each acts on the current viewer's composer.
- `loadReplyDraft`. `load` in `drafts.ts` already ignores an answer whose entry was replaced (`entries.get(e.id) !== e`).
