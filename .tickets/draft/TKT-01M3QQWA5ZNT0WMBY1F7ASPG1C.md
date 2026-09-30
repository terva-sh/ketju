---
schema: 3
id: TKT-01M3QQWA5ZNT0WMBY1F7ASPG1C
title: Add the flag item and dialog to the post menu
type: task
status: draft
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
claim: null
archive: null
created_at: 2026-09-29T23:26:57Z
updated_at: 2026-09-30T00:29:01Z
created_by:
  id: agent:claude-code/phase2-run-cp2pj
  name: ""
updated_by:
  id: agent:claude-code/phase2-groom
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

- [ ] A signed-in member sees flag in the footer of a shown post, and not on a deleted or moved post, nor for a signed-out reader, nor on their own post if the open decision picks (b) (Post.test.tsx)
- [ ] The dialog offers spam, abuse, off-topic and other, needs a non-blank note for other, refuses a note over 1000 code points unsent, and sends exactly {ordinal, reason, note} to POST /api/v1/threads/{id}/flags once per press (FlagDialog.test.tsx)
- [ ] A 200 and a 503 unavailable both show only "Thanks, a moderator will look." with no count or outcome, and the thread page is not requested again (FlagDialog.test.tsx, ThreadScreen.test.tsx)
- [ ] A 422, 409, 404 or 429 shows its sentence and keeps the form filled (FlagDialog.test.tsx)
- [ ] The dialog is modal: focus starts inside, Tab wraps, Escape and Close close it, and focus returns to the post's flag control (FlagDialog.test.tsx, ThreadScreen.test.tsx)
- [ ] At 390 px link, flag and history sit under the post footer's more control, and e2e/targets.spec.ts passes for it and its items
- [ ] The dist from go generate ./internal/web/spa is committed and the thread-view bundle stays under threadViewGzipBytes in budget.json

## Implementation plan

Groomed 2026-09-30 for Phase 2 (TKT-01M3M9RR26), against main at 43a844d. The server half is merged: TKT-01M29CP2PJ (Implement flags with the auto-hide threshold), PR #176. The tombstone words for `removal: "review"` are already in `components/thread/removal.tsx` (TKT-01M29CKRJ9).

### Settled in grooming

- **The route is TKT-01M29CP2PJ's** (docs/07 route table, `POST /threads/{id}/flags`). It takes `{ordinal, reason, note}` and needs a member session and read access. `reason` is `spam`, `abuse`, `off-topic` or `other`. `other` needs a note, and a note is at most 1000 characters (`threads.MaxFlagNote`, counted in code points, `utf8.RuneCountInString`). The answer is 200 `FlagReceipt` `{"received": true}`, `no-store`, and charged to the write limiters. An ordinal the thread lacks is `not-found`. A post in an archived thread is `conflict`, and so is a post already removed or moved, unless the member's own flag on it is still open.
- **A reporter sees only the acknowledgement** (docs/08 "Flags": "Reporters see 'thanks, a moderator will look' and nothing else"; TKT-01M29CP2PJ). No count, no reporter, no outcome.
- **A member cannot flag the same post twice** (docs/08 "Flags", `flags_one_open_per_reporter`). A repeat adds no row and gets the same receipt, even after the post is hidden, so a repeat does not reveal the hide. Once a moderator closes their flag, the member may flag the post again.
- **Auto-hide** (maintainer decisions 2026-09-28 and 2026-09-29, docs/08 "Flags"). At N open flags, 3 by default and a site setting, the post is hidden pending review. Its author and moderators still see it, marked "hidden pending review". This ticket draws none of that; `removal.tsx` already does.
- **Where flag sits** (docs/06 "Post anatomy" and "Mobile"). On desktop, the footer reads `quote · reply · link · flag · history`. On a phone, link, flag and history collapse under a "more" control, and every control is at least 44 px on its shorter side. The reply expander's copies carry no actions (docs/06), so they get no flag.
- **The built client is committed.** Run `go generate ./internal/web/spa` and commit `internal/web/spa/dist`. Only one lane may have a PR open that changes the dist (docs/13 "Running two lanes"). `FlagReceipt` is already in `web/client/src/api/dto.ts` and `dto-contract.ts`.

