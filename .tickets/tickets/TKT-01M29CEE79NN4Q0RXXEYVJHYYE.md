---
schema: 3
id: TKT-01M29CEE79NN4Q0RXXEYVJHYYE
title: Build the composer with server-rendered preview
type: task
status: in-progress
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
claim:
  actor: agent:claude-code/phase1-run-415af6aa
  branch: run/cee79-composer
  worktree: /home/sothr/.t3/worktrees/ketju/t3code-415af6aa
  commit: df5f2edc86b2b33efc8358f0764559332213630a
  session: null
  claimed_at: 2026-09-24T22:07:14Z
  expires_at: null
archive: null
created_at: 2026-09-11T23:22:04Z
updated_at: 2026-09-24T22:07:14Z
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
- [ ] Selecting text in a post and clicking quote inserts an attributed blockquote linked to that ordinal
- [ ] The reply chip sets reply_to, is removable, and replying from the bottom composer sets none
- [ ] Mention autocomplete resolves handles and inserts them in the form the renderer expects
- [ ] Preview requests are debounced rather than sent per keystroke

## Implementation plan

Pre-groom plan (2026-09-24). Revise at claim, after reading the code as it stands then.

### Approach
1. `composer/Composer.tsx` has Write and Preview tabs, a `<textarea>`, and a `Toolbar` matching the mockup icons (bold, italic, quote, code, link, @, attach). The attach icon stays inert until the blob upload lands. Its state lives in the `composer` signal (`state/composer.ts`), so the phone sheet (CGBVN) shares it.
2. Preview: after 400 ms idle, `apiSend('POST', '/render', {source, thread_id, reply_to})`. An `AbortController` cancels the stale request, and the rendered `html` goes into `Preview.tsx` through `dangerouslySetInnerHTML`, which is safe because the server sanitizes it. There is no client-side markdown library; CGBYH can assert that.
3. Quote (`composer/quote.ts`): on a post's quote action, read `window.getSelection()` inside that post's body (or the whole post if nothing is selected). Build `> [quote=#N]\n> line…` using the `[quote=#N]` first-line form that `internal/render/quote.go` recognises, then insert it at the caret.
4. Reply chip: the reply action on post N sets `composer.replyTo = N`. `ReplyChip` shows `↩ #N` with a remove button. The bottom composer opened through the sticky Reply button sets `replyTo = null`.
5. Mentions: after `@` plus one or more handle characters, query the autocomplete endpoint and show a listbox. On select, insert `@handle` using the canonical spelling. docs/07 names no endpoint yet, and it must share the renderer's `HandleResolver` (note on C0WEQ).
6. Submit: `POST /threads/{id}/posts {source, reply_to}`. On success, insert the returned post, clear the draft, and reset `replyTo`. Problem types show through `problemMessage`.
7. Drafts: debounced `PUT /drafts/{thread_id}`, loaded on mount (docs/06).
8. Placement: at the bottom of ThreadPage, after the posts, with a sticky Reply button that scrolls to it.

### Files
`web/client/src/composer/*`, `src/state/composer.ts`; the post actions in `src/components/Post.tsx`.

### Tests
- AC1: a Playwright test types source that includes a quote and a mention, waits for the preview, posts, and asserts that the preview `innerHTML` equals the posted post's `.post-body` `innerHTML`. Fetch the posted post with a fresh `GET /threads/{id}` so it comes from the read path.
- AC2: Vitest for `quote.ts` (selection to exact markdown, including multi-line), plus a Playwright select-and-quote test asserting the preview contains `a.quote-source[href="#post-N"]`.
- AC3: Vitest asserts the chip sets and clears `replyTo`, and the bottom composer sends `reply_to: null`. Assert on the request body in Playwright.
- AC4: Vitest with a stubbed endpoint asserts the insertion is `@canonical` followed by a space, and the preview then shows `a.mention[href="/u/canonical"]`.
- AC5: Vitest with fake timers types 20 characters in 1 s and asserts exactly one `/render` call after the debounce.

### Design
docs/06 "Composer"; docs/07 `POST /render`, `POST /threads/{id}/posts`, drafts; `internal/render/quote.go` (attribution syntax, `#post-N` links) and `mention.go`; ThreadLight.dc.html (composer at the bottom, "Draft saved", Write/Preview).

### Interactions
Depends on CA9D4 (`/render`) and C2WWG (`dto.RenderResult`), CA9B9 (reply), CA9CE (drafts), and an autocomplete endpoint no ticket owns (interfaces item 10). CGBVN reuses it in a sheet; CGBY1 drives it.
Open in docs: the render request needs `thread_id` and `reply_to` for an exact preview (interfaces item 2), and drafts for a new thread have no key.

### Settled in grooming (2026-09-24)

- POST /render accepts optional thread_id and reply_to, so the preview matches the stored post (update docs/07 in CA9D4).
- Mention autocomplete comes from TKT-01M38XJJKM (Serve handle autocomplete for mentions).
