---
schema: 3
id: TKT-01M29CYSBAVXNFWDZVAK5PD71S
title: Implement merge into an existing thread
type: task
status: done
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
  - TKT-01M29CYSAMEVQ7MJG41HR7HPRH
blocks_on: none
references:
  - ref: design:split-merge
    path: docs/06-thread-views.md
  - ref: pr:359
    path: null
  - ref: pr:398
    path: null
claim: null
archive: null
created_at: 2026-09-11T23:31:00Z
updated_at: 2026-10-03T09:11:25Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase4-run-cysba
  name: ""
extensions: {}
---

## Description

The mirror of split: selected posts move into an existing thread, appended in ordinal order, with bidirectional notices.

Merging two active threads is the common case, and it produces posts arriving out of chronological order in the target. Decide whether merged posts sort by their original timestamps or land at the end, and say so, because readers will notice either way.

Reversal is by the opposite operation rather than an undo, which is worth telling a moderator before they commit.

## Acceptance criteria

- [x] Merged posts keep their original authors and timestamps and carry an origin reference
- [x] Both threads carry notices linking to each other
- [x] Reply_to references inside the merged set still resolve after the move
- [x] Merged posts land at the end of the target in source order after a merge notice, as docs/06 decides, and the merge neither bumps the thread nor notifies anyone (TestMergeAppendsAtEndInSourceOrder, TestMergeDoesNotBumpThread, TestMergeNotifiesNobody)
- [x] A merge between forums is refused unless the actor holds moderate in both, by role or through a group; a target the actor cannot read is not-found; a refusal writes nothing (TestMergeAcrossForumsNeedsModerateInBoth)
- [x] Each notice names the other thread only to viewers who may read it, and the cached posts never hold it (TestMergeNoticesRevealOnlyToReaders, TestMergeNoticeNotInCachedPosts)
- [x] A merge into or out of an archived thread is refused, moderators and admins included (TestMergeRefusesArchived)
- [x] The merge dialog and notice pass the merge step of e2e/posts-split.spec.ts at 390 and 1440 px with one axe scan per test
- [x] The committed dist is regenerated and the reader, MovePosts chunk included, stays within the 120000 B gzip budget

## Implementation plan

### Settled in grooming

- **The design.** docs/06 "Moderator flow for drift": "merge into an existing thread", with bidirectional notices (docs/09 Phase 4). Reversal is by the opposite operation (docs/08 "Primitives").
- **The ordering rule** (criterion 2) is docs/06's run decision of 2026-10-02 (pending maintainer review):
  - merged posts land at the end of the target, in their source order, after a "N posts were merged from <link>" notice;
  - each keeps its original author and time;
  - a merge does not move the thread up any list and notifies nobody.
- Interleaving by original time lost, because ordinals are positions and docs/03 never renumbers.
- **The write protocol**, including `reply_to` renumbering inside the merged set (criterion 4), is TKT-01M29CXJZZ's `movePosts`. The selection bar, the per-viewer `moved_to`, the source notice and the `ThreadRef` DTO are TKT-01M29CYSAM's. This ticket adds the merge route, the target's notice and the merge dialog.
- **docs/15 "Invariants, added"** (security):
  - a merge between two forums needs `moderate` in both, by role or through a group;
  - the source notice names the target only to the target's readers;
  - the target's "merged from" notice names the source only to the source's readers.
- **The route gate.** The route is behind `moderatesGate`, with a row in `adminRouteGates`. Modal UI uses `useModal`.

### Approach

1. **Route.** Add `POST /api/v1/admin/threads/{id}/merge` in `internal/web/api/moderate.go`.
   - The body is `{ordinals: [int], into}`, where `into` is the target thread's base32 id (a link pasted in the client is reduced to its id there). It is bounded at 128 KiB.
   - It answers 200 `{source: ThreadHeader, target: ThreadHeader}`, `no-store`.
   - Write the docs/07 route row in the same PR.
2. **Service.** `(*Service).Merge` in `service/threads/move.go`:
   - Parse `into` with `threadid`.
   - An unknown target, or one the actor cannot read, is `not-found`, so a merge never confirms that a thread exists.
   - A target equal to the source is `validation-failed`.
   - A target the actor cannot moderate is `forbidden`.
   - Otherwise it calls `movePosts`, as `Split` does.
3. **DTO.** Add `Post.MergeNotice *MoveNotice` (`{count, thread: *ThreadRef}`) on the first copy of each merge.
   - It is drawn from `post_moves` rows grouped by `(target_thread_id, merged_offset)`, at draw time and per viewer, as TKT-01M29CYSAM resolves `moved_to`.
   - A split thread's own first group is its "split from" header, not a notice.
   - Copies carry no other marker.
