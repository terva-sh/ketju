---
schema: 3
id: TKT-01M3ART992NAPPBS8PCYAWRT0J
title: Post a reply from the composer
type: task
status: done
status_reason: null
priority: high
due_on: null
labels:
  - area:client
assignees: []
milestone: phase-1
parent: null
origin: null
dependencies:
  - TKT-01M29CA9B9CEVWZ6A3CH3DZRMF
  - TKT-01M29CEE79NN4Q0RXXEYVJHYYE
blocks_on: none
references:
  - ref: pr:81
    path: null
claim: null
archive: null
created_at: 2026-09-24T22:33:12Z
updated_at: 2026-09-25T01:44:10Z
created_by:
  id: agent:claude-code/phase1-run-415af6aa
  name: ""
updated_by:
  id: agent:claude-code/phase1-run-415af6aa
  name: ""
extensions: {}
---

## Description

The composer from TKT-01M29CEE79 (Build the composer with server-rendered preview) previews, quotes, replies and autocompletes, but it cannot post yet.

`POST /threads/{id}/posts` and its answer, `CreatedPost` in the plan, belong to TKT-01M29CA9B9 (Implement create thread and reply). When CEE79 was built, CA9B9 was blocked and the response shape was neither documented nor pinned by a golden. A Post button that could only answer 404 would have been worse than none, so it was left out.

### What to build
Add "Post reply" and "Discard" to the composer footer, as ThreadLight draws them. Posting sends `{source, reply_to}` from `state/composer.ts`, using the TS type for the golden CA9B9 adds. Then:
- **On success:** clear the source and reply_to, and show the new post. That means either routing to its permalink or storing the returned page, depending on what CA9B9 returns; the thread cache is keyed on log length.
- **On failure:** show the problem in the composer and keep the source.
- **While in flight:** the button is disabled, so a double click cannot post twice.

TKT-01M29CGBY1's "reply" step needs this.

## Acceptance criteria

- [x] A reply posts, the composer clears, and the new post shows on the page
- [x] The posted post's HTML equals the last preview of the same source (TKT-01M29CEE79's AC1, end to end)
- [x] A refused post keeps the source and says why
- [x] A double click posts once

## Implementation plan

### Approach (written at claim, 2026-09-25)
1. **`state/composer.ts`.**
   - Each Composer gains `posting` and `failure`.
   - `postReply(thread)` sends `{source, reply_to?}` to `POST /api/v1/threads/{id}/posts` (`CreatedPost`, from TKT-01M29CA9B9). It does nothing while a post is in flight or when the source is blank. On success it clears the source, chip and caret and closes the sheet. On refusal it records why and keeps everything.
   - `discardReply` empties the composer.
   - These live in state rather than the component, so an answer that arrives after the sheet closed still clears the right composer.
2. **Composer.**
   - The footer gets Discard and Post reply, as ThreadLight draws them.
   - A refusal is shown above the text as `role=alert`, using post-specific wording for conflict, forbidden, rate-limited (with Retry-After), validation-failed and payload-too-large, and the generic wording otherwise.
   - The textarea is read-only while a post is in flight.
   - The phone sheet puts Post in its head, because the footer would be under the keyboard.
3. **Showing the post.** ThreadBody's `onPosted` routes to the new post's permalink. That asks for the page holding the post at the new log length, which replaces the cached entry. The arrival code then scrolls to the post. A reader who has left the thread is not moved.

## Notes

**agent:claude-code/phase1-run-415af6aa** at 2026-09-25T01:27:57Z

Promoted by the maintainer on 2026-09-25, when asked how to proceed at CGBY1: do this first, then CGBY1 with a full reply step. Put in milestone phase-1 because it gates CGBY1.

**agent:claude-code/phase1-run-415af6aa** at 2026-09-25T01:38:35Z

**Evidence**
- **AC1.** Vitest in `Post.test.tsx` covers these flows, on the desktop and in the phone sheet:
  - Post reply sends `{source: 'my reply', reply_to: 2}`, clears the text and chip, routes to `/t/{id}/{slug}/3`, and the new post is on the page.
  - A reply to the thread sends no `reply_to`.
  - Posting from the sheet closes it and empties the bar.
  - If the sheet is closed while a post is in flight, the composer is still cleared when the answer comes.
- **AC2.** `internal/web/api` `TestPostedMatchesItsPreview` checks this at the API, for three sources:
  - the sources cover a live quote, a deleted quote, a missing quote, a known and an unknown mention, a list and a link, with and without reply_to;
  - each is previewed with `POST /render` (thread_id and reply_to), then posted;
  - the created post's HTML and a later cold read's HTML both equal the preview's.
  The client shows the server's HTML as it came, as it does for every post.
- **AC3.** Each of conflict, forbidden, rate-limited (Retry-After 30 gives "post again in 30 seconds"), validation-failed, payload-too-large and unavailable keeps the source and the chip, shows `role=alert` text, and re-enables the button. The next attempt clears the message.
- **AC4.** Two clicks in one tick send one POST. The button is disabled and `aria-busy`, and the textarea is read-only, until the answer comes. `postReply` also refuses a second call while one is in flight, and refuses whitespace whoever calls it.

**Checks**
- **19 hand mutants**, all killed. The one survivor of the first pass, `postReply`'s own whitespace guard, is now tested directly.
- **Local Playwright** (vite with the API routed, 1440 and 390): the first attempt got a 409. The message showed and the text stayed. The second attempt posted and routed to /3, where the post showed and the composer (or the bar) was empty.

**Decision:** the reply bar's pen button is still left out. On the bar a refusal would have nowhere to show; the sheet's Post has the composer to show it in.

**agent:claude-code/phase1-run-415af6aa** at 2026-09-25T01:44:10Z

Review 1 on PR #81 (head 57d5a4f, run 413) raised one medium finding: while a post was in flight, Reply, Quote, the thread's Reply and the chip's × could still change the composer, and a successful post then cleared the new choice. I fixed it by freezing the composer while a post is in flight. quotePost, replyToPost and replyToThread still bring the composer up but change nothing, and the chip's × is disabled, joining the read-only textarea and the disabled Discard. The alternative, clearing only what was sent, was rejected: a quote inserted mid-flight would leave the posted text behind with it, inviting a duplicate post. A new test covers this, and removing any one of the four guards fails it. CI run 414 passed on 57d5a4f.

## Summary

The composer posts. Post reply (Post in the phone sheet's head) sends the source and chip to POST /threads/{id}/posts. It posts once, however often it is clicked. On success it clears the composer and routes to the new post's permalink, which loads the page at the new log length. A refusal keeps the text and says why in post-specific words. Discard empties the composer. A server test holds the posted HTML equal to the preview's.
