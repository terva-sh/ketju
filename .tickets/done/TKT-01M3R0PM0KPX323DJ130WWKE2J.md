---
schema: 3
id: TKT-01M3R0PM0KPX323DJ130WWKE2J
title: Show slow mode in the composer and add the moderator's slow mode dialog
type: task
status: done
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
  - TKT-01M3RQP0TGP43KRWGW11XSN2T7
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-30T02:01:08Z
updated_at: 2026-09-30T14:36:29Z
created_by:
  id: agent:claude-code/phase2-run-crj50
  name: ""
updated_by:
  id: agent:claude-code/phase2-run-r0pm0k
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

- [x] With slow_mode_seconds > 0 the composer shows 'Slow mode: one post every N seconds', and with 0 it shows nothing (Composer.test.tsx)
- [x] While viewer.next_post_at is in the future Send is disabled with a countdown that re-enables it at next_post_at without a refetch, under fake timers (Composer.test.tsx)
- [x] The thread-view bundle stays within the 60000 B gzip budget and the dist from go generate ./internal/web/spa is committed
- [x] A rate-limited answer keeps the draft, says the wait from Retry-After, and holds Send until it passes, under fake timers (Post.test.tsx)
- [x] A successful post starts the wait for a viewer without can_moderate when slow_mode_seconds > 0, and a moderator is never held (Composer.test.tsx)
- [x] Mounting the composer keeps the later of its local deadline and the page's next_post_at, and null or an earlier value never shortens it: closing and reopening the phone sheet during the wait after a successful post keeps Send disabled until the wait ends (ComposerSheet.test.tsx 'reopening the sheet keeps the wait', Composer.test.tsx 'the page never shortens the wait')
- [x] 'Slow mode…' is offered whenever viewer.can_moderate is true, whatever the role, and never when it is false; it is prefilled from slow_mode_seconds, sends one PUT /api/v1/admin/threads/{id}/slow-mode {seconds}, asks for the page again, and shows conflict, validation-failed and forbidden beside the field (ThreadScreen.test.tsx 'a moderator sets slow mode', 'a group moderator sets slow mode', 'a member is not offered slow mode')
- [x] A next_post_at 40 days ahead holds Send from mount with the countdown reading 960:00:00, ticks once a second, and releases Send exactly at the deadline after vi.setSystemTime moves the clock near it, in at most ten timer callbacks (Composer.test.tsx 'a far wait holds Send')

## Implementation plan

