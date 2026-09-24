---
schema: 3
id: TKT-01M29CEE79NN4Q0RXXEYVJHYYE
title: Build the composer with server-rendered preview
type: task
status: done
status_reason: null
priority: urgent
due_on: null
labels:
  - area:client
  - effort:L
assignees: []
milestone: phase-1
parent: TKT-01M29BN367F3YQZESM63VN26GP
origin: null
dependencies:
  - TKT-01M29CA9D4R0YMPNSDZK9XENM6
  - TKT-01M29CCC02K8F14P0SKAYFMSE6
  - TKT-01M38XJJKMBZTQHHVN6SB5M6T5
blocks_on: none
references:
  - ref: design:composer
    path: docs/06-thread-views.md
  - ref: pr:76
    path: null
claim: null
archive: null
created_at: 2026-09-11T23:22:04Z
updated_at: 2026-09-24T22:38:24Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase1-run-415af6aa
  name: ""
extensions: {}
---

## Description

Markdown with a toolbar, a debounced live preview through `POST /render`, the quote action that inserts an attributed blockquote from selected text, the reply chip that sets reply_to and can be removed, and `@handle` autocomplete.

The composer sits at the bottom of the page after the posts, which is Tildes' reasoning: read before you write. A sticky Reply button scrolls to it.

No client-side markdown parser. The preview is a server round trip, which is both exact and the thing that makes the same preview work for a non-JS client later.

## Acceptance criteria

- [ ] Preview output is identical to the posted result, because both come from the server pipeline
- [x] Selecting text in a post and clicking quote inserts an attributed blockquote linked to that ordinal
- [x] The reply chip sets reply_to, is removable, and replying from the bottom composer sets none
- [x] Mention autocomplete resolves handles and inserts them in the form the renderer expects
- [x] Preview requests are debounced rather than sent per keystroke

## Implementation plan

Revised at claim (2026-09-25), after reading web/client as it stands and the server pieces this uses: POST /render (TKT-01M29CA9D4) and GET /users?prefix= (TKT-01M38XJJKM), both merged.

### Scope
This ticket builds the composer's five criteria. It does **not** build posting or drafts:

- **Posting.** `POST /threads/{id}/posts` and its response DTO (`CreatedPost`) belong to TKT-01M29CA9B9 (Implement create thread and reply). It is still blocked, and its response shape is not documented or pinned by a golden yet. Guessing it here would lock in a guess.
  - There is therefore no "Post reply" button yet: a button that answers 404 is worse than none, which is the rule Post.tsx already follows for quote and reply.
  - A draft is filed to wire it once CA9B9 lands.
- **Drafts.** `PUT /drafts/{thread_id}` belongs to TKT-01M29CA9CE, which is not built either. The source is kept in memory per thread for the session.
- **The mobile sheet and sticky reply bar** are TKT-01M29CGBVN's.
- **A "Reply" thread action** is added to the one action list, so it appears in the desktop toolbar and the phone's more menu. It scrolls to the bottom composer and clears reply_to (AC3).

### Approach
1. `state/composer.ts` holds per-thread signals `{source, replyTo}` in a Map, shared by the post actions and the composer, and later by CGBVN's sheet. It provides `composerFor(id)`, `quote(id, ordinal, text)` and `reply(id, ordinal)`.
2. `composer/Composer.tsx`, laid out as in ThreadLight:
   - A "Reply" heading, the reply chip (`↩ #N` with a remove button), and Write/Preview tabs.
   - The toolbar: bold, italic, quote, code, link, and @. Attach is left out until blobs exist.
   - A textarea, and a footer saying "Markdown supported".
3. Preview: 400 ms after the last change to source or reply_to, `POST /api/v1/render {source, thread_id, reply_to}`. An AbortController cancels a superseded request, and only the newest answer is shown. The html is inserted as the server sent it, the same way Post.tsx inserts a body, so the preview is the server pipeline's output byte for byte (AC1). Empty source asks nothing. A problem shows in the preview through ErrorPanel's words.
4. Quote (`composer/quote.ts`):
   - `quoteMarkdown(ordinal, text)` produces `> [quote=#N]` and then each line of the text as `> line`, with blank lines as `>`, CRLF normalised, and surrounding blank lines trimmed.
   - The text is the selection when it lies inside that post's `.post-body`. Otherwise it is the whole body, block by block, leaving out the post's own nested quotations (`details.quote`), whose words are someone else's.
   - It is inserted at the caret with blank lines around it, and the composer is scrolled to and focused.
5. Reply: the post's reply action sets replyTo, scrolls to the composer and focuses it. The chip's × clears it. The thread-level Reply action clears it.
6. Mentions (`composer/mention.ts`):
   - `mentionAt(text, caret)` finds `@` plus handle characters ending at the caret, by internal/render/mention.go's rules: not after a handle byte, with a trailing `.` or `-` not part of it.
   - After 150 ms, it asks `GET /api/v1/users?prefix=` and shows the answers in a listbox under the textarea (aria-autocomplete list, aria-activedescendant). ArrowUp and ArrowDown move, Enter or Tab insert, Escape closes.
   - Insertion replaces the typed `@abc` with `@canonical ` (AC4). A handle the server offers is one the renderer resolves (TKT-01M38XJJKM AC2).
7. Post.tsx takes optional `onQuote` and `onReply`. ThreadBody passes them only when `viewer.can_reply`, and renders the composer then too. The popover gets neither.

