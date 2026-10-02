---
schema: 3
id: TKT-01M29CYSAMEVQ7MJG41HR7HPRH
title: Implement split posts into a new thread
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:moderation
  - effort:L
assignees: []
milestone: phase-4
parent: TKT-01M29BN36JH4M1W5819D06PGPP
origin: null
dependencies:
  - TKT-01M29CXJZZZN7X4EZX37JJBF91
  - TKT-01M29CEE6V358M5KCBJGHD58HM
blocks_on: none
references:
  - ref: design:split-merge
    path: docs/06-thread-views.md
  - ref: pr:359
    path: null
claim: null
archive: null
created_at: 2026-09-11T23:31:00Z
updated_at: 2026-10-02T17:21:59Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase4-groom-moving-posts
  name: ""
extensions: {}
---

## Description

Moderator multi-select with a checkbox per post in mod mode, then split to a new thread with a title prompt. The source shows "5 posts were moved to <link>" at the position of the first moved post, and the new thread carries a "split from <link>" header.

Drift is the known cost of flat threads, and moving posts is the answer to it. This is the feature that lets a moderator fix a conversation instead of scolding people for having it.

## Acceptance criteria

- [ ] The source thread shows the moved notice at the first moved ordinal and keeps every ordinal
- [ ] The new thread carries a link back to the source and the posts keep their original authors and timestamps
- [ ] Both threads read correctly after a reindex from logs alone
- [ ] Selection, the selection bar and the split dialog pass e2e/posts-split.spec.ts at 390 and 1440 px with one axe scan per test
- [ ] A split into another forum is refused unless the actor holds moderate in both, by role or through a group, and a refusal writes nothing (TestSplitAcrossForumsNeedsModerateInBoth)
- [ ] The source notice, moved_to and the split-from header name the other thread only to viewers who may read it, and the cached posts never hold them (TestSplitNoticeRevealsTargetOnlyToReaders, TestSplitFromRevealsSourceOnlyToReaders, TestMovedToNotInCachedPosts)
- [ ] The split route has a row in adminRouteGates and in docs/07, and docs/07's post example and Removed posts describe moved, moved_to and move_notice
- [ ] The committed dist is regenerated and the reader, MovePosts chunk included, stays within the 120000 B gzip budget

## Implementation plan

### Settled in grooming

- **The design.** docs/06 "Moderator flow for drift":
  - a checkbox per post in a moderator's selection;
  - "split to a new thread" with a title prompt;
  - the new thread holds the posts in ordinal order;
  - the source shows "N posts were moved to <link>" at the first moved post;
  - the split thread carries a "split from <link>" header.
- **Run decisions** (docs/06, 2026-10-02, pending maintainer review):
  - the selection UI;
  - only posts shown as themselves are selectable;
  - the split thread's starter is the moderator who split it;
  - quotes in copies draw as "Quote from another thread".
- **The write protocol** is TKT-01M29CXJZZ's `movePosts`. This ticket adds the route around it.
- **docs/15 "Invariants, added"** (security):
  - a split into another forum needs `moderate` in both, by role or through a group;
  - the source's notice and every moved post's `moved_to` name the target only to a viewer who may read it, and everyone else sees that posts were moved and nothing more;
  - the same holds in mirror for the split thread's "split from" header, because a source title is source content.
- **The response cache is keyed by viewer class** (docs/07). Whether a viewer may read another forum is not in that key, so target and source references are resolved when the page is drawn and are never stored in the cached posts. Quote tombstones and mentions are resolved at draw time in the same way.
- **The route gate.** The route is behind `moderatesGate`, like `move` (docs/07, docs/08 "Decided 2026-09-30"). Adding it means adding a row to `adminRouteGates`.
- **Modal UI** uses `useModal` (`components/focus.ts`). Times use `PostTime`.

### Approach

1. **Route.** Add `POST /api/v1/admin/threads/{id}/split` in `internal/web/api/moderate.go`, through `h.adminRoute(..., moderatesGate, h.splitPosts)`.
   - The body is `{ordinals: [int], title, forum}`, where `forum` is the destination's slug. It is decoded strictly and bounded at 128 KiB, which holds 10000 ordinals.
   - The title follows create-thread's rules.
   - Over 10000 ordinals is `validation-failed`.
   - It answers 200 `{source: ThreadHeader, thread: ThreadHeader}`, `no-store`.
   - Add the row to `adminRouteGates` in `admin_test.go`.
   - Write the docs/07 route row in the same PR.
2. **Service.** `(*Service).Split` in `service/threads/move.go` resolves the forum as `Move` does (`readForum`). It calls `movePosts` with a new-thread target, then projects both threads.
   - Refusals:
     - a posts problem is `validation-failed`;
     - a posts state that changed under the request is `conflict`;
     - a forum the actor cannot moderate is `forbidden`;
     - a quarantined thread is `unavailable`.
   - Nothing is written on any refusal.
3. **DTO** (`internal/dto/dto.go`):
   - `Post.MovedTo` becomes `*ThreadRef` (`{id, slug, title}`). It is null for a post that did not move, and also when the viewer may not read the target.
   - Add `Post.Moved bool`, so a client can tell a moved post from a deleted one without a target.
   - Add `Post.MoveNotice *MoveNotice` (`{count, thread: *ThreadRef}`) on the first moved post of each move.
   - Add `ThreadHeader.SplitFrom *SplitFrom` (`{thread: *ThreadRef}`) for a thread whose post 1 is a copy: the `post_moves` row with this thread as target and `target_ordinal` 1. Its `thread` is null for a viewer who may not read the source. While the projection trails, the header is left out, which fails closed.
   - Mirror all of this in `web/client/src/api/dto.ts`.
   - Update docs/07's post example and "Removed posts".