4. **Client, in the reader bundle** (about 0.3 KB gzip): `MergeNotice` draws "N posts were merged from <link>" (or "from another thread") above the first copy, in the tombstone row style. The times shown after it are each post's own, through `PostTime`.
5. **Client, in TKT-01M29CYSAM's `MovePosts` lazy chunk** (about 1.5 KB gzip more):
   - The selection bar's "Merge…" opens `MergeDialog` (`useModal`). It has a field for a thread link or id.
   - On a valid id it fetches `GET /api/v1/threads/{id}` and shows the target's title and forum before the Merge button enables. A `not-found` reads "No thread you can read has that address."
   - The dialog says: "The posts move to the end of that thread, keeping their authors and times. To undo, select them there and merge them back."
   - On success it stores both headers and navigates to the target at the first copy.
6. Regenerate the committed dist. This PR queues for the dist after TKT-01M29CYSAM.

### Tests

- Go, invariants:
  - `TestMergeAcrossForumsNeedsModerateInBoth`. Each refusal leaves both logs byte-identical:
    - moderator role: allowed;
    - moderate through a group grant in both forums: allowed;
    - moderate in the source only: `forbidden`;
    - moderate in the target only: refused;
    - a target the actor cannot read: `not-found`.
  - `TestMergeNoticesRevealOnlyToReaders`. Three viewers each see exactly what their forums allow:
    - a viewer of both forums sees both links;
    - a viewer of the source only gets the source notice with a null thread and cannot open the target;
    - a viewer of the target only gets the merge notice with a null thread and reads the copies.
  - `TestMergeNoticeNotInCachedPosts`.
  - `TestMergeRefusesArchived`: target or source, moderators and admins included.
  - `TestMergeIntoArchivedForumAdminOnly`.
- Go, behaviour:
  - `TestMergeAppendsAtEndInSourceOrder` (criterion 2).
  - `TestMergeKeepsAuthorsTimesAndOrigin` (criterion 1).
  - `TestMergeNoticesBothWays` (criterion 3).
  - `TestMergeReplyToInsideSetResolves` (criterion 4: inside the set it resolves to the copy, and outside it is dropped).
  - `TestMergeBackRestoresReading`: merging copies back makes copies of copies, and the original ordinals stay moved pointers.
  - `TestMergeDoesNotBumpThread`: it includes a moved post newer than every post in the target.
  - `TestMergeNotifiesNobody`.
  - `TestMergeRouteGated`.
- Vitest:
  - `MergeDialog.test.tsx`: link parsing, the preview, `not-found` wording, the focus trap and the success flow.
  - `MergeNotice.test.tsx`.
- e2e: `e2e/posts-split.spec.ts` (TKT-01M29CYSAM) gains the merge-back step. It runs at 390 and 1440 px with one axe scan per test, on mona's stored session.

### Out of scope

- The records, the protocol, recovery and the read paths: TKT-01M29CXJZZ.
- Selection, the split route and the source notice: TKT-01M29CYSAM.
- Searching for a target thread by title in the dialog: no ticket. The dialog takes a link or id. If a Phase 4 test shows the field is not enough, file a draft with no milestone.

## Notes

**agent:claude-code/phase4-groom-moving-posts** at 2026-10-02T17:21:10Z

run decision: pending maintainer review. docs/06 'Moderator flow for drift' (2026-10-02). Merged posts land at the end of the target in source order, after a 'N posts were merged from <link>' notice, and keep their original authors and times. A merge neither bumps the thread nor notifies anyone. Interleaving by original time lost because ordinals are positions and docs/03 never renumbers. The merge dialog takes the target as a link or id.

**agent:claude-code/phase4-run-cysba** at 2026-10-03T04:14:05Z

Phase A done on run/cysba-merge-posts, cut from run/cysam-split-posts and unpushed; it rebases onto main after the split PR (TKT-01M29CYSAM) merges, and queues for the dist after it.

### What landed

