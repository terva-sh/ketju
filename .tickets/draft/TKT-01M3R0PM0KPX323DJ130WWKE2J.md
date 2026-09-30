---
schema: 3
id: TKT-01M3R0PM0KPX323DJ130WWKE2J
title: Show slow mode in the composer and add the moderator's slow mode dialog
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:moderation
  - area:client
  - effort:S
assignees: []
milestone: phase-2
parent: TKT-01M29BN36JH4M1W5819D06PGPP
origin: null
dependencies:
  - TKT-01M29CRJ50CEYVG2VHYEXYVDKB
  - TKT-01M3QGH66WA5X7DBGZS8662S5T
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-30T02:01:08Z
updated_at: 2026-09-30T04:04:40Z
created_by:
  id: agent:claude-code/phase2-run-crj50
  name: ""
updated_by:
  id: agent:claude-code/phase2-groom
  name: ""
extensions: {}
---

## Description

Show slow mode in the thread page's composer, and give moderators a "Slow mode…" action in the thread's moderator actions (web/client). The server side is TKT-01M29CRJ50 (Implement per-thread slow mode). It shipped without UI because the client lane held the committed dist (the docs/13 lane rule). Its plan's step 6 described this work, and the criteria below are the client half of that ticket's third criterion and the client test its plan named.

The contract is in docs/07 "Slow mode" and the `POST /threads/{id}/posts` row. `web/client/src/api/dto.ts` already carries the types.
- `viewer.slow_mode_seconds` on the thread page is the thread's effective interval, 0 for off. Every viewer gets it.
- `viewer.next_post_at` is the Unix ms when this viewer may next post. It is null while they may post, for a moderator, who is exempt, and for an anonymous viewer.
- A reply sent too soon answers `rate-limited` with `Retry-After` in whole seconds, rounded up.
- `PUT /api/v1/admin/threads/{id}/slow-mode` with `{seconds}`, 0 to 86400, answers 200 `SlowMode` `{seconds}`, `no-store`. Setting the value the thread already has is `conflict`, one out of range is `validation-failed`, an archived thread is `conflict`, and a viewer without moderate in the forum is `forbidden`.

### Composer

- When `slow_mode_seconds > 0`, the composer shows "Slow mode: one post every N seconds", with N written as minutes or hours when it divides evenly.
- While `next_post_at` is in the future, Send is disabled with a countdown to it. It re-enables when the countdown ends, without a refetch.
- A `rate-limited` answer (another tab posted, or the clock is skewed) shows the wait from `Retry-After` and keeps the draft.

### Moderator action

The thread's moderator actions (TKT-01M29CKRGS's dialog pattern) gain "Slow mode…". It opens a dialog with a seconds field, or a choice of Off, 10 s, 30 s, 1 min, 5 min and a custom value, prefilled from `slow_mode_seconds`. It sends one PUT and then refetches the page, so the viewer block shows the new value. `conflict` and `validation-failed` are shown beside the field.

Tests: the notice text, the disabled Send and the countdown under fake timers, the `Retry-After` path, and the dialog's request and error display. The thread-view bundle stays within the 60000 B gzip budget, and the regenerated dist is committed.

## Acceptance criteria

- [ ] With slow_mode_seconds > 0 the composer shows 'Slow mode: one post every N seconds', and with 0 it shows nothing (Composer.test.tsx)
- [ ] While viewer.next_post_at is in the future Send is disabled with a countdown that re-enables it at next_post_at without a refetch, under fake timers (Composer.test.tsx)
- [ ] The thread-view bundle stays within the 60000 B gzip budget and the dist from go generate ./internal/web/spa is committed
- [ ] A rate-limited answer keeps the draft, says the wait from Retry-After, and holds Send until it passes, under fake timers (Post.test.tsx)
- [ ] A successful post starts the wait for a viewer without can_moderate when slow_mode_seconds > 0, and a moderator is never held (Composer.test.tsx)
- [ ] 'Slow mode…' is offered only with can_moderate and a role of moderator or admin; it is prefilled from slow_mode_seconds, sends one PUT /api/v1/admin/threads/{id}/slow-mode {seconds}, asks for the page again, and shows conflict and validation-failed beside the field (ThreadScreen.test.tsx)

