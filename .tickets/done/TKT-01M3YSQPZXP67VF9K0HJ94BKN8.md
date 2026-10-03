---
schema: 3
id: TKT-01M3YSQPZXP67VF9K0HJ94BKN8
title: Show, filter and set thread tags in the client
type: task
status: done
status_reason: null
priority: low
due_on: null
labels:
  - area:client
  - effort:M
assignees: []
milestone: phase-4
parent: TKT-01M29BN367F3YQZESM63VN26GP
origin: null
dependencies:
  - TKT-01M29CYSCPQJ5ZZHCFDGV1S4TF
  - TKT-01M3B12ZZ403NEPFH3DASEW1D8
  - TKT-01M29CRJ64G7PJBPK8Z52SE8NW
blocks_on: none
references:
  - ref: pr:361
    path: null
  - ref: pr:391
    path: null
claim: null
archive: null
created_at: 2026-10-02T17:14:05Z
updated_at: 2026-10-03T05:13:53Z
created_by:
  id: agent:claude-code/phase4-groom-tags-archive
  name: ""
updated_by:
  id: agent:claude-code/phase4-run-SQPZX
  name: ""
extensions: {}
---

## Description

The client half of thread tags, split out of TKT-01M29CYSCP (Implement thread tags) in Phase 4 grooming so the server work can land while client PRs queue for the committed dist (docs/15 "Order").

What it covers:
- Tag chips in thread lists and on the thread head, as links that filter a forum's list (docs/06 "Filtering by tag", decided by the run 2026-10-02).
- The filtered list, `/f/{slug}?tag=TAG`, with its "Tagged TAG" header.
- A Tags field on the new thread screen (TKT-01M3B12ZZ4's create rules).
- A moderator's "Edit tags" panel (TKT-01M29CYSCP's retag route).
- The enlarged hit area rule for chips on phones (docs/06 "Mobile").

## Acceptance criteria

- [x] Tags render as chips in every thread list and on the thread head, each a link to its forum list filtered to the tag, and the filtered list shows "Tagged TAG", a way back, and keeps the tag across pages (TagChips.test.tsx, ThreadListScreen.test.tsx, thread-list.spec.ts)
- [x] The new thread screen sends tags from one optional field and names each refused tag under it with aria-invalid; a moderator can Edit tags from the moderation menu and the head updates (NewThreadScreen.test.tsx, new-thread.spec.ts, thread-moderation.spec.ts)
- [ ] Edit tags is offered only with viewer.can_moderate and never on an archived thread (threadActions.test.ts)
- [x] Tag chips have a hit area at least 44 px tall at 390 px without growing visually (thread-list.spec.ts, by targets.spec.ts method)
- [x] The thread list, thread page and new thread screen pass axe at 390 and 1440 px with tags shown, one scan per test
- [x] The committed dist is regenerated with go generate ./internal/web/spa and the reader is within the 120000 B gzip budget (npm run budget), its growth stated in the PR

## Implementation plan

### Settled in grooming

- **Chips are links.** In a forum's list and on the thread head, a chip opens that forum's list filtered to the tag, `/f/{slug}?tag=TAG`. On a cross-forum list it opens the row's own forum filtered the same way. A filtered list shows only threads carrying the tag, pins included, under a "Tagged TAG" header with a link back to the whole list. One tag filters at a time (docs/06 "Thread list views", run decision pending maintainer review).
- **The thread head.** The tags follow "started by HANDLE · TIME · N posts" as chips. A thread with no tags shows nothing there (docs/06 "The thread head", run decision pending maintainer review).
- **Setting tags.** The new thread screen has one optional "Tags" text field below the title, with tags separated by spaces or commas. A moderator's "Edit tags" in the thread's moderation menu opens the same field, filled in. A refused tag is named under the field (docs/06 "The thread head", run decision pending maintainer review).
- **Hit area.** Chips are inline links and keep their visual size, but get an enlarged hit area through padding on phones (docs/06 "Mobile", "Targets and gestures"). Use the existing `hit` class, as the row's title and forum links do.
- **The server routes** are TKT-01M3B12ZZ4 (tags on create, `validation-failed` with `{field: "tags.N", code}`) and TKT-01M29CYSCP (`PUT /admin/threads/{id}/tags`, `GET /forums/{slug}/threads?tag=`).
- **Who sees "Edit tags".** A viewer whose page says `viewer.can_moderate`, as for lock and move. Members are not offered it (docs/08 "Thread tags").
- **Bundle.** Everything here is in the reader bundle. `ThreadRow`, `ThreadScreen`, `NewThreadScreen`, `ThreadListScreen` and `ModerateDialog` are all in the reader chunk (web/client/src/routes.tsx), and the budget counts every chunk but admin, so a lazy chunk saves nothing and adds a shared chunk, as routes.tsx's measurement of the account screens found (2182 B more split). The estimate is about 1.2 to 1.6 KB gzip:
  - chips as links in two places: 0.3 KB;
  - the filtered header and query plumbing: 0.3 KB;
  - the Tags field and its error mapping on two screens: 0.5 KB;
  - the "Edit tags" panel kind: 0.3 KB.

  From about 79200 B, the reader rises to about 80800 B of the 120000 B budget, before other lanes' additions.