Engineering choices made here, each with the alternative that lost:
- **`unavailable` reads as the receipt.** The route answers `unavailable` when the flag was kept but the auto-hide it triggered could not be appended. Only a flag that reached the threshold can get that answer, so different words for it would tell the reporter that others had flagged the post, which docs/08 forbids. The dialog therefore shows "Thanks, a moderator will look." for `unavailable` as for 200. The ticket's description asked for a separate short message; that lost for this reason. The same code also comes from a quarantined thread (`threads.access`), where the flag was not kept. That case is rare, and the thread page is failing too; being wrong there costs less than the leak.
- **A flag does not refetch the thread page.** Revalidating after a flag could show the reporter their post turning into a tombstone, which is the outcome. The page updates on the reader's next load, as it would for anyone. Refetching to show a fresh page lost for that reason.
- **The client keeps no record of what it flagged.** The server makes a repeat harmless and answers it identically, and a member may flag again once a moderator closes their flag. A stored "already flagged" mark would be wrong after that, and it would reveal nothing the member does not know. Hiding flag after a report, in memory or in `localStorage`, lost.
- **Flag is offered from the page's viewer, not from a new server field.** It shows when `viewer.value.user` is not null, which holds only for an active member's session (the session chokepoint refuses anyone else, and a pre-user has `user: null`). It also needs the post to be neither `deleted` nor moved. Archived threads are not known to the page, so an archived thread's `conflict` gets words. A `can_flag` on `ThreadViewer` lost: it would change a cached DTO and its viewer-class key for a case the `conflict` words already cover.
- **The dialog is a modal in the thread view's main chunk,** `components/thread/FlagDialog.tsx`, following `HelpOverlay`: `role="dialog"`, `aria-modal`, `trapTab`, Escape, and focus back to the opener. The thread view is at 45573 B of the 60000 B gzip budget on main. A lazy chunk lost: the dialog is small, and a lazy chunk would make the first press wait on a fetch.
- **The phone's "more" is a disclosure**, `components/thread/PostMore.tsx`: a button with `aria-expanded` and `aria-controls` opening a short list of the real `link` and `history` anchors and the flag button. Reusing `shell/MoreMenu.tsx` lost, because it takes only `ShellAction`s run as buttons, and link and history must stay anchors so they can be opened in a new tab or copied. Adding flag to the phone footer as a sixth inline control lost: `.post-footer` does not wrap, and at 390 px it would overflow with 44 px targets. The phone layout is chosen with `isPhone` (`state/layout`), as `Shell` and `ThreadBody` choose it.

### Open decision

Is flag offered on the viewer's own post? The server accepts it, and docs/08 says only "Any member can flag a post".
- (a) Offer flag on every shown post, the viewer's own included.
- (b) Do not offer flag on the viewer's own post (`post.author.handle === viewer.value.user.handle`).

Recommendation: (b). A member who wants their own post reviewed can edit or delete it, and a self-flag counts toward the auto-hide. The plan below builds whichever is chosen through `mayFlag` in step 1.

### Approach

1. **`components/thread/Post.tsx`**:
   - `Post` gains `onFlag?: (ordinal: number) => void`, separate from `PostActions`, because flag does not depend on `can_reply`: a locked thread still takes flags.
   - A helper `mayFlag(post)` is true when `!post.deleted && post.moved_to === null`, plus the open decision's own-post rule.
   - On desktop, the footer renders `<button type="button" class="hit" data-post-action="flag">flag</button>` between link and history when `onFlag && mayFlag(post)`.
   - On a phone, it renders the reply expander, quote and reply, then `PostMore`, holding link, flag (under the same condition) and history.
   - The comment above `Post` that says flag arrives with its own ticket is replaced.