## Implementation plan

Groomed 2026-09-30 for Phase 2 (TKT-01M3M9RR26), against main at 9a8fc76. The server half is TKT-01M29CRJ50 (Implement per-thread slow mode), PR #188 at a827aef, with migration 0017 `thread_slow_mode`. Its summary and notes 1 to 3 were read for this plan.

Two corrections to the description, read from main:
- **There is no moderator dialog on the thread screen to extend.** TKT-01M29CKRGS shipped lock and pin without a client. Its client half is TKT-01M3QGH66W (Add lock, unlock, pin and unpin to the thread screen, ready), which builds the `moderate` option of `shell/threadActions.ts`, the inline panel `components/thread/ModerateDialog.tsx`, and the `mayModerate` test. This ticket depends on it.
- **The `Retry-After` path is already built.** `postFailure` in `web/client/src/composer/Composer.tsx` says "This thread is in slow mode. You can post again in N seconds." from `ProblemError.retryAfter` (`api/problem.ts`). `postReply` in `state/composer.ts` keeps the source on any refusal. `composer/Post.test.tsx` ("a refused post", the 429 row with `Retry-After: 30`) tests both. This ticket adds the countdown after such an answer.

### Settled in grooming

- **The contract is TKT-01M29CRJ50's** (docs/07 "Slow mode" and the `POST /threads/{id}/posts` row).
  - `viewer.slow_mode_seconds` is the effective interval, 0 for off, sent to every viewer.
  - `viewer.next_post_at` is Unix ms, and null while the viewer may post, for a moderator, who is exempt, and for an anonymous viewer.
  - A reply sent too soon is `rate-limited` with `Retry-After` in whole seconds, rounded up.
  - `PUT /api/v1/admin/threads/{id}/slow-mode` takes `{seconds}` from 0 to 86400 (`threads.MaxSlowModeSeconds`) and answers 200 `SlowMode` `{seconds}`, `no-store`. Refusals come in lock's order: the gate, then `bad-request`, then `forbidden` without `moderate`, then `conflict` on an archived thread, then `validation-failed` out of range, then `conflict` for the value the thread already has.
  - `dto.ts` and `dto-contract.ts` already carry the types. No server change.
- **Slow mode stays in Phase 2** (maintainer decision 2026-09-29, TKT-01M29CRJ50 note 1).
- **Moderators are exempt, and agents are not** (docs/08 "Primitives" and "Rate limits").
- **Only staff set slow mode** (TKT-01M29CRJ50 note 2). The route is behind `staffGate`, so "Slow mode…" is offered with `mayModerate`, meaning `viewer.can_moderate` and a role of moderator or admin, as lock is. A member whose group grants `moderate` is exempt from slow mode but cannot set it. That asymmetry is the server's, and changing it is a product change outside this ticket.
- **Setting a value over no row is a change** (TKT-01M29CRJ50 note 2). Setting the site default on a thread with no row of its own pins it and is not `conflict`. So the client cannot predict `conflict` from `slow_mode_seconds`, and always sends.
- **One deadline per composer** (engineering choice). `Composer` in `state/composer.ts` gains `slowUntil: Signal<number | null>`, a Unix ms, set from three sources:
  - the page's `next_post_at`, whenever the page's value changes;
  - a `rate-limited` answer's `Retry-After`, as `Date.now() + retryAfter * 1000`;
  - the viewer's own successful post, as `Date.now() + slow_mode_seconds * 1000`, when `slow_mode_seconds > 0` and the viewer does not moderate. The page's viewer block is not fetched again after a post, so without this Send would stay enabled into a certain refusal.

  `PostButton` is disabled while `Date.now() < slowUntil`. Holding the countdown in component state lost: the phone's sheet unmounts the composer when it closes, and the wait has to survive until it opens again, as the draft does.
