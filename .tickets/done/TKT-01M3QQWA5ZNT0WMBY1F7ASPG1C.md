---
schema: 3
id: TKT-01M3QQWA5ZNT0WMBY1F7ASPG1C
title: Add the flag item and dialog to the post menu
type: task
status: done
status_reason: null
priority: high
due_on: null
labels:
  - area:moderation
  - area:client
  - effort:M
assignees: []
milestone: phase-2
parent: TKT-01M29BN36JH4M1W5819D06PGPP
origin: TKT-01M29CP2PJ23SK6AN8M0CJY4JT
dependencies:
  - TKT-01M29CP2PJ23SK6AN8M0CJY4JT
blocks_on: none
references:
  - ref: design:flags
    path: docs/08-moderation.md
  - ref: pr:202
    path: null
claim: null
archive: null
created_at: 2026-09-29T23:26:57Z
updated_at: 2026-09-30T04:30:13Z
created_by:
  id: agent:claude-code/phase2-run-cp2pj
  name: ""
updated_by:
  id: agent:claude-code/phase2-run-qqwa5z
  name: ""
extensions: {}
---

## Description

The client half of TKT-01M29CP2PJ (Implement flags with the auto-hide threshold), filed from it because the server lane may not change `internal/web/spa/dist` while another lane holds it.

The server route exists: `POST /api/v1/threads/{id}/flags` with `{ordinal, reason, note}` answers 200 `FlagReceipt` `{"received": true}` (docs/07). `dto.ts` already declares `FlagReceipt`.

### What to build

- A "Flag" item in the post menu (`components/thread/Post.tsx`) for a signed-in member, on a post that is shown (not `deleted`).
- A dialog offering the four reasons (spam, abuse, off-topic, other) and a note, required for other and at most 1000 characters.
- On success it says "Thanks, a moderator will look." and nothing else (docs/08 "Flags"). It never shows a count or an outcome.
- A `validation-failed`, `conflict` or `unavailable` answer shows a short message. `unavailable` still means the report was kept.
- Regenerate `dist` with `go generate ./internal/web/spa` and keep the bundle under budget.

The tombstone text for `removal: "review"` ("Post hidden pending review", and "hidden pending review" for the author and moderators) is TKT-01M29CKRJ9's client half, not this ticket.

## Acceptance criteria

- [x] The dialog offers spam, abuse, off-topic and other, needs a non-blank note for other, refuses a note over 1000 code points unsent, and sends exactly {ordinal, reason, note} to POST /api/v1/threads/{id}/flags once per press (FlagDialog.test.tsx)
- [x] At 390 px link, flag and history sit under the post footer's more control, and e2e/targets.spec.ts passes for it and its items
- [x] The dist from go generate ./internal/web/spa is committed and the thread-view bundle stays under threadViewGzipBytes in budget.json
- [x] A signed-in member sees flag in the footer of another member's shown post, and not on their own post, a deleted or moved post, nor for a signed-out reader (Post.test.tsx)
- [x] A 422, 409, 403, 404 or 429 shows its sentence and keeps the form filled (FlagDialog.test.tsx)
- [x] Only a 200 shows "Thanks, a moderator will look.", with no count or outcome and no second request for the thread page; a 503 unavailable says the report could not be sent and keeps the form (FlagDialog.test.tsx, ThreadScreen.test.tsx)
- [x] A kept flag whose auto-hide fails answers 200 {"received":true}, the error is logged, and the sweep hides the post; a flag on a quarantined thread answers 503 with no flags row (TestAutoHideRecoversFromAFailedAppend, TestAFailedHideAnswersTheReceipt, TestAQuarantinedThreadKeepsNoFlag)
- [x] The dialog is modal: focus starts inside, Tab wraps, and Escape and Close close it. Focus then returns to the returnTo control the post handed over: the flag button on desktop, and the post's visible More button on a phone, never document.body (FlagDialog.test.tsx; ThreadScreen.test.tsx "flags a post from the page" and "flags a post from the phone's More")
- [x] The server refuses a self-flag with 403 forbidden and writes no flags row, on the author's shown post and on their own hidden post. On the shown post another member's flag is accepted, and on the hidden post another member with no open flag gets conflict, as on main (TestFlagRefusesTheAuthor, TestFlagRouteRefusals)

