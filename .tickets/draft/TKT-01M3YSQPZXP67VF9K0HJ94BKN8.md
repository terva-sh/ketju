---
schema: 3
id: TKT-01M3YSQPZXP67VF9K0HJ94BKN8
title: Show, filter and set thread tags in the client
type: task
status: draft
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
claim: null
archive: null
created_at: 2026-10-02T17:14:05Z
updated_at: 2026-10-02T17:24:09Z
created_by:
  id: agent:claude-code/phase4-groom-tags-archive
  name: ""
updated_by:
  id: agent:claude-code/phase4-groom-tags-archive
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

- [ ] Tags render as chips in every thread list and on the thread head, each a link to its forum list filtered to the tag, and the filtered list shows "Tagged TAG", a way back, and keeps the tag across pages (TagChips.test.tsx, ThreadListScreen.test.tsx, thread-list.spec.ts)
- [ ] The new thread screen sends tags from one optional field and names each refused tag under it with aria-invalid; a moderator can Edit tags from the moderation menu and the head updates (NewThreadScreen.test.tsx, new-thread.spec.ts, thread-moderation.spec.ts)
- [ ] Edit tags is offered only with viewer.can_moderate and never on an archived thread (threadActions.test.ts)
- [ ] Tag chips have a hit area at least 44 px tall at 390 px without growing visually (thread-list.spec.ts, by targets.spec.ts method)
- [ ] The thread list, thread page and new thread screen pass axe at 390 and 1440 px with tags shown, one scan per test
- [ ] The committed dist is regenerated with go generate ./internal/web/spa and the reader is within the 120000 B gzip budget (npm run budget), its growth stated in the PR

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