### Approach

1. **DTOs and URLs** (web/client/src/api/dto.ts, screens/ThreadListScreen.tsx):
   - `threadListUrl(forum, cursor?, sort?, tag?)` and `pageHref` carry `tag`.
   - Add `tagHref(forumSlug, tag)` beside `threadHref` in components/ThreadRow.tsx.
   - The list screen reads `query.tag` and passes it into the request key, so a filtered list and the whole list are cached apart.
2. **A `TagChips` component** (components/TagChips.tsx, used by `ThreadRow` and the thread head):
   - a `<ul class="thread-tags" aria-label="Tags">` of `<li><a class="hit" href={tagHref(...)}>`;
   - `ThreadRow` passes the row's forum, the list's forum when `t.forum` is null;
   - CSS in threadrow.css: the chip styling moves from `li` to `a`, keeping its size, and the hit padding applies below 720 px.
3. **The filtered list.**
   - With `tag` set, the header reads "Tagged TAG" with a "Show all threads" link to `/f/{slug}`.
   - The empty state says "No threads tagged TAG."
   - "Newest" and "Older threads" keep the tag.
   - A `bad-request` from a malformed tag in the address shows the list's error panel (ErrorPanel), as a bad cursor does.
4. **The thread head** (screens/ThreadScreen.tsx, the "started by" line near line 317): render `<TagChips>` after the line when `head.tags.length > 0`.
5. **The Tags field.** In screens/NewThreadScreen.tsx:
   - add an `<input id="new-thread-tags" autocomplete="off">` labelled "Tags (optional)", below the title, with a hint "Separate tags with spaces or commas";
   - split on `/[\s,]+/`, drop empties, and send the result as `tags`;
   - keep the field in the forum draft only if `ForumDraft` gains it. It does not, so tags are not autosaved, and the plan says so in a code comment;
   - map `validation-failed` `errors` entries for `tags.N` and `tags` to one message under the field, naming the tag: "“X” is not a tag: use lowercase letters, digits and single hyphens", "“X” is longer than 24 characters", "“agent” is reserved for moderators and agent accounts", "A thread can have at most 8 tags". Bind the input with `aria-describedby`, and use `aria-invalid` while there is an error.
6. **Edit tags.** In shell/threadActions.ts, add `'tags'` to `PanelKind`. The moderation menu offers "Edit tags" to a viewer with `can_moderate`, unless the thread is archived (TKT-01M3YSQQ33 hides every moderation control there, so whichever lands second keeps both rules).

   In components/thread/ModerateDialog.tsx, add `titles.tags = 'Edit tags'` and `buttons.tags = 'Save'`. The panel is the same field, prefilled with `head.tags.join(' ')`, and sends `PUT /api/v1/admin/threads/{id}/tags` with `{tags}`. The 200 header goes through `storeThreadHeader`, as the other panels do. A `conflict` for "no change" says "The thread already has these tags." Field errors map as on the new thread screen; share the mapping in a small `tagErrors.ts`. The panel uses `useModal` (components/focus.ts) as the others do.
7. **Built client.** Run `go generate ./internal/web/spa` and commit the dist. Then run `npm run budget`.

### Tests

- Vitest:
  - `TagChips.test.tsx`: chips link to `/f/{slug}?tag=`, use the row's forum on a cross-forum row, and render nothing for an empty list.
  - `ThreadListScreen.test.tsx`: `?tag=garden` asks for `...threads?tag=garden`, shows "Tagged garden" and a link back, and keeps the tag in "Older threads". A bad-request shows the error panel.
  - `NewThreadScreen.test.tsx`: "Garden, long-read  x" sends `["Garden","long-read","x"]`. A `reserved_tag` answer shows the reserved message under the field with `aria-invalid`.
  - `ModerateDialog` (in `ThreadScreen.test.tsx` or a new test): "Edit tags" is offered to `can_moderate` only. Saving sends the PUT and updates the head's chips.
  - `threadActions.test.ts`: `'tags'` is offered with `can_moderate` and not without it.
  - Use `renderAt` for screen tests. No new lazy chunk, so `testing.tsx`'s `lazyChunks` is unchanged.