- **The client's clock decides when Send comes back** (engineering choice). The server answers in absolute Unix ms, and the client has no server time. A client clock that runs fast re-enables Send early, and the post is then refused with `Retry-After`, which resets the countdown. That path exists anyway for a second tab. Estimating the skew from the `Date` header lost: it adds a moving part for a wait of seconds.
- **The words** (engineering choice, from the description).
  - The notice reads "Slow mode: one post every N seconds". N is written in minutes or hours when it divides evenly, and one unit reads "every minute", "every hour" or "every second".
  - The countdown reads "You can post again in m:ss", or "h:mm:ss" from an hour up, beside the disabled Send, which points to it with `aria-describedby`. It updates every second and is not a live region, so a screen reader is not interrupted each second.
  - The dialog's words are in Approach step 4.
- **The built client is committed.** Run `go generate ./internal/web/spa` and commit `internal/web/spa/dist`. Only one lane may have a PR open that changes the dist (docs/13 "Running two lanes").

### Approach

1. **`state/composer.ts`**:
   - Add `slowUntil` to `Composer` and to `composerFor`'s new entries.
   - `postReply` takes an optional `slow?: { seconds: number; exempt: boolean }`. On success, when `seconds > 0 && !exempt`, it sets `slowUntil`. In the `catch`, a `rate-limited` answer with `retryAfter` sets `slowUntil` from it.
   - `discardReply` leaves `slowUntil` alone, because the wait is the viewer's, not the draft's.
2. **`composer/Composer.tsx`**:
   - `Composer` and `PostButton` take `slow?: { seconds: number; nextPostAt: number | null; exempt: boolean }`.
   - An effect on `slow.nextPostAt` sets `c.slowUntil.value = slow.nextPostAt`.
   - A hook `useCountdown(until: number | null)` in `composer/countdown.ts` (new) re-renders once a second with `setTimeout` until `until` passes, and returns the seconds left, or 0.
   - `PostButton` is disabled while `posting || !source.trim() || left > 0`, and passes `slow` to `postReply`.
   - Above the toolbar, when `slow.seconds > 0`, a `<p class="slow-mode" data-slow-mode>` shows the notice. When `left > 0`, a `<span id="slow-wait-{thread}" data-slow-wait>` shows the countdown.
   - `postFailure`'s `rate-limited` words stay as they are.
   - Two helpers, `slowModeText(seconds)` and `waitText(left)`, are exported for the tests.
3. **`composer/ComposerSheet.tsx`** passes `slow` through to `Composer` and to the `PostButton` it renders. **`screens/ThreadScreen.tsx`** `ThreadBody` builds `slow` from `page.viewer`: `{seconds: slow_mode_seconds, nextPostAt: next_post_at, exempt: can_moderate}`, and passes it to both.
4. **Moderator action.**
   - `shell/threadActions.ts`: TKT-01M3QGH66W's `moderate` option gains `slowMode: () => void`, which adds "Slow mode…" (id `slow-mode`).
   - `components/thread/ModerateDialog.tsx` gains the kind `slow-mode`. It is a radio group, "Slow mode", with Off (0), 10 seconds, 30 seconds, 1 minute, 5 minutes, and Custom. Custom shows a text field with `inputMode="numeric"`, labelled "Seconds between posts", taking a whole number from 0 to 86400 (`slowModeMax`, a constant commented as `threads.MaxSlowModeSeconds`).
   - The group is prefilled from `slow_mode_seconds`: the matching choice, or Custom with the value.
   - Save sends one `apiSend<SlowMode>('PUT', `/api/v1/admin/threads/${id}/slow-mode`, {seconds})`. On 200 the panel closes. The thread's held GETs are forgotten (`forgetGets((u) => u.startsWith(`/api/v1/threads/${id}`))`), and the page is asked for again with `res.retry()`, so the viewer block shows the new value.
   - Errors appear beside the field, with the panel left open:
     - `conflict`: "Slow mode is already set to that, or the thread is archived."
     - `validation-failed`: "Enter a whole number of seconds from 0 to 86400."
     - A Custom value that is not a whole number in range is refused before sending, with the same words.
     - Anything else gets `problemMessage`.