- Route `POST /api/v1/admin/threads/{id}/merge` `{ordinals, into}` behind moderatesGate, with a row in adminRouteGates (eleven moderates routes). Body bounded at 128 KiB as the split's. Answers `MergedPosts {source, target}`, no-store.
- `Service.Merge` (move.go): `into` that is not a thread id, or is the source, is validation-failed; then movePosts. `admitTarget` now maps forbidden and unauthenticated from the target's read check to not-found, so a merge never tells a hidden thread from a missing one. A restricted forum already answered not-found; a members-only forum to a viewer without read did not.
- `Post.MergeNotice` drawn in `drawMoves` from the new `MergesIntoRange` query (post_moves grouped by merged_offset and source, opens_thread = 0, first copy in the page's range). It is read only when the page's snapshot holds a copy (EntryCopied), over the copies' range, so a thread nothing was moved into pays no statement: reading it on every page failed TestMentionsLensStatements and TestUnreadLensStatements, which hold the canonical page's statement count, and was rejected for that reason. Cached posts never hold it (TestMergeNoticeNotInCachedPosts).
- docs/07 "Moving posts": route row, refusals, the ordering rule, `merge_notice`, and the post example.
- Client: `MergeNotice` in removal.tsx (reader bundle); `MergeDialog`, `threadIdFrom` and `mergeRefusal` in the MovePosts chunk; Merge… enabled in the selection bar.

### Deviations from the plan

- The first copy's ordinal for navigation is `target.post_count - ordinals.length + 1` from the answer, not a field of its own. A reply landing in the target between the merge and the header read would put the landing one post late; MergedPosts stays `{source, target}` as planned.
- The dialog reads the target's first page (`GET /threads/{id}?page=1`) for the preview, through apiGet and not the thread cache, so a refused lookup does not mark the thread refused in the cache.
- A link is accepted from any origin: the id is looked up here and the dialog shows the thread found before Merge is offered.
- TestMergeNotifiesNobody lives in internal/web/api (route plus generator). It asserts no notifications row is written and nobody lists one of the target. The originals' own mention and reply notifications stop being listed after the move, as TestNotificationsNeverWidenAccess (e) already holds for any moved post.

### Tests

Go: internal/service/threads/merge_test.go (TestMergeAcrossForumsNeedsModerateInBoth, TestMergeValidation, TestMergeNoticesRevealOnlyToReaders, TestMergeNoticeNotInCachedPosts, TestMergeRefusesArchived, TestMergeIntoArchivedForumAdminOnly, TestMergeAppendsAtEndInSourceOrder, TestMergeKeepsAuthorsTimesAndOrigin, TestMergeNoticesBothWays, TestMergeReplyToInsideSetResolves, TestMergeBackRestoresReading, TestMergeDoesNotBumpThread); internal/web/api/merge_test.go (TestMergeRouteGated, TestMergeNotifiesNobody). Vitest: MergeDialog.test.tsx, MergeNotice.test.tsx, MovePosts.test.tsx, ThreadPage.test.tsx, ThreadScreen.select.test.tsx. e2e: posts-split.spec.ts gains the merge-back test; it and thread, thread-moderation, thread-rename-move, targets and a11y pass at 390 and 1440 px.

Bundle, measured with go generate and not committed: thread-view 84344 B gzip of 120000 (83279 B on the split branch). MovePosts chunk 3160 B JS and 548 B CSS gzip.

**agent:claude-code/phase4-run-cysba** at 2026-10-03T09:11:25Z

Terva review 2058 covered head 8303a318, base 5e8d033c, run 01f626ab, Actions #2693. It raised one medium finding, fixed in 6deeeb71. This note supersedes the line in the earlier implementation note that said MergedPosts stays `{source, target}`.

- **Finding.** The dialog found the first copy by counting back from the target header (`post_count - n + 1`). That header is read after the move, so a reply that lands in the target in between makes the landing late.
- **Fix.**
  - `MergedPosts` gains `first_copy`, which is `MoveResult.Copies[0]`, the move's own answer.
  - The dialog passes it to `onMerge`.
  - docs/07 describes the field.
- **Tests.**
  - TestMergeAnswersItsFirstCopy appends a reply to the target through a new `afterMerge` hook, between the move and the header read. The header then counts 4, and `first_copy` is still 2, the post carrying the merge notice.
  - MergeDialog.test: "lands on the first copy the answer names, whatever the target now counts".
  - The dto and served goldens carry `first_copy`.

The thread view is 89137 B gzip.

## Summary

Landed in one PR on main, after the split (#397).

- **Route.** A moderator moves selected posts into an existing thread with `POST /api/v1/admin/threads/{id}/merge` `{ordinals, into}`.
  - The route is gated by moderatesGate and has a row in adminRouteGates.
  - The service requires moderate in both forums.
  - A target the actor cannot read is not-found.
  - A merge into or out of an archived thread is conflict for everyone.
  - A refusal writes nothing.
- **Logs.** The posts land at the end of the target, in source order, after a merge notice (docs/06). They keep their authors and timestamps, carry an origin reference, and keep reply_to resolving inside the merged set. The merge does not bump the target and notifies nobody.
- **Drawing.** Each thread carries a notice linking to the other, drawn per viewer at draw time. It names the other thread only to its readers and is never in the cached posts. The merge read is gated on EntryCopied in the snapshot, so a page with no copies runs no extra statement.
- **Client.** The MovePosts chunk gains the merge dialog: paste a link or an id, see the thread named, then Merge. It also gains the merge notice. A dialog closed before its merge answers takes nobody anywhere, the gap Terva review 2057 found on the split dialog.
- **Served golden.** The merge route has its servedCases entry and golden.
- **Bundle.** The thread view is 89147 B gzip against the 120000 B budget. Main was 88120 B.
