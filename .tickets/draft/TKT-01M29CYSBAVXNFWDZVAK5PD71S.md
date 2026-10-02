---
schema: 3
id: TKT-01M29CYSBAVXNFWDZVAK5PD71S
title: Implement merge into an existing thread
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
  - TKT-01M29CYSAMEVQ7MJG41HR7HPRH
blocks_on: none
references:
  - ref: design:split-merge
    path: docs/06-thread-views.md
  - ref: pr:359
    path: null
claim: null
archive: null
created_at: 2026-09-11T23:31:00Z
updated_at: 2026-10-02T18:03:28Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase4-groom-moving-posts
  name: ""
extensions: {}
---

## Description

The mirror of split: selected posts move into an existing thread, appended in ordinal order, with bidirectional notices.

Merging two active threads is the common case, and it produces posts arriving out of chronological order in the target. Decide whether merged posts sort by their original timestamps or land at the end, and say so, because readers will notice either way.

Reversal is by the opposite operation rather than an undo, which is worth telling a moderator before they commit.

## Acceptance criteria

- [ ] Merged posts keep their original authors and timestamps and carry an origin reference
- [ ] Both threads carry notices linking to each other
- [ ] Reply_to references inside the merged set still resolve after the move
- [ ] Merged posts land at the end of the target in source order after a merge notice, as docs/06 decides, and the merge neither bumps the thread nor notifies anyone (TestMergeAppendsAtEndInSourceOrder, TestMergeDoesNotBumpThread, TestMergeNotifiesNobody)
- [ ] A merge between forums is refused unless the actor holds moderate in both, by role or through a group; a target the actor cannot read is not-found; a refusal writes nothing (TestMergeAcrossForumsNeedsModerateInBoth)
- [ ] Each notice names the other thread only to viewers who may read it, and the cached posts never hold it (TestMergeNoticesRevealOnlyToReaders, TestMergeNoticeNotInCachedPosts)
- [ ] A merge into or out of an archived thread is refused, moderators and admins included (TestMergeRefusesArchived)
- [ ] The merge dialog and notice pass the merge step of e2e/posts-split.spec.ts at 390 and 1440 px with one axe scan per test
- [ ] The committed dist is regenerated and the reader, MovePosts chunk included, stays within the 120000 B gzip budget

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