## Implementation plan

Groomed 2026-09-30 for Phase 2 (TKT-01M3M9RR26), against main at 43a844d. The server half is merged: TKT-01M29CP2PJ (Implement flags with the auto-hide threshold), PR #176. The tombstone words for `removal: "review"` are already in `components/thread/removal.tsx` (TKT-01M29CKRJ9).

Revised 2026-09-30 at claim (agent:claude-code/phase2-run-qqwa5z), against main at 052db1d. Of the files the plan names, only `internal/service/threads/flags.go` changed since grooming, in the flag-lock comment and the moderator queue's locked restore (TKT-01M29CRJ4E), not in `Flag`, so the approach stands; three details below change with what building it found, each marked "Revised at claim". The settled items are carried unchanged.

### Settled in grooming

- **The route is TKT-01M29CP2PJ's** (docs/07 route table, `POST /threads/{id}/flags`). It takes `{ordinal, reason, note}` and needs a member session and read access. `reason` is `spam`, `abuse`, `off-topic` or `other`. `other` needs a note, and a note is at most 1000 characters (`threads.MaxFlagNote`, counted in code points, `utf8.RuneCountInString`). The answer is 200 `FlagReceipt` `{"received": true}`, `no-store`, and charged to the write limiters. An ordinal the thread lacks is `not-found`. A post in an archived thread is `conflict`, and so is a post already removed or moved, unless the member's own flag on it is still open.
- **A reporter sees only the acknowledgement** (docs/08 "Flags": "Reporters see 'thanks, a moderator will look' and nothing else"; TKT-01M29CP2PJ). No count, no reporter, no outcome.
- **A member cannot flag their own post** (maintainer decision 2026-09-29, relayed by the Phase 2 coordinator on PR #181). The client does not offer flag on the viewer's own posts, and the server refuses a self-flag. The server half is new work in this ticket (Approach step 2). Filing it as a separate server ticket lost: it is one check in `threads.Service.Flag` and one test, and splitting it would add a second PR and a dependency for no gain.
- **A member cannot flag the same post twice** (docs/08 "Flags", `flags_one_open_per_reporter`). A repeat adds no row and gets the same receipt, even after the post is hidden, so a repeat does not reveal the hide. Once a moderator closes their flag, the member may flag the post again.
- **Auto-hide** (maintainer decisions 2026-09-28 and 2026-09-29, docs/08 "Flags"). At N open flags, 3 by default and a site setting, the post is hidden pending review. Its author and moderators still see it, marked "hidden pending review". This ticket draws none of that; `removal.tsx` already does.
- **Where flag sits** (docs/06 "Post anatomy" and "Mobile"). On desktop, the footer reads `quote · reply · link · flag · history`. On a phone, link, flag and history collapse under a "more" control, and every control is at least 44 px on its shorter side. The reply expander's copies carry no actions (docs/06), so they get no flag.
- **The built client is committed.** Run `go generate ./internal/web/spa` and commit `internal/web/spa/dist`. Only one lane may have a PR open that changes the dist (docs/13 "Running two lanes"). `FlagReceipt` is already in `web/client/src/api/dto.ts` and `dto-contract.ts`.

Engineering choices made here, each with the alternative that lost:
- **A self-flag is `forbidden`.** The check reads the post's author from the thread index (`IndexEntry.Author`) inside the `Threads.With` callback that already reads the entry, and runs before the shown and repeat logic, so a member's own post is refused whether it is shown, hidden or already flagged by them. `conflict` lost: it means the resource's state stopped the change, and a later state would allow it, which is never true here. `validation-failed` lost: nothing in the body is wrong.
- **A kept flag always answers 200, and `unavailable` means nothing was kept** (Terva review 1516 on PR #181, finding 3). On main (PR #176), `threads.Service.Flag` commits the flag and then calls `enforceAutoHide`. When that hide fails, it answers `unavailable` with the flag kept. A quarantined thread also answers `unavailable`, from `threads.access`, before anything is written. The client therefore cannot tell "kept" from "not kept", and different words for the failed hide would tell the reporter that the post had reached the threshold, which docs/08 forbids.
  - This ticket changes the server so that a failed hide after the commit answers 200 `FlagReceipt`, like every other kept flag. `AutoHideSweep` hides the post within a minute, as it already does for this case.
  - The hide's error is not dropped. `Flag` returns the receipt wrapped with a new sentinel, `threads.ErrHideDeferred`, and the handler logs it at warn and answers 200.
  - After the change, `unavailable` comes only from paths that write nothing: `access` on a quarantined thread, before the insert.
  - The dialog shows "Thanks, a moderator will look." only on 200. On `unavailable` it says "Your report could not be sent. Try again shortly." and keeps the form.
  - Three alternatives lost. Showing the thanks on every `unavailable` would tell a member a report was received when none was. A distinct code for the failed hide would reveal the threshold. A logger field on `threads.Service` would be new plumbing, since the service has no logger today, while the handler already has one.
- **A flag does not refetch the thread page.** Revalidating after a flag could show the reporter their post turning into a tombstone, which is the outcome. The page updates on the reader's next load, as it would for anyone. Refetching to show a fresh page lost for that reason.
- **The client keeps no record of what it flagged.** The server makes a repeat harmless and answers it identically, and a member may flag again once a moderator closes their flag. A stored "already flagged" mark would be wrong after that, and it would reveal nothing the member does not know. Hiding flag after a report, in memory or in `localStorage`, lost.
- **Flag is offered from the page's viewer, not from a new server field.** It shows when `viewer.value.user` is not null, which holds only for an active member's session (the session chokepoint refuses anyone else, and a pre-user has `user: null`). It also needs the post to be neither `deleted` nor moved. Archived threads are not known to the page, so an archived thread's `conflict` gets words. A `can_flag` on `ThreadViewer` lost: it would change a cached DTO and its viewer-class key for a case the `conflict` words already cover.
- **The dialog is a modal in the thread view's main chunk,** `components/thread/FlagDialog.tsx`, following `HelpOverlay`: `role="dialog"`, `aria-modal`, `trapTab` and Escape. Focus goes back to a return target the dialog is given (next item), not to whatever was focused when it opened. The thread view is at 45573 B of the 60000 B gzip budget on main. A lazy chunk lost: the dialog is small, and a lazy chunk would make the first press wait on a fetch.
- **The dialog is told where focus returns** (Terva review 1525 on PR #181, finding 1). On a phone, flag sits inside `PostMore`, which closes when an item is chosen, so the flag item is gone by the time the dialog closes. So `Post` passes the control that stays visible with the ordinal: `onFlag(ordinal, returnTo)`. On desktop that is the flag button itself. On a phone it is the post's More button, whose ref `PostMore` holds and hands to its flag item. `ThreadBody` keeps `{ordinal, returnTo}`, and `FlagDialog` takes `returnTo` as a prop. On unmount it focuses `returnTo` if it is still connected (`isConnected`) and visible (`offsetParent !== null`). Otherwise it focuses the post's `<article id="post-N">`, which is `tabIndex={-1}`, and otherwise nothing. Two alternatives lost. Capturing `document.activeElement` at mount, as `HelpOverlay` does, returns focus to the phone's flag item, which no longer exists, so focus falls to the body. Looking the target up by selector when the dialog closes would tie the dialog to `Post`'s markup.
- **The phone's "more" is a disclosure**, `components/thread/PostMore.tsx`: a button with `aria-expanded` and `aria-controls` opening a short list of the real `link` and `history` anchors and the flag button. Reusing `shell/MoreMenu.tsx` lost, because it takes only `ShellAction`s run as buttons, and link and history must stay anchors so they can be opened in a new tab or copied. Adding flag to the phone footer as a sixth inline control lost: `.post-footer` does not wrap, and at 390 px it would overflow with 44 px targets. The phone layout is chosen with `isPhone` (`state/layout`), as `Shell` and `ThreadBody` choose it.

### Approach

1. **Server: a kept flag answers the receipt.**
   - `internal/service/threads/flags.go`: `var ErrHideDeferred = errors.New(…)`. In `Flag`, when `enforceAutoHide` fails after the insert, return `dto.FlagReceipt{Received: true}` and `fmt.Errorf("%w: …: %w", ErrHideDeferred, err)` in place of `problem.CodeUnavailable`. The doc comment says that `unavailable` now means nothing was written.
   - `internal/web/api/flags.go`, `flag`: on `errors.Is(err, threads.ErrHideDeferred)`, log at warn with the thread and ordinal, then write the 200 receipt `no-store` as for success. Any other error goes to `h.fail` as now.
   - docs/07's `POST /threads/{id}/flags` row and docs/08 "Flags" ("If the hide cannot be appended, the reporter's request answers `unavailable`") change to: the flag is kept, the answer is the receipt, and the sweep hides the post within a minute.
2. **Server: refuse a self-flag.**
   - `internal/service/threads/flags.go`, `Flag`: inside the `s.Threads.With` callback, after `th.Index.Entry(req.Ordinal)` succeeds, return `problem.Coded(problem.CodeForbidden, …)` when `e.Author == uint64(v.UserID)`. The doc comment on `Flag` gains the rule.
   - No migration. Open flags written before this change are left as they are.
   - docs/07's `POST /threads/{id}/flags` row and docs/08 "Flags" say that a member cannot flag their own post (`forbidden`), citing the maintainer decision of 2026-09-29.
3. **`components/thread/Post.tsx`**:
   - `Post` gains `onFlag?: (ordinal: number, returnTo: HTMLElement) => void`, separate from `PostActions`, because flag does not depend on `can_reply`: a locked thread still takes flags.
   - A helper `mayFlag(post)` is true when `!post.deleted && post.moved_to === null` and the post is not the viewer's own: `post.author.handle` differs from `viewer.value.user.handle`, compared case-insensitively as handles are.
   - On desktop, the footer renders `<button type="button" class="hit" data-post-action="flag">flag</button>` between link and history when `onFlag && mayFlag(post)`.
   - On a phone, it renders the reply expander, quote and reply, then `PostMore`, holding link, flag (under the same condition) and history.
   - The comment above `Post` that says flag arrives with its own ticket is replaced.
4. **`components/thread/PostMore.tsx`**: a "more" button (`aria-label="More actions for post #N"`, `aria-expanded`, `aria-controls`, `data-post-more`) and a list. Escape, a press outside, or choosing an item closes it. Escape returns focus to the button. Every item is a full target (Revised at claim: each list item is drawn at least `var(--target)` high and the list's width, so `.hit`'s padding would add nothing; the button itself carries `.hit`). The flag item closes the list and calls `onFlag(ordinal, moreButton)` with the button's ref, so the dialog returns focus to the button, which stays on screen.
5. **`components/thread/FlagDialog.tsx`**, with props `{thread: string, ordinal: number, returnTo: HTMLElement, onClose: () => void}`. On unmount, focus goes to `returnTo` when it is connected and visible, or else to `#post-{ordinal}`.
   - The title is "Flag post #N".
   - A radio group "Why?" offers Spam, Abuse, Off-topic and Other, with values `spam`, `abuse`, `off-topic` and `other`. None is chosen at first.
   - A textarea "Note", with the hint "Required for Other. At most 1000 characters." and a live count of code points (`[...note].length`). It has no `maxlength`, which counts UTF-16 units, not what the server counts.
   - Send stays disabled until a reason is chosen, and for Other until the trimmed note is non-empty. A note over 1000 code points shows the words beside the field and is not sent. While a request is in flight, Send is disabled, so a double press sends once.
   - Send calls `apiSend<FlagReceipt>('POST', '/api/v1/threads/{id}/flags', {ordinal, reason, note})` with the note untrimmed, as typed.
   - 200 replaces the form with "Thanks, a moderator will look." and a Close button. Nothing else is shown.
   - `unavailable` says "Your report could not be sent. Try again shortly." and keeps the form filled, because the server wrote nothing.
   - `validation-failed` says "Choose a reason, and say what is wrong when it is Other."
   - `conflict` says "This post can no longer be flagged. It may have been removed or moved, or its thread archived."
   - `forbidden` says "You cannot flag this post. It is your own, or you no longer have access to it." The client does not offer flag on the viewer's own post, so this is reached only by a race such as a rename. Revised at claim: `threads.access` also answers `forbidden` when read access to the thread was withdrawn after the page loaded, so "You cannot flag your own post." alone would be untrue there.
   - `not-found` says "This post is no longer here."
   - Anything else (`rate-limited`, `unauthenticated` and the rest) gets `problemMessage` from `api/problem.ts`.
   - On a refusal the form stays as filled.
6. **`screens/ThreadScreen.tsx`, `ThreadBody`**: `const [flagging, setFlagging] = useState<{ordinal: number; returnTo: HTMLElement} | null>(null)`. Pass `onFlag={viewer.value.user ? (ordinal, returnTo) => setFlagging({ordinal, returnTo}) : undefined}` to each `Post`, and render `{flagging && <FlagDialog thread={thread.id} ordinal={flagging.ordinal} returnTo={flagging.returnTo} onClose={() => setFlagging(null)} />}`. `Tombstone`, `PostPopover` and the reply expander's copies get no `onFlag`.
7. **CSS** in `components/thread/thread.css`: the dialog and backdrop, reusing the `.help-overlay` tokens, and the `PostMore` list. Every target is at least `var(--target)`.
8. Run `go generate ./internal/web/spa`, commit `internal/web/spa/dist`, and check that `npm run build`'s budget line stays under 60000 B.

### Tests

- **`components/thread/Post.test.tsx`** (new):
  - `a member sees flag on a shown post`: with `onFlag`, the footer has `[data-post-action=flag]`, and pressing it calls `onFlag(ordinal, thatButton)`.
  - `no flag on a removed or moved post`: absent for `deleted: true` (the moderator's and author's view of a removed post) and for `moved_to` set.
  - `no flag without onFlag`: absent when the page passes none, as for a signed-out reader.
  - `no flag on the viewer's own post`: absent when `post.author.handle` is the viewer's, in any case.
  - `the phone footer collapses`: with `isPhone` true, link, flag and history are inside the `PostMore` list and not in the footer row. Opening it sets `aria-expanded`, and Escape closes it and focuses the button. Choosing flag closes the list and calls `onFlag(ordinal, moreButton)`.
- **Server:**
  - `TestFlagRefusesTheAuthor` in `internal/service/threads/flags_test.go`. The author flagging their own post is `forbidden` and adds no `flags` row. This holds for a shown post and for their own post that is already hidden, because the check runs inside the `Threads.With` callback before the shown-and-repeat logic. It does not count toward the auto-hide. On the shown post, another member's flag is still accepted (the receipt, and one row).
    - Read from main (Terva review 1525 on PR #181, finding 2): for a post that is not shown, `Flag` answers the receipt only to a reporter who already has an open flag on it (`HasOpenFlag`), and `conflict` to anyone else. This ticket does not change that. The test asserts it as it is: on the author's hidden post, another member with no open flag gets `conflict` and no row is written. `TestFlagRefusals` already holds the general case.
  - `TestFlagRouteRefusals` in `internal/web/api/flags_test.go` gains a row: a self-flag through the route answers 403 `forbidden`.
  - `TestAutoHideRecoversFromAFailedAppend` (`internal/service/threads/flags_test.go`) changes: the threshold-crossing flag whose hide fails returns the receipt with an error matching `ErrHideDeferred`, not `unavailable`. It still asserts that 3 flags are open and the post is visible, and that the sweep then hides it.
  - `TestAFailedHideAnswersTheReceipt` (new, `internal/web/api/flags_test.go`): the same case through the route answers 200 with the body exactly `{"received":true}`, the same body a first flag gets.
  - `TestAQuarantinedThreadKeepsNoFlag` (new, `internal/web/api/flags_test.go`): a flag on a quarantined thread answers 503 `unavailable`, and the `flags` table has no row for it.
- **`components/thread/FlagDialog.test.tsx`** (new), with `fetch` stubbed:
  - `sends one of the four reasons`: the body is exactly `{"ordinal":4,"reason":"spam","note":""}`.
  - `other needs a note`: Send is disabled with Other and an empty or blank note, and enabled once a note is typed.
  - `the note is counted in code points`: `'😀'.repeat(1000)` is sent, and `'😀'.repeat(1001)` is refused with nothing sent.
  - `the receipt says thanks and nothing else`: after a 200, the dialog's text is exactly "Thanks, a moderator will look." plus Close.
  - `unavailable is not a receipt`: a 503 `unavailable` shows "Your report could not be sent. Try again shortly.", no thanks, and the form still filled.
  - `refusals are said in words`: 422, 409, 403, 404 and 429 each show their sentence, with the form still filled.
  - `sends once`: two quick presses make one request.
  - `is a modal`: focus starts inside, Tab from the last control wraps to the first, and Escape and Close call `onClose`.
  - `returns focus where it is told`: on unmount, focus goes to `returnTo`. When `returnTo` has been removed from the document, focus goes to `#post-N`.
- **`screens/ThreadScreen.test.tsx`**:
  - `flags a post from the page`: a signed-in viewer presses flag on post 3, the dialog opens titled "Flag post #3", and the request goes to `/api/v1/threads/{id}/flags`. After the receipt, no second request for the thread page is made. When the dialog closes, focus is back on post 3's flag control (desktop).
  - `flags a post from the phone's More` (review 1525): with `isPhone` true, the viewer opens post 3's More, presses flag, and the list closes. The dialog opens, and when it is closed with Close and, separately, with Escape, focus is on post 3's More button (`[data-post-more]`, `aria-expanded="false"`), not on `document.body`.
  - `a signed-out reader has no flag`.
- **e2e:** `e2e/targets.spec.ts` covers the phone's "more" control and its items at 390 px. `e2e/replies.spec.ts`, or a new `e2e/flag.spec.ts`, flags a post at 390 px and 1440 px and sees the thanks. Revised at claim: a new `e2e/flag.spec.ts`, so that the Phase 2 exit lane's edits to existing specs do not collide; and the open list covers the post below it, so `targets.spec.ts`'s `small` takes an optional root and measures only the list while it is open. On the phone a radio is drawn in a 44 px box, because the check presses 21 px beside each input and the label is not the input's hit area.
- **Invariants held by the server's tests, which stay green:**
  - Only the acknowledgement reaches the reporter: `TestReporterSeesOnlyTheReceipt`.
  - A repeat adds no row: `TestDuplicateFlagDoesNotCount`.
  - Refusals: `TestFlagRouteRefusals` and `TestFlagRefusals`.
  - The auto-hide moves the response cache key: `TestAutoHideMovesTheResponseCacheKey`. This ticket changes no cached DTO and no viewer class. The flag answer is `no-store`, and a POST is never held by `peekGet`.
  - No session is issued.

### Out of scope

- The moderator queue's flag list, dismissal and "keep hidden" (TKT-01M29CRJ4E).
- The "hidden pending review" words and tombstone, which are already on main (TKT-01M29CKRJ9).
- Editing the auto-hide threshold: the admin site settings screen (TKT-01M29CXK2).
- The HTML form route `POST /t/{id}/posts/{ordinal}/flag`, which is Phase 5 (docs/07).
- Moderator actions in the post footer, such as hide and restore.

## Notes

**agent:claude-code/phase2-groom** at 2026-09-30T00:29:01Z

Planned in Phase 2 grooming (TKT-01M3M9RR26), 2026-09-30, against main at 43a844d. Open decision, not taken here: is flag offered on the viewer's own post? (a) yes, as the server allows; (b) no. The grooming recommends (b). Two departures from the description, both for confidentiality under docs/08 'Reporters see thanks and nothing else'. (1) unavailable shows the same thanks, because only a flag at the auto-hide threshold can get it, so separate words would reveal that others flagged the post. (2) A flag does not refetch the page, which could show the reporter the hide. The phone's post footer gains the 'more' control docs/06 'Mobile' asks for, holding link, flag and history, because a sixth inline control overflows at 390 px. Effort raised to M for that.

**agent:claude-code/phase2-groom** at 2026-09-30T00:37:39Z

Maintainer decision 2026-09-29, relayed by the Phase 2 coordinator on PR #181. It supersedes the open decision in the previous note: a member cannot flag their own post. The client does not offer flag on the viewer's own posts, and the server refuses a self-flag. The server half is added to this ticket's plan as step 0 and is not filed separately, because it is one check in threads.Service.Flag: forbidden, read from IndexEntry.Author. It is tested by TestFlagRefusesTheAuthor and a new row in TestFlagRouteRefusals. The open decision is removed from the plan, and the criteria are rewritten.

**agent:claude-code/phase2-groom** at 2026-09-30T00:52:13Z

Terva review 1516 on PR #181 (head 9fc6307), finding 3 (medium), accepted. This supersedes the earlier choice that unavailable reads as the receipt. On main, Flag answers unavailable both for a kept flag whose auto-hide failed and for a quarantined thread, where nothing was written. The plan adds a server step: a kept flag whose hide fails answers 200 FlagReceipt, like any accepted flag, the error is logged through threads.ErrHideDeferred, and the sweep hides the post within a minute. So unavailable means nothing was kept, and the threshold is not revealed. The client shows the thanks only on 200, and 'Your report could not be sent. Try again shortly.' on unavailable. docs/07 and docs/08 change with it. New tests: TestAFailedHideAnswersTheReceipt and TestAQuarantinedThreadKeepsNoFlag. TestAutoHideRecoversFromAFailedAppend changes.

**agent:claude-code/phase2-groom** at 2026-09-30T01:34:38Z

Terva review 1525 on PR #181 (head 8bcf19f), two medium findings, both accepted. (1) On a phone, flag sits in PostMore, which closes when an item is chosen, so focus could not return to the flag item. The dialog is now told where to return: Post calls onFlag(ordinal, returnTo), with the flag button on desktop and the More button's ref on a phone. ThreadBody keeps it, and FlagDialog focuses it on unmount if it is still connected and visible, or else the post's article. Capturing document.activeElement at mount lost. New tests: 'flags a post from the phone's More' and 'returns focus where it is told'. (2) The self-flag criterion claimed another member's flag on the author's hidden post is accepted. On main, Flag answers conflict for a post that is not shown, unless that reporter already has an open flag (HasOpenFlag). The acceptance case is narrowed to the shown post, and the hidden-post case asserts conflict. No route change. Criteria 2 and 6 are replaced.

**agent:claude-code/phase2-run-qqwa5z** at 2026-09-30T03:47:34Z

Built on run/qqwa5z-flag-dialog from main at 052db1d. Three departures from the plan, each recorded in the revised plan as "Revised at claim".

- The forbidden sentence is "You cannot flag this post. It is your own, or you no longer have access to it." threads.access also answers forbidden when read access to the thread was withdrawn after the page loaded. The plan's "You cannot flag your own post." would be untrue in that case, and the client cannot tell the two apart from the problem type.
- The PostMore list items do not carry .hit. Each is drawn at least var(--target) high and as wide as the list, so the padding would add nothing. The More button carries .hit.
- The e2e flow is a new e2e/flag.spec.ts, not an edit to replies.spec.ts, to keep clear of the Phase 2 exit lane's spec edits. Alice flags carol's #3 at both widths, and the second width is a repeat that gets the same receipt. One flag is under the threshold, so nothing on the page changes for other specs. targets.spec.ts's small() takes an optional root, because the open list covers the post below it. On the phone a radio is a 44 px box (appearance: none, the circle drawn by a gradient), because the target check presses 21 px beside each input.

Evidence: go test -race ./... is green, and so are vitest (48 files, 1475 tests), tsc --noEmit -p . and Playwright (70 passed, 10 skipped by width). The thread view is 47363 B gzip, up from 45724 B on main (+1639 B, 78.9% of 60000 B). Each commit body lists the mutations tried against the tests; each was caught.

**agent:claude-code/phase2-run-qqwa5z** at 2026-09-30T04:10:52Z

Terva review 1538 on PR #202 (head 9686acb, base 9a8fc76) raised one medium finding, "Show the thanks only after an HTTP 200". It is accepted and fixed. apiSend resolves on any 2xx, so a 204 or any other 2xx body would have shown the thanks. FlagDialog now shows the thanks only for the receipt, {"received": true}, which is what the server answers a kept flag with. Anything else counts as a server fault (internal): it gets its sentence and the form stays filled. New FlagDialog.test.tsx cases cover 204, 201 with {}, 200 with {} and 200 with {received: false}. CI run 1294 on 9686acb was green.

**agent:claude-code/phase2-run-qqwa5z** at 2026-09-30T04:30:13Z

Terva review 1544 on PR #202 (head 11b5326, base 23f4cc8) raised two medium findings. Both are accepted and fixed.
(1) "Require HTTP 200 before showing the thanks". This is review 1538's finding carried forward: a 201 carrying the receipt would still have been thanked. apiSend now takes an optional status, and any other 2xx is internal. FlagDialog passes status 200 and still requires {"received": true}. Tests: client.test.ts "refuses a success other than the status asked for", and FlagDialog cases for 201 and 202 carrying the receipt.
(2) "Check post ownership before rejecting archived threads". This is on the server self-flag, so it is treated as security. In Flag, the author check now runs on the index entry before the archived check, so the author's own post in an archived thread is forbidden. Every other answer keeps its order from main: an archived thread is conflict for anyone else, even for an ordinal it lacks. TestFlagRefusesTheAuthor gains the archived case. docs/07's row says the same.
Mutations tried: dropping status 200 fails two FlagDialog cases; putting the archived check first fails TestFlagRefusesTheAuthor. The thread view is 47415 B gzip. CI run 1306 on 11b5326 was green.

## Summary

Flag is in the post footer for a signed-in member on another member's shown post: between link and history on desktop, and under the new PostMore disclosure on a phone. FlagDialog sends {ordinal, reason, note} once per press, says the thanks only on 200, words each refusal with the form kept, and returns focus to the control the post handed over. On the server, threads.Service.Flag refuses a self-flag with forbidden, and a kept flag whose auto-hide fails answers the receipt beside threads.ErrHideDeferred, which the handler logs at warn, so unavailable now means nothing was kept. docs/07 and docs/08 say both. The dist is regenerated: the thread view is 47363 B gzip (+1639 B).