4. **Draw-time resolution** (`service/threads/view.go`). After the cached posts are taken, look up the page's moved ordinals in `post_moves` (TKT-01M29CXJZZ). Then check each target thread's forum with `Perm.Can(..., PermRead)` for this viewer, memoised per forum for the request, and fill `MovedTo` and `MoveNotice`.
   - A moved entry with no `post_moves` row yet, because the projection trails, is drawn moved with a null target. That fails closed.
   - The same applies to `posts.go` (one post, replies), lenses, `userposts.go` and the ETag digest, which hashes the body as served.
5. **Client, in the reader bundle** (about 0.6 KB gzip):
   - `components/thread/removal.tsx` `TombstoneText` draws "Post moved to <title>" with a link to `/t/{id}/{slug}/{ordinal}` when there is a target, and "Post moved to another thread" when there is none. This replaces today's slugless `/t/{id}` link, which matches no route.
   - `MoveNotice` draws "N posts were moved to <link>" (or "to another thread") above the first moved post, in the mockups' tombstone row style.
   - `ThreadScreen` draws the header's "Split from <link>" (or "Split from another thread").
6. **Client, in the lazy chunk `MovePosts`** (about 3 KB gzip; it counts toward the 120000 B budget, as every non-admin chunk does):
   - "Select posts" goes in `shell/threadActions.ts` as a new `PanelKind`, offered when `viewer.can_moderate`. It appears in the desktop toolbar and the phone more menu.
   - While selection is on, `Post` and `Tombstone` draw a checkbox at the start of the header for each selectable post, one shown as itself. The checkbox is labelled "Select post #N".
   - A `SelectionBar` is fixed to the bottom of the viewport, above the phone tab bar. It shows the count (in a live region), "Split…", "Merge…" (enabled by TKT-01M29CYSBA) and "Cancel".
   - The selection is held in `state/selection.ts`. It survives page changes within the thread, ends on leaving it or on Escape, and stops at 10000.
   - `SplitDialog` uses `useModal`. It has a title field, a forum `<select>` built as `ModerateDialog`'s `destinations()` does (the source's forum by default), and the text "The posts move to the new thread. To undo, merge them back." On success it calls `storeThreadHeader` for both threads, `forgetGets(forumsUrl)`, and navigates to the new thread. Refusals are worded by `moderateRefusal`.
   - The chunk loads on the first "Select posts". `testing.tsx` gets a preload helper for it beside `lazyChunks` (#250).
7. Run `go generate ./internal/web/spa` and commit the dist. This PR queues for the dist.

### Tests

- Go, invariants:
  - `TestSplitAcrossForumsNeedsModerateInBoth`. Each refused case leaves both logs byte-identical and creates no thread file:
    - moderator role: allowed;
    - moderate through a group grant in both forums: allowed;
    - moderate in the source forum only: `forbidden`;
    - moderate in the target forum only: `forbidden`;
    - a member: refused at the gate.
  - `TestSplitNoticeRevealsTargetOnlyToReaders`: a viewer of both forums gets `moved_to` and `move_notice.thread`; a viewer of the source only gets `moved: true` with both null; a moderator of the source only gets the same.
  - `TestSplitFromRevealsSourceOnlyToReaders`.
  - `TestMovedToNotInCachedPosts`: two members in the same viewer class who differ only in reading the target are served different bodies and different ETags, and the second request is served from the cache.
  - `TestSplitRefusesArchived`: an archived source, moderators and admins included.
  - `TestSplitIntoArchivedForumAdminOnly`.
- Go, behaviour:
  - `TestSplitKeepsSourceOrdinals` (criterion 2).
  - `TestSplitThreadKeepsAuthorsAndTimes` (criterion 3).
  - `TestSplitReindexMatches` (criterion 4: reindex of both threads, then the same pages).
  - `TestSplitValidation`: empty, over 10000, a removed post, a post pending review, an already moved post, a title too long.
  - `TestSplitRouteGated` (the `adminRouteGates` row).
- Vitest:
  - `SelectionBar.test.tsx`: the count, cancel, Escape, the 10000 stop, and keeping the selection across pages.
  - `SplitDialog.test.tsx`: focus trap, the default forum, the success flow and refusal wording.
  - `removal.test.tsx`: moved with a target, moved without one, and the link carries the slug.
  - `MoveNotice.test.tsx`.
  - Split-from in the `ThreadScreen` test.
- e2e: `e2e/posts-split.spec.ts` with mona's stored session, so it needs no `paceAuth` and no `session.ts` row.
  - It selects two posts in a seeded side thread, splits them into a new thread, checks the source notice, the tombstones and the new thread's header, and then merges them back once TKT-01M29CYSBA lands. Until then it leaves the seed as `cmd/ketju/e2e.go` expects.
  - It runs at 390 and 1440 px with one axe scan per test.

### Out of scope

- The records, the protocol, recovery, the read paths and the quote rule: TKT-01M29CXJZZ.
- The merge route, the merge dialog and the "merged from" notice: TKT-01M29CYSBA.
- Tags on the split thread: TKT-01M29CYSCP. A split thread starts untagged.
- The exit-check tests: TKT-01M3YMG0NX.

## Notes

**agent:claude-code/phase4-groom-moving-posts** at 2026-10-02T17:21:07Z

run decision: pending maintainer review. Each is under docs/06 'Moderator flow for drift' (2026-10-02). Selecting is done with 'Select posts' in the thread actions: a checkbox in each post header and a bar fixed to the bottom of the viewport. Only posts shown as themselves move. The split thread's starter is the moderator who split it. A [quote=#N] in a copied post draws as 'Quote from another thread' without text.