2. **`components/thread/PostMore.tsx`**: a "more" button (`aria-label="More actions for post #N"`, `aria-expanded`, `aria-controls`) and a list. Escape, a press outside, or choosing an item closes it. Escape returns focus to the button. Every item carries `.hit`.
3. **`components/thread/FlagDialog.tsx`**, with props `{thread: string, ordinal: number, onClose: () => void}`:
   - The title is "Flag post #N".
   - A radio group "Why?" offers Spam, Abuse, Off-topic and Other, with values `spam`, `abuse`, `off-topic` and `other`. None is chosen at first.
   - A textarea "Note", with the hint "Required for Other. At most 1000 characters." and a live count of code points (`[...note].length`). It has no `maxlength`, which counts UTF-16 units, not what the server counts.
   - Send stays disabled until a reason is chosen, and for Other until the trimmed note is non-empty. A note over 1000 code points shows the words beside the field and is not sent. While a request is in flight, Send is disabled, so a double press sends once.
   - Send calls `apiSend<FlagReceipt>('POST', '/api/v1/threads/{id}/flags', {ordinal, reason, note})` with the note untrimmed, as typed.
   - 200 and `unavailable` replace the form with "Thanks, a moderator will look." and a Close button. Nothing else is shown.
   - `validation-failed` says "Choose a reason, and say what is wrong when it is Other."
   - `conflict` says "This post can no longer be flagged. It may have been removed or moved, or its thread archived."
   - `not-found` says "This post is no longer here."
   - Anything else (`rate-limited`, `unauthenticated` and the rest) gets `problemMessage` from `api/problem.ts`.
   - On a refusal the form stays as filled.
4. **`screens/ThreadScreen.tsx`, `ThreadBody`**: `const [flagging, setFlagging] = useState<number | null>(null)`. Pass `onFlag={viewer.value.user ? setFlagging : undefined}` to each `Post`, and render `{flagging !== null && <FlagDialog thread={thread.id} ordinal={flagging} onClose={() => setFlagging(null)} />}`. `Tombstone`, `PostPopover` and the reply expander's copies get no `onFlag`.
5. **CSS** in `components/thread/thread.css`: the dialog and backdrop, reusing the `.help-overlay` tokens, and the `PostMore` list. Every target is at least `var(--target)`.
6. Run `go generate ./internal/web/spa`, commit `internal/web/spa/dist`, and check that `npm run build`'s budget line stays under 60000 B.

### Tests

- **`components/thread/Post.test.tsx`** (new):
  - `a member sees flag on a shown post`: with `onFlag`, the footer has `[data-post-action=flag]`, and pressing it calls `onFlag(ordinal)`.
  - `no flag on a removed or moved post`: absent for `deleted: true` (the moderator's and author's view of a removed post) and for `moved_to` set.
  - `no flag without onFlag`: absent when the page passes none, as for a signed-out reader.
  - For the open decision, if (b) is chosen: `no flag on the viewer's own post`.
  - `the phone footer collapses`: with `isPhone` true, link, flag and history are inside the `PostMore` list and not in the footer row. Opening it sets `aria-expanded`, and Escape closes it and focuses the button.
- **`components/thread/FlagDialog.test.tsx`** (new), with `fetch` stubbed:
  - `sends one of the four reasons`: the body is exactly `{"ordinal":4,"reason":"spam","note":""}`.
  - `other needs a note`: Send is disabled with Other and an empty or blank note, and enabled once a note is typed.
  - `the note is counted in code points`: `'😀'.repeat(1000)` is sent, and `'😀'.repeat(1001)` is refused with nothing sent.
  - `the receipt says thanks and nothing else`: after a 200, the dialog's text is exactly "Thanks, a moderator will look." plus Close.
  - `unavailable reads as the receipt`: a 503 `unavailable` shows the same text and nothing else.
  - `refusals are said in words`: 422, 409, 404 and 429 each show their sentence, with the form still filled.
  - `sends once`: two quick presses make one request.
  - `is a modal`: focus starts inside, Tab from the last control wraps to the first, and Escape and Close call `onClose`.
- **`screens/ThreadScreen.test.tsx`**:
  - `flags a post from the page`: a signed-in viewer presses flag on post 3, the dialog opens titled "Flag post #3", and the request goes to `/api/v1/threads/{id}/flags`. After the receipt, no second request for the thread page is made. When the dialog closes, focus is back on post 3's flag control.
  - `a signed-out reader has no flag`.
- **e2e:** `e2e/targets.spec.ts` covers the phone's "more" control and its items at 390 px. `e2e/replies.spec.ts`, or a new `e2e/flag.spec.ts`, flags a post at 390 px and 1440 px and sees the thanks.
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