- e2e (web/client/e2e), stored sessions and one axe scan per test:
  - `thread-list.spec.ts`: the six-tag row's chips are links; clicking `long-read` opens the filtered list holding that thread; "Show all threads" returns. Run at 390 and 1440 px, and check that a chip's hit box is at least 44 px tall at 390 px while its visual box is unchanged (`targets.spec.ts`'s method).
  - `new-thread.spec.ts`: a member starts a thread with two tags, sees them on the head, and is refused `agent` with the message under the field.
  - `thread-moderation.spec.ts`: a moderator edits the tags and the head updates.
  - No `/auth/*` call is added, so `paceAuth` and `e2e/session.ts` are unchanged. A failing test prints its error context (#261).

### Out of scope

- Every server behaviour: TKT-01M3B12ZZ4 and TKT-01M29CYSCP.
- Autosaving tags in the forum draft: no ticket. `ForumDraft` has no tags field, and adding one is a server change nobody asked for.
- A tag picker, filtering by several tags, and tags on search results: no ticket (docs/06 "Thread list views", run decision).

## Notes

**agent:claude-code/phase4-groom-tags-archive** at 2026-10-02T17:21:50Z

run decision: pending maintainer review. Phase 4 grooming (tags and archive lane) settled the tag UI in docs/06 on branch groom/p4-tags-archive:
- A chip is a link to its forum's list filtered to the tag, one tag at a time. A filtered list keeps only the pins carrying the tag ("Thread list views").
- Tags follow the "started by" line on the thread head. They are set in one optional text field on the new thread screen and in a moderator's "Edit tags" panel ("The thread head").

This ticket also depends on TKT-01M29CRJ64 (Implement thread archival), so the header's `archived` field exists when "Edit tags" must be withheld on an archived thread.

**agent:claude-code/phase4-run-SQPZX** at 2026-10-03T00:52:53Z

Phase A is built on run/sqpzx-thread-tags, from main at 4936e55c. The dist is not committed yet: it waits its turn in the dist queue (docs/15 "Order").

### What was built, and the decisions made on the way

- **Chips.** `components/TagChips.tsx` is used by `ThreadRow` and the thread head. It renders `<li><a class="hit" href="/f/{slug}?tag=TAG">`. `ThreadRow` takes a `listForum` prop, because a forum's own list sends `forum: null` on each row; a cross-forum row uses its own forum.
- **The 44 px target on a phone.** Each `li` is `min-height: var(--target)` and centres its chip, with a 6 px top margin and a 10 px bottom margin. So each line of chips is one target tall, and the chip keeps its drawn size of about 20 px.
  - Why: `.hit`'s `::after` reaches 12 px past a chip on each side. With the old 4 px gaps, a chip's press 21 px below its centre landed on the next line's chip, and the title's press landed on the first line.
  - Alternatives that lost:
    - Padding the chip itself makes it grow visually, which criterion 4 forbids.
    - Ordering the `::after`s with z-index only moves the loss to the other neighbour.
  - The cost: on a phone the six-tag row's two lines of chips take about 88 px instead of about 46 px (screenshots refreshed).
  - Mutation check: removing the phone rule fails both `targets.spec.ts` "the thread list" and the new chip test in `thread-list.spec.ts`.
- **The filtered list.**
  - It shows "Tagged TAG" with a "Show all threads" link back, which keeps the sort.
  - Newest, Older threads and both sort links keep the tag.
  - The empty list says "No threads tagged TAG.".
  - A tag the server will not read is its bad-request, shown in the list's ErrorPanel.
- **The Tags field** is `#new-thread-tags`, labelled "Tags (optional)", with the hint "Separate tags with spaces or commas". It is split on `/[\s,]+/`, and the tags are sent as typed. It is not autosaved, because `ForumDraft` has no tags; this is the plan's out-of-scope item. `NewThread.tagsSent` keeps the list a refusal's `tags.N` indexes.
- **Error words** live in `state/tagErrors.ts`, in the reader chunk. They name each tag:
  - "“X” is not a tag: use letters, digits and single hyphens between them."
  - "“X” is longer than 24 characters."
  - "“X” is reserved for moderators and agent accounts."
  - "A thread can have at most 8 tags."
  - An empty tag: "A tag cannot be empty."

  Two choices behind those words:
  - They say "letters" rather than the plan's "lowercase letters", because the server folds ASCII A–Z, so `Garden` is accepted.
  - `forms/messages.ts` carries words for `invalid_tag` and `reserved_tag` without the tag's name, for the codes table. It is in the apply chunk, so the reader cannot import it, and the two lists are written twice on purpose.
- **Edit tags.**
  - It is the `'tags'` PanelKind, labelled "Edit tags…" with an ellipsis, as Move… and Slow mode… are. It is offered with `moderate`, which means `viewer.can_moderate`.
  - The panel is titled "Edit tags", its button is "Save", and the field is prefilled with `head.tags.join(' ')`. It sends `PUT /admin/threads/{id}/tags` and stores the returned header.
  - Save is off while the field folds to the set the thread already has, so the common conflict is not sent.
  - A conflict says "The thread already has these tags, or it is archived.", because the route refuses an archived thread with conflict too. This follows slow mode's wording.
  - A refusal keeps the form and what was typed, as rename and move do.
- **Codes.** `invalid_tag` and `reserved_tag` are now `forms.CodeInvalidTag` and `forms.CodeReservedTag`, in `forms.Codes` and docs/07's form-codes table. The `threads.Code*` constants are removed. docs/07's sentence about the codes outside the table no longer lists them.

### Criterion 3

Edit tags is offered only with `can_moderate` (`threadActions.test.ts`). The criterion's archived half cannot be built yet: `ThreadHeader` has no `archived` until TKT-01M3YSQQ33 (Show archived threads and offer archiving in the client). That ticket hides every moderation control on an archived thread, Edit tags included, and ticks this box in its pull request.

### Measured

- The reader bundle grows from 79983 B on main to 81345 B, which is +1362 B, within the plan's estimate of 1.2 to 1.6 KB.
- Vitest: 2609 tests pass.
- Mutation checks: each of the main source changes was reverted alone, and every one failed a test.
- e2e: the whole suite passes at 390 and 1440 px, 297 tests, with the new ones in thread-list, new-thread and thread-moderation. Each runs one axe scan.
- `go test -race -p 4 ./...` passes.

**agent:claude-code/phase4-run-SQPZX** at 2026-10-03T05:13:53Z

Terva review 2047 (run ce149ddb, Actions #2646), on head 6e31fbf7 and base 5bb0d3b4, raised one medium finding. Fixed.

- **Finding.** A form holding only tags offered no Discard, because `canDiscard` ignored the Tags field.
- **Fix.** `canDiscard` now counts the tags. `discardNewThread` deletes the forum draft only when the form holds something the draft saves (title, body or attachments). A form of tags alone is cleared without a DELETE, because the tags are not in the draft.
- **Test.** NewThreadScreen.test, "discards a form of tags alone, and deletes no draft". Reverting either change alone fails it.

The reader bundle is now 82846 B (+1363 B over main's 81483 B).

## Summary

Built on run/sqpzx-thread-tags, rebased onto main at 5bb0d3b4.

What it does:
- Tags show as chips in every thread list and on the thread head (`components/TagChips.tsx`). Each chip links to its forum's list filtered to the tag. On a cross-forum list that is the row's own forum.
- `/f/{slug}?tag=TAG` shows "Tagged TAG" with a "Show all threads" link. The tag stays across sorts and pages, and an empty result says "No threads tagged TAG.".
- The new thread screen has an optional Tags field. It is split on spaces and commas and is not autosaved.
- A moderator gets "Edit tags…", which sends `PUT /admin/threads/{id}/tags` and updates the head from the answer.
- Both name each refused tag under the field, with `aria-invalid` (`state/tagErrors.ts`).
- On a phone, each line of chips is one 44 px target tall, while each chip keeps its drawn size.
- `invalid_tag` and `reserved_tag` are now in `forms.Codes` and docs/07's form-codes table, with words in `messages.ts`.

Criterion 3: the archived half is left for TKT-01M3YSQQ33 (Show archived threads and offer archiving in the client), which adds `archived` to the header and withholds Edit tags there. That ticket's PR ticks it.

Results:
- The reader bundle goes from 81483 B to 82839 B, +1356 B. index.js grows 63707 B → 64885 B, index.css 11048 B → 11169 B, and ApplyRoot.js 5197 B → 5254 B.
- Vitest passes (2665 tests), and so does `go test -race -p 4 ./...`.
- The thread-list, new-thread, thread-moderation and targets e2e specs pass at 390 and 1440 px.