5. **CSS** in `composer/composer.css` for the notice and the countdown, and TKT-01M3QGH66W's panel styles for the dialog.
6. Run `go generate ./internal/web/spa`, commit `internal/web/spa/dist`, and check that `npm run build`'s budget line is under 60000 B.

### Tests

- **`composer/Composer.test.tsx`**, with `vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout', 'Date'] })` for the countdown cases:
  - `shows the slow mode notice`: 30 gives "Slow mode: one post every 30 seconds", 300 gives "every 5 minutes", 60 gives "every minute", 7200 gives "every 2 hours", 90 gives "every 90 seconds", and 0 shows no `[data-slow-mode]`.
  - `holds Send until next_post_at`: with `nextPostAt` 42 s ahead and a draft typed, Send is disabled and the countdown reads "0:42". After 41 s it is still disabled. After 42 s it is enabled and the countdown is gone, and no request was made.
  - `a moderator is never held`: with `exempt: true` and `seconds: 30`, a successful post leaves Send enabled for the next draft.
  - `a post starts the wait`: with `seconds: 30` and `exempt: false`, a successful post disables Send for the next draft for 30 s.
- **`composer/Post.test.tsx`** ("a refused post"): the `rate-limited` row with `Retry-After: 30` still keeps the source and says "post again in 30 seconds". It now expects Send to be disabled, and enabled again 30 s later under fake timers. The other rows are unchanged.
- **`composer/ComposerSheet.test.tsx`**: the sheet's Post is disabled while the wait holds, and closing and reopening the sheet keeps the wait.
- **`screens/ThreadScreen.test.tsx`**:
  - `a moderator sets slow mode`: role `moderator` with `can_moderate`. "Slow mode…" opens the panel prefilled from `slow_mode_seconds: 30` with "30 seconds" chosen. Choosing 5 minutes sends exactly one `PUT /api/v1/admin/threads/{id}/slow-mode` with `{"seconds":300}`, then asks for the page again, and the notice then reads "every 5 minutes".
  - `slow mode refusals stay beside the field`: 409 and 422 each show their words, and the panel stays open with the choice kept. A Custom value of 86401 sends nothing.
  - `a member is not offered slow mode`: no "Slow mode…" with role `member`, whether `can_moderate` is true (a group grant) or false.
- The thread-view bundle stays within 60000 B gzip (`npm run build`), and `TestTheAdminBundleIsSeparate` passes.
- **Server invariants** are TKT-01M29CRJ50's and stay green, because this ticket changes no server code. They include `TestSlowModeIsInTheViewerBlockNotTheCache`, which is the response-cache test for the per-viewer fields.

### Out of scope

- Any server change, including letting group-granted moderators set slow mode.
- The site-wide default's editor (TKT-01M29CXK2, Build the admin site settings screen).
- The overflow of a huge `slow_mode.default_seconds` (TKT-01M3R33SR5).
- Lock and pin, and the panel itself (TKT-01M3QGH66W). Rename and move (TKT-01M3QXV399).

## Notes

**agent:claude-code/phase2-groom** at 2026-09-30T04:04:40Z

Groomed 2026-09-30 (TKT-01M3M9RR26). Now depends on TKT-01M3QGH66W (Add lock, unlock, pin and unpin to the thread screen): the 'TKT-01M29CKRGS dialog pattern' the description names was never built on the client. The Retry-After words and the kept draft already exist (postFailure, Post.test.tsx), so criterion 3 now asks for the countdown after such an answer and names Post.test.tsx. Settled as an engineering choice: one slowUntil signal per composer, fed by next_post_at, Retry-After and the viewer's own post.