Groomed 2026-09-30 for Phase 2 (TKT-01M3M9RR26), against main at 9a8fc76. Refreshed on 2026-09-30 against main at a660ae5 (5124209 plus the run's tracking commits). The server half is TKT-01M29CRJ50 (Implement per-thread slow mode), PR #188 at a827aef, with migration 0017 `thread_slow_mode`. Its summary and notes 1 to 3 were read for this plan. This ticket also depends on TKT-01M3RQP0TG (Let forum moderators by grant lock, pin, move and set slow mode), which moves the slow mode route to the `moderates` gate.

Two corrections to the description, read from main, both still true at a660ae5:
- **There is no moderator dialog on the thread screen to extend.** TKT-01M29CKRGS shipped lock and pin without a client. Its client half is TKT-01M3QGH66W (Add lock, unlock, pin and unpin to the thread screen, ready), which builds the `moderate` option of `shell/threadActions.ts`, the inline panel `components/thread/ModerateDialog.tsx`, and the `mayModerate` test. After its refresh of 2026-09-30, `mayModerate` is `viewer.can_moderate` alone. This ticket depends on it.
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
  - the page's `next_post_at`, on mount and whenever the page's value changes, keeping whichever deadline is later (Approach step 2);
  - a `rate-limited` answer's `Retry-After`, as `Date.now() + retryAfter * 1000`;
  - the viewer's own successful post, as `Date.now() + slow_mode_seconds * 1000`, when `slow_mode_seconds > 0` and the viewer does not moderate. The page's viewer block is not fetched again after a post, so without this Send would stay enabled into a certain refusal.

  `PostButton` is disabled while `Date.now() < slowUntil`. Holding the countdown in component state lost: the phone's sheet unmounts the composer when it closes, and the wait has to survive until it opens again, as the draft does.
- **The client's clock decides when Send comes back** (engineering choice). The server answers in absolute Unix ms, and the client has no server time. A client clock that runs fast re-enables Send early, and the post is then refused with `Retry-After`, which resets the countdown. That path exists anyway for a second tab. Estimating the skew from the `Date` header lost: it adds a moving part for a wait of seconds.
- **The words** (engineering choice, from the description).
  - The notice reads "Slow mode: one post every N seconds". N is written in minutes or hours when it divides evenly, and one unit reads "every minute", "every hour" or "every second".
  - The countdown reads "You can post again in m:ss", or "h:mm:ss" from an hour up, beside the disabled Send, which points to it with `aria-describedby`. It updates every second and is not a live region, so a screen reader is not interrupted each second.
  - The dialog's words are in Approach step 4.
- **The built client is committed.** Run `go generate ./internal/web/spa` and commit `internal/web/spa/dist`. Only one lane may have a PR open that changes the dist (docs/13 "Running two lanes").


Added in the refresh of 2026-09-30:
- **Who sets slow mode, from 2026-09-30. This supersedes "Only staff set slow mode" above.** The maintainer decided that thread moderation follows the forum grant: "Anyone holding `moderate` in a thread's forum, by role or through a group, may lock, unlock, pin, unpin and set slow mode there" (docs/08 "Decided 2026-09-30"). TKT-01M3RQP0TG puts `PUT /api/v1/admin/threads/{id}/slow-mode` behind `moderatesGate`, and the service decides `moderate` in the thread's forum. So "Slow mode…" is offered on `viewer.can_moderate` alone, the same check. The asymmetry noted above is gone: whoever is exempt from slow mode may also set it.
- **Long waits** (from TKT-01M3R33SR5 (Bound the post edit and delete windows before making Durations)). A thread's own value is at most 86400. `slow_mode.default_seconds` has no upper bound, though, and after TKT-01M3R33SR5 a huge default yields a `next_post_at` far in the future rather than null. `useCountdown` ticks with one-second `setTimeout`s and never sets one timer for the whole wait, because a browser fires a delay over 2^31 − 1 ms at once. `waitText` writes hours without an upper bound ("h:mm:ss").
- **The thread screen as it stands now.** PR #202 (TKT-01M3QQWA5Z) added `onFlag`, `mayFlag`, `PostMore` and `FlagDialog` to the posts, and `init.status` to `apiSend`. None of them touches the composer. `Composer` takes `{thread, authorOf, onPosted}` and `PostButton` takes `{thread, onPosted, label}`, as the approach assumes. The reader bundle is 47415 B of 60000 B gzip on main.

### Approach

1. **`state/composer.ts`**:
   - Add `slowUntil` to `Composer` and to `composerFor`'s new entries.
   - `postReply` takes an optional `slow?: { seconds: number; exempt: boolean }`. On success, when `seconds > 0 && !exempt`, it sets `slowUntil`. In the `catch`, a `rate-limited` answer with `retryAfter` sets `slowUntil` from it.
   - `discardReply` leaves `slowUntil` alone, because the wait is the viewer's, not the draft's.
2. **`composer/Composer.tsx`**:
   - `Composer` and `PostButton` take `slow?: { seconds: number; nextPostAt: number | null; exempt: boolean }`.
   - An effect on mount and on `slow.nextPostAt` reconciles, and never simply assigns: `c.slowUntil.value = later(c.slowUntil.value, slow.nextPostAt)`. `later` returns the larger of the two, and treats null as "no deadline". A deadline in the past counts as null. So a page value never replaces a later local deadline, and null never clears one. The page may be one fetched before the viewer's own post, whose viewer block still says null. Without this rule, closing and reopening the phone sheet remounts `Composer` and clears the wait (review 1540 on PR #203). A successful post and a `rate-limited` answer set the deadline directly (step 1). Both can happen only while Send is enabled, which means no local deadline is in force.
   - A hook `useCountdown(until: number | null)` in `composer/countdown.ts` (new) re-renders once a second with `setTimeout(…, 1000)` until `until` passes, and returns the seconds left, or 0.
   - `PostButton` is disabled while `posting || !source.trim() || left > 0`, and passes `slow` to `postReply`.
   - Above the toolbar, when `slow.seconds > 0`, a `<p class="slow-mode" data-slow-mode>` shows the notice. When `left > 0`, a `<span id="slow-wait-{thread}" data-slow-wait>` shows the countdown.
   - `postFailure`'s `rate-limited` words stay as they are.
   - Two helpers, `slowModeText(seconds)` and `waitText(left)`, are exported for the tests.
3. **`composer/ComposerSheet.tsx`** passes `slow` through to `Composer` and to the `PostButton` it renders. **`screens/ThreadScreen.tsx`** `ThreadBody` builds `slow` from `page.viewer`: `{seconds: slow_mode_seconds, nextPostAt: next_post_at, exempt: can_moderate}`, and passes it to both.
4. **Moderator action.**
   - `shell/threadActions.ts`: TKT-01M3QGH66W's `moderate` option gains `slowMode: () => void`, which adds "Slow mode…" (id `slow-mode`). It is set whenever `moderate` is, which is whenever `viewer.can_moderate`.
   - `components/thread/ModerateDialog.tsx` gains the kind `slow-mode`. It is a radio group, "Slow mode", with Off (0), 10 seconds, 30 seconds, 1 minute, 5 minutes, and Custom. Custom shows a text field with `inputMode="numeric"`, labelled "Seconds between posts", taking a whole number from 0 to 86400 (`slowModeMax`, a constant commented as `threads.MaxSlowModeSeconds`).
   - The group is prefilled from `slow_mode_seconds`: the matching choice, or Custom with the value. A site default above 86400 prefills Custom with that value, and Save refuses it before sending, as below.
   - Save sends one `apiSend<SlowMode>('PUT', `/api/v1/admin/threads/${id}/slow-mode`, {seconds}, { status: 200 })`. On 200 the panel closes. The thread's held GETs are forgotten (`forgetGets((u) => u.startsWith(`/api/v1/threads/${id}`))`), and the page is asked for again with `res.retry()`, so the viewer block shows the new value.
   - Errors appear beside the field, with the panel left open:
     - `conflict`: "Slow mode is already set to that, or the thread is archived."
     - `validation-failed`: "Enter a whole number of seconds from 0 to 86400."
     - `forbidden`: "You can no longer moderate this thread.", and the page is asked for again.
     - A Custom value that is not a whole number in range is refused before sending, with the `validation-failed` words.
     - Anything else gets `problemMessage`.
5. **CSS** in `composer/composer.css` for the notice and the countdown, and TKT-01M3QGH66W's panel styles for the dialog.
6. Run `go generate ./internal/web/spa`, commit `internal/web/spa/dist`, and check that `npm run build`'s budget line is under 60000 B. Record the change from 47415 B in the PR.

### Tests

- **`composer/Composer.test.tsx`**, with `vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout', 'Date'] })` for the countdown cases:
  - `shows the slow mode notice`: 30 gives "Slow mode: one post every 30 seconds", 300 gives "every 5 minutes", 60 gives "every minute", 7200 gives "every 2 hours", 90 gives "every 90 seconds", and 0 shows no `[data-slow-mode]`.
  - `holds Send until next_post_at`: with `nextPostAt` 42 s ahead and a draft typed, Send is disabled and the countdown reads "0:42". After 41 s it is still disabled. After 42 s it is enabled and the countdown is gone, and no request was made.
  - `a far wait holds Send` (review 1564 on PR #211): it runs a bounded number of ticks, never one advance across the whole wait, because each tick sets the next one-second timeout. With the clock at T0 and `nextPostAt` at T0 + 40 days, Send is disabled on mount and the countdown reads "960:00:00", so the deadline did not fire at once. After `vi.advanceTimersByTime(3_000)` it reads "959:59:57" and Send is still disabled.
    Then `vi.setSystemTime(T0 + 40 days - 5_000)` and `vi.advanceTimersByTime(1_000)`: it reads "0:04" and Send is still held, so the far deadline did not overflow into an early one. After `vi.advanceTimersByTime(4_000)` Send is enabled and the countdown is gone. At most ten timer callbacks run in the test.
  - `a moderator is never held`: with `exempt: true` and `seconds: 30`, a successful post leaves Send enabled for the next draft.
  - `a post starts the wait`: with `seconds: 30` and `exempt: false`, a successful post disables Send for the next draft for 30 s.
- **`composer/Post.test.tsx`** ("a refused post"): the `rate-limited` row with `Retry-After: 30` still keeps the source and says "post again in 30 seconds". It now expects Send to be disabled, and enabled again 30 s later under fake timers. The other rows are unchanged.
- **`composer/ComposerSheet.test.tsx`**, `reopening the sheet keeps the wait` (review 1540): with `seconds: 30`, `exempt: false` and the page's `next_post_at` null, a successful post from the sheet starts the wait. The sheet is closed and reopened 10 s later, and the page it is given still says `next_post_at: null`. Post is disabled with "0:20" and is enabled again 20 s later under fake timers.
- **`composer/Composer.test.tsx`**, `the page never shortens the wait`: `slowUntil` 30 s ahead, then a rerender with `nextPostAt` 5 s ahead, then with null. Send stays disabled until 30 s. A later `nextPostAt` of 60 s moves the wait to 60 s.
- **`screens/ThreadScreen.test.tsx`**:
  - `a moderator sets slow mode`: role `moderator` with `can_moderate`. "Slow mode…" opens the panel prefilled from `slow_mode_seconds: 30` with "30 seconds" chosen. Choosing 5 minutes sends exactly one `PUT /api/v1/admin/threads/{id}/slow-mode` with `{"seconds":300}`, then asks for the page again, and the notice then reads "every 5 minutes".
  - `a group moderator sets slow mode`: role `member` with `can_moderate: true` is offered "Slow mode…" and sends the same PUT.
  - `slow mode refusals stay beside the field`: 409, 422 and 403 each show their words, and the panel stays open with the choice kept. A Custom value of 86401 sends nothing.
  - `a member is not offered slow mode`: no "Slow mode…" with `can_moderate: false`, whatever the role.
- The thread-view bundle stays within 60000 B gzip (`npm run build`), and `TestTheAdminBundleIsSeparate` passes.
- **Server invariants** are TKT-01M29CRJ50's and TKT-01M3RQP0TG's and stay green, because this ticket changes no server code. They include `TestSlowModeIsInTheViewerBlockNotTheCache`, which is the response-cache test for the per-viewer fields, and `TestThreadModerationGate`.

### Out of scope

- Any server change. Who may set slow mode is TKT-01M3RQP0TG's.
- The site-wide default's editor (TKT-01M29CXK2, Build the admin site settings screen).
- The overflow of a huge `slow_mode.default_seconds` (TKT-01M3R33SR5).
- Lock and pin, and the panel itself (TKT-01M3QGH66W). Rename and move (TKT-01M3QXV399).


### Revised at claim (2026-09-30)

Read against main at 1ae02c5, which has #214 (the slow mode route on `moderatesGate`, re-checked at log length), #218 (the `secondsDuration` clamp: a huge setting keeps slow mode on), #224 (the lock and pin panel, `components/thread/ModerateDialog.tsx`, and the `moderate` option of `shell/threadActions.ts`), and #227 (reply autosave through `state/drafts.ts`). Everything above stands. Where the build departs from the Approach, this section says how and why.

- **The countdown lives in `state/composer.ts`, not in a component hook.** `Composer` gains `slowUntil` and `left` signals. `hold(c, until)` sets `slowUntil` and counts `left` down with one `setTimeout` a second, aligned to the deadline's second boundaries (`ms % 1000 || 1000`), and re-reads the clock on each tick. `holdAtLeast(thread, at)` is the page's path, and keeps the later deadline. Why: the phone's sheet renders two Post buttons for one composer (its head and the hidden footer), and a hook per button would run two timers. One timer per composer is "one countdown per composer" literally. Lost: `useCountdown` in `composer/countdown.ts`.
- **Reconciling with the page** is a layout effect in `Composer` on `[thread, nextPostAt]`, so Post is never painted enabled before the page's wait applies.
- **`Slow`** is `{seconds, nextPostAt, exempt}`, exported from `state/composer.ts`, and `postReply(thread, slow?)` takes it as planned. `discardReply` leaves the wait alone. `resetComposers` and a change of viewer clear the timers.
- **"Slow mode…" is an item of the existing `moderate` option.** `ModeratePanel = ModerateKind | 'slow-mode'`, and `open('slow-mode')` opens the same `ModerateDialog`. No `slowMode` callback and no second panel. The item follows the moderation kinds and comes before Copy link. It is offered whenever `moderate` is, meaning whenever `viewer.can_moderate`, whatever the lock, the pin or the role.
- **No `forgetGets` before asking for the page again.** The thread page's ETag hashes the body, viewer block included (`internal/service/threads/view.go` `finish`), so a changed `slow_mode_seconds` makes the revalidation a 200 with the new value, and `res.retry()` alone shows it. `useResource` keeps the page on screen while it is asked for.
- **Refusals follow the panel's existing rule.** A conflict or a refusal that withdraws access asks for the page again, as for lock and pin. A slow mode conflict leaves the form, because slow mode stays offered.
- **Words** (engineering choice): the panel's title is "Slow mode for this thread", the radio group's legend is "Time between posts", and the button is "Save". The countdown is a `<p class="slow-wait">`, not a span, and reads "You can post again in m:ss".
- **e2e** (#223's pattern: short tests, one axe scan each, stored sessions only): `e2e/thread-moderation.spec.ts` gains "a moderator sets slow mode and turns it off" and "the slow-mode panel passes the scan" in each theme. Turning slow mode off leaves a row at 0, which reads the same as the seed's default of 0, and the next run's 10 s is still a change from it.
- **Bundle**: 51214 B before, 52160 B after (+946 B). The panel stays in the reader bundle, where #224 put it; moving it into a lazy chunk is out of scope.

## Notes

**agent:claude-code/phase2-groom** at 2026-09-30T04:04:40Z

Groomed 2026-09-30 (TKT-01M3M9RR26). Now depends on TKT-01M3QGH66W (Add lock, unlock, pin and unpin to the thread screen): the 'TKT-01M29CKRGS dialog pattern' the description names was never built on the client. The Retry-After words and the kept draft already exist (postFailure, Post.test.tsx), so criterion 3 now asks for the countdown after such an answer and names Post.test.tsx. Settled as an engineering choice: one slowUntil signal per composer, fed by next_post_at, Retry-After and the viewer's own post.

**agent:claude-code/phase2-groom** at 2026-09-30T04:21:39Z

Review 1540 on PR #203 (head 22de825), medium, accepted: the composer overwrote slowUntil with the page's next_post_at on mount, so after a post the held page's null cleared the wait when the phone sheet reopened. The plan now keeps the later of the two and never lets null or an earlier value shorten the wait. New criterion 7, with the tests 'reopening the sheet keeps the wait' and 'the page never shortens the wait'.

**agent:claude-code/phase2-groom** at 2026-09-30T09:19:52Z

Plan refreshed against main at a660ae5 on 2026-09-30. What changed: (1) 'Slow mode…' is offered on viewer.can_moderate alone, by the maintainer decision of 2026-09-30 (docs/08) and TKT-01M3RQP0TG, which puts the route behind the moderates gate. The 'Only staff set slow mode' item is carried verbatim and marked superseded. Criterion 6 was replaced by criterion 7. The tests now include 'a group moderator sets slow mode', and 403 gets its own words. (2) For TKT-01M3R33SR5, a huge site default gives a far next_post_at. The countdown keeps one-second ticks, never one long timeout. New criterion 8 and test 'a far wait holds Send'. A default above 86400 prefills Custom, and Save refuses it before sending. (3) The two corrections to the description still hold at a660ae5. #202 did not touch the composer, the Composer and PostButton props are as the plan assumes, apiSend is called with status 200, and the bundle figure is 47415 of 60000 B.

**agent:claude-code/phase2-groom** at 2026-09-30T09:37:16Z

Review 1564 on PR #211 (head eea67a7), medium, accepted. 'a far wait holds Send' advanced fake timers by 2147483647 ms, and useCountdown's one-second reschedule would run about 2.1 million callbacks. Instead, the test now checks the mount reading (960:00:00), three ticks, and then vi.setSystemTime to 5 s before the deadline, with release exactly at it: at most ten callbacks. It still proves that a huge deadline neither fires at once nor overflows. This note supersedes the refresh note's description of the test. Criterion 8 was rewritten to match.

**agent:claude-code/phase2-run-r0pm0k** at 2026-09-30T14:33:23Z

Replayed onto main at f2eac74 before pushing (nothing had been pushed). This note supersedes two details in the plan's "Revised at claim" section.

- **The panel kind.** #231 had added `PanelKind = ModerateKind | 'rename' | 'move'`. Slow mode joins it as `'slow-mode'`, so `ModeratePanel` is gone. In the actions, Slow mode… comes after Move… and before Copy link.
- **The composer.** #235 moved the body into `composer/ComposerBody.tsx`. The notice, the countdown and any refusal now go into its `notice` slot. `Composer` and `PostButton` keep the `slow` prop. The new-thread composer is untouched: it has no thread, so it has no slow mode. The reply autosave from #227 is unchanged.
- **The timer.** `deadline.ts` (`delayUntil`, `maxTimeout`) arrived with #231. It is not used: the countdown never sets a timer longer than one second (`ms % 1000 || 1000`), so it never needs the cap.
- **The e2e tests.** The slow mode tests use mona's stored session and call no `/auth/*` route, so `e2e/session.ts` needs no `paceAuth` row for them.
- **The bundle.** The reader bundle went from 53877 B to 54811 B (+934 B), against the 70000 B budget from #234.

## Summary

Slow mode is now visible in the thread page's composer, and moderators can set it from the thread's moderation panel. Client only. No server change.

- **The composer** (`composer/Composer.tsx`, `state/composer.ts`):
  - It shows "Slow mode: one post every N seconds", with N in minutes or hours when they divide it evenly.
  - While the viewer must wait, Post is disabled and a countdown reads "You can post again in m:ss", or h:mm:ss for waits of an hour or more.
  - Each composer has one deadline. Three things set it: the page's `next_post_at`, where the later deadline always wins and null never shortens it; a `rate-limited` answer's `Retry-After`; and the viewer's own post, when they do not moderate the thread.
  - One timer per composer ticks each second, aligned to the deadline. So the wait survives the phone sheet closing, and a wait weeks long neither fires at once nor needs one long timeout.
- **The moderator action:**
  - "Slow mode…" is offered whenever `viewer.can_moderate` is true, in the same `ModerateDialog` panel as lock, pin, rename and move.
  - The panel offers Off, 10 s, 30 s, 1 min, 5 min or Custom, prefilled from `slow_mode_seconds`.
  - Saving sends one `PUT /api/v1/admin/threads/{id}/slow-mode` and then asks for the page again. The ETag covers the viewer block, so the new value shows.
  - `conflict`, `validation-failed` and `forbidden` are shown next to the field. A Custom value outside 0 to 86400 is refused before anything is sent.
- **Tests:** unit tests in Composer, Post, ComposerSheet, ThreadScreen and threadActions. The e2e spec `e2e/thread-moderation.spec.ts` has "a moderator sets slow mode and turns it off", plus one axe scan per theme of the slow mode panel, at 390 px and 1440 px.
- **Bundle:** the reader bundle went from 53877 B to 54811 B (+934 B) of 70000 B, and the regenerated dist is committed.