### Tests (Vitest, happy-dom)
- AC1: the preview pane's innerHTML is exactly the `html` of the stubbed /render answer, and the request carries source, thread_id and reply_to. **The end-to-end half, preview equal to the posted post, needs CA9B9, and its test is CGBY1's Playwright.** Until then AC1 stays unticked.
- AC2: `quoteMarkdown` is tested for single-line, multi-line, CRLF, blank lines and trim. On the screen, a selection inside #N's body and a click on quote insert exactly the expected markdown. With no selection, the whole post is quoted without its nested quotation.
- AC3: the chip appears with #N after reply, and × removes it. The next /render carries reply_to N, then null. The Reply thread action clears it.
- AC4: `mentionAt` covers every parser rule. With a stubbed /users, typing `@al`, pressing ArrowDown then Enter inserts `@alice ` at the caret, and the listbox closes.
- AC5: with fake timers, 20 characters typed over 1 s send no /render until 400 ms after the last, and then exactly one.
- Mutation: hand mutants over quote.ts, mention.ts and the debounce.

### Settled in grooming (2026-09-24)

- POST /render accepts optional thread_id and reply_to, so the preview matches the stored post (update docs/07 in CA9D4).
- Mention autocomplete comes from TKT-01M38XJJKM (Serve handle autocomplete for mentions).

## Notes

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T22:33:27Z

Built as the revised plan says.

### What's where
- **State:** `state/composer.ts` holds the per-thread source, replyTo, caret and summons.
- **Logic:** `composer/{quote,mention,edit}.ts` hold the pure logic.
- **UI:** `composer/Composer.tsx` is the component, and `composer.css` has phone touch targets.
- **Post:** the footer gets quote and reply, only when `viewer.can_reply`.
- **Thread action:** `threadActions` adds Reply, which clears reply_to and summons the composer.
- **API client:** `apiSend` takes an optional abort signal.

### AC1 is left unticked
The client half holds: the preview pane holds the /render `html` byte for byte, in a `.post-body` like a post's, and the request carries thread_id and reply_to. The server half, preview equal to the read path, is TKT-01M29CA9D4's AC1. But "identical to the posted result" cannot be checked until something can post.
- Posting is left out, because `POST /threads/{id}/posts` and its response DTO are TKT-01M29CA9B9's, which is blocked and has no documented shape.
- Filed draft TKT-01M3ART992NAPPBS8PCYAWRT0J (Post a reply from the composer), which depends on CA9B9 and this ticket. It carries this AC's end-to-end check.
- Noted on TKT-01M29CGBY1, whose "reply" step needs it.

### Other things left out
- **Drafts:** the source lasts the session. `PUT /drafts` is TKT-01M29CA9CE's.
- **Attach:** omitted until blobs exist.
- **Sticky Reply bar and phone sheet:** TKT-01M29CGBVN's. On desktop, Reply is a thread action instead.

### Found in a real browser (Playwright-core against vite, both widths), not by Vitest
The composer focused itself in a `useEffect`, which runs after paint. Keys pressed straight after quote went to the button, and a space clicked it again, which quoted twice and dropped what was typed. It also let the reply's late focus move a selection made just after it. The fix is `useLayoutEffect`. Vitest's `act()` flushes effects, so it cannot see this; CGBY1's Playwright run is where it stays covered. Quoted lines also now drop trailing spaces.

### Tests and mutants
- 42 new client tests, all passing.
- 30 hand mutants across quote.ts, mention.ts, edit.ts, Composer.tsx, Post.tsx, ThreadScreen.tsx, state/composer.ts and threadActions.ts, all killed.
- The four first-round survivors each got a test: a backwards selection, a late answer already received, autocomplete with a range selected, and the dismissed-listbox rule.
- The full client suite ran 581 tests. The production build passes.

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T22:38:24Z

Review 537 (head adca12d, Actions run 376) and CI run 377 are answered.

### CI run 377
The "built client is current" step failed: internal/web/spa/dist is committed, and I had not regenerated it. It is now rebuilt with `go generate ./internal/web/spa`. **For the next client ticket:** run that before pushing, not only Vitest and tsc.

### Review 537, two medium findings, both accepted and fixed

**1. "Clear the old preview when its inputs change".** Opening Preview during the debounce, or while a request was in flight, showed the rendering of the previous input.
- Fix: while busy, the pane says "Rendering…" and does not show the held HTML.
- Test: a new test switches to Preview right after a source change and right after a reply change.
- Mutation: clearing the HTML in state as well would be an equivalent mutant, so it is not done.

**2. "Exclude nested quotations throughout a whole-post quote".** Only a `details.quote` directly under the body was skipped, so one inside a list item or a div was copied as this post's words.
- Fix: `bodyText` works on a clone with every `details.quote` removed, and leaves the post's DOM alone.
- Test: covers a quotation in a list item and one in a div, and checks the original is unchanged.

## Summary

The reply composer sits at the foot of the thread when viewer.can_reply. It has Write/Preview, a toolbar, a removable reply chip, and @handle autocomplete from GET /users. The live preview goes through POST /render, debounced 400 ms, with thread_id and reply_to; stale requests are aborted and the HTML is inserted as served. Posts have quote (selection or whole body, in the [quote=#N] form) and reply actions, and the thread has a Reply action. AC2–5 are met. AC1 is unticked until posting exists: draft TKT-01M3ART992 (Post a reply from the composer) depends on TKT-01M29CA9B9. Drafts are CA9CE's and the phone sheet is CGBVN's.
