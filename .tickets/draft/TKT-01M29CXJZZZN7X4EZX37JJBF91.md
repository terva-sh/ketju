---
schema: 3
id: TKT-01M29CXJZZZN7X4EZX37JJBF91
title: Implement split and merge records in the thread log
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:storage
  - effort:L
assignees: []
milestone: phase-4
parent: TKT-01M29BKBKP8C66GXXQK4HPEZ35
origin: null
dependencies:
  - TKT-01M29C5Y1E782FS135JRVBFRJ6
  - TKT-01M29C0WD88M1C8EW4QQTX76NJ
  - TKT-01M29CXJZ9MVXGY1FC0ESYNG8F
blocks_on: none
references:
  - ref: design:storage-records
    path: docs/03-thread-storage.md
  - ref: pr:359
    path: null
claim: null
archive: null
created_at: 2026-09-11T23:30:21Z
updated_at: 2026-10-02T18:03:30Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase4-groom-moving-posts
  name: ""
extensions: {}
---

## Description

`PostsSplitOut` and `PostsMergedIn`, the hardest events in the format and the reason their type numbers were reserved at the freeze.

Posts are copied into the target thread's log as new Post records carrying an `origin {thread_id, ordinal}`, and the source posts get a moved flag and render as a pointer. Ordinals in the source are never renumbered, which is what preserves every permalink ever issued.

Two logs change, and they cannot change atomically. Decide the order and the recovery: which log is written first, and what a crash between them leaves behind.

## Acceptance criteria

- [ ] Source ordinals are unchanged after a split and every source permalink still resolves
- [ ] Copied posts carry their origin reference and render with attribution to the original author
- [ ] Projections for both threads are correct after replay from logs alone
- [ ] PostsSplitOut and PostsMergedIn bodies match docs/03's fields, with key names pinned by a golden test, and both types stay advisory at 20 and 21
- [ ] A move between forums is refused unless the actor holds moderate in both, by role or through a group, and a refusal writes nothing (TestMoveNeedsModerateInBoth)
- [ ] A moved post is served under the target forum's rules and by no other path: the TestMovedPost* tests cover the page, post, replies, history, search, notifications, blobs, user posts, quotes, the moderator queue and live events
- [ ] A move into or out of an archived thread is refused, moderators and admins included (TestMoveRefusesArchived)
- [ ] ketju fsck passes a data directory holding an unfinished move and reports it as a warning; ketju redact refuses while a move is unfinished
- [ ] docs/03's index table and flags.go assign bit 8 (copied), and docs/04 and audit.Actions list posts.moved
- [ ] A crash after the source's PostsSplitOut, part-way through the target's copies, or between a copied Post and its Edits is finished from the logs alone: TestMoveCrashAfterSourceRecordFinishes, TestMoveCrashMidRunFinishes, TestMoveCrashBetweenPostAndEditsFinishes and TestFinishMovesIdempotent pass, and recovery runs before the server serves

## Implementation plan

### Settled in grooming

- **Stop class 2 is clear for these two records.** docs/15 stops this ticket only if docs/03 leaves a field of either body unfixed, or if the plan changes the `Post` record. Neither applies:
  - `PostsSplitOut` is `ordinals[], target_thread_id` and `PostsMergedIn` is `source_thread_id, ordinals[]` (docs/03 "Record types").
  - The encoding follows `internal/store/threadlog/records.go`: snake_case keys, the same `cbor` and `json` tags, `omitempty`, thread ids as `uint64` (as `ThreadMovedBody.ForumID` and `PostOrigin.ThreadID` are), and ordinals as `uint32`.
  - `Post.origin {thread_id, ordinal}` is already in docs/03's table and in `PostBody.Origin` (`records.go`). This plan writes it and adds nothing to `Post`.
- **How a move is written.** docs/03, "Decided by the run (2026-10-02, pending maintainer review)" under "Split and merge are the hardest events", fixes:
  - the log each record goes in, and their order;
  - the copy frames: original actor, timestamp, revision and machine flag; `reply_to` renumbered; `Edit`s carried;
  - deterministic recovery from the logs;
  - the cap of 10000 posts a move;
  - index entry flag bit 8, `copied`.
- The format is unchanged. 20 and 21 stay advisory, at their held numbers (docs/03 "Reserved record type numbers", `TestEveryCatalogueTypeIsAdvisory`).
- What moves, ordering, quotes in copies and notifications are set by docs/06's run decisions of 2026-10-02 under "Moderator flow for drift":
  - only posts shown as themselves move;
  - merged posts land at the end;
  - attributed quotes in a copy draw as "Quote from another thread";
  - a move notifies nobody. `notifier.go` already skips a `Post` with an `origin`.
- **The thread-move rule** (docs/08 "Decided 2026-09-30", docs/15 "Invariants, added"):
  - a move whose two threads are in different forums needs `moderate` in both, by role or through a group;
  - a moved post is read under the target forum's rules and by no other path;
  - the source notice reveals the target only to its readers.

  Any finding against this is a security finding.
- docs/07 "Removed posts": a moved post is `deleted: true` with `removal: null`, and "nobody gets a moved post's body here", moderators included.
- Phase 4 is the first build that knows 20 and 21, and every released binary will know them. An older reader skipping them shows the moved posts in the source, as docs/03 accepts, and only a pre-release build does that.

### Approach

1. **Records** (`internal/store/threadlog/records.go`):
   - Add `PostsSplitOutBody{Ordinals []uint32 "ordinals"; TargetThreadID uint64 "target_thread_id"}` and `PostsMergedInBody{SourceThreadID uint64 "source_thread_id"; Ordinals []uint32 "ordinals"}` to `catalogue`.
   - Drop them from `Reserved()`, which then reports nothing. Keep `typeNames`.
   - Add `MaxMoveOrdinals = 10000`. A body naming no ordinal, a duplicate, ordinal 0, or a thread id of 0 is malformed. The writer sorts the ordinals ascending. A reader accepts any order, so a later writer is not bound to sort.
   - `DecodeBody` keeps `MaxArrayElements 1<<16`, which is above the cap.
2. **Index fold** (`threadlog/apply.go`, `flags.go`):
   - `PostsSplitOut` sets `EntryMoved` on each named ordinal. An ordinal past `Len()`, or one already moved or not `Visible()`, is `ErrMalformed`.
   - `PostsMergedIn` changes no entry.
   - Add `EntryCopied = 1 << 8`, taken out of `EntryFlagsReserved`. `applyPost` sets it when `PostBody.Origin != nil`.
   - Update the docs/03 index-entry table and `flags.go` together.
3. **Actor** (`threadlog/actor.go`):
   - Add `(*Actor).AppendRun([]Append) ([]Result, error)`. It stages the records in order against the index, as updated by the earlier ones, and commits them as one contiguous run with one fsync.
   - `AtLength` on the first record applies to the run. The run is all or nothing in memory. A crash may leave a prefix on disk, which recovery finishes.
   - `posts.Writer` gets the same method and publishes one event per run.
   - `frameFor` accepts 20/21 only as thread-level (ordinal 0) records.
4. **Projection** (`internal/events/projection`), with migration 0025, the next free number (re-check at implementation):
   - Add a `post_moves` table: `source_thread_id, source_ordinal, source_revision, target_thread_id, target_ordinal NULL, split_offset, merged_offset NULL, opens_thread`, with key `(source_thread_id, source_ordinal)`. Add it to `ProjectionTables`.
   - `source_revision` is the source post's revision when `PostsSplitOut` was applied. A moved post accepts no later record, so it is fixed.
   - A row is finished when its copy's `post_meta.current_revision` equals `source_revision`, which counts every carried `Edit`.
   - Apply `PostsSplitOut`: set `post_meta.moved = 1` and upsert the `post_moves` rows. Recompute the source's `last_post_*` from its newest post that is not moved, deleted ones counted as they are today.
   - Apply `PostsMergedIn`: set `merged_offset` on its rows.
   - Apply a `Post` with an origin: set `target_ordinal`.
   - Apply `PostsMergedIn`: when the thread holds no post yet (a split), mark its rows `opens_thread`.
   - `applyPost` for a copy (one with an origin) changes `last_post_*` only when its `PostsMergedIn` opened the thread, and then only when the copy is newer than the current latest post. A copy merged into an existing thread never changes it, even when the copy is newer (docs/06). A post that is not a copy sets `last_post_*` as today. `post_count` is still the ordinal. Add the query beside `SetThreadLastPost`.
   - Write an audit row `posts.moved` from `PostsSplitOut`, with `{from_thread, to_thread, count}`, as `thread.moved` is written (`projection/audit.go`).
   - Add `posts.moved` to `audit.Actions`, `auditlog`'s related-thread handling and the docs/04 audit table.
   - `decisions` gets real entries, which satisfies `TestEveryRecordTypeHasADecision`.
5. **Search** (`events/search/decisions.go`):
   - `PostsSplitOut` touches its ordinals, which drops their rows, since `Visible()` excludes moved.
   - `PostsMergedIn` touches nothing; the copies index as Posts.
6. **The move protocol** goes in `internal/service/threads/move.go`. It is internal; the split and merge routes are TKT-01M29CYSAM and TKT-01M29CYSBA.
   - `movePosts(ctx, v, source, ordinals, target)` runs under one process-wide `moveMu`. The thread-move path (`Service.Move`) takes the same lock, and so does archival (TKT-01M29CRJ64; whichever lands second adds it). So no thread changes forum or becomes archived during a move. docs/02 runs one process per data directory.
   - **Admit.** Project both threads to their lengths (`admitAt`). Require:
     - `moderate` in each thread's forum;
     - neither thread archived, and neither quarantined (`unavailable`);
     - a target forum that is not archived, unless the actor is an admin, as `Move` does;
     - between 1 and 10000 ordinals, each existing and `Visible()` and not `EntryAutoHidden`;
     - a target that differs from the source.
   - **Write, in docs/03's order:**
     1. For a split, the new thread's header and `ThreadCreated`.
     2. `PostsSplitOut` with `AtLength` = the source's admitted length. `ErrLogMoved` re-admits, up to `threadAttempts`.
     3. The target run, with `AtLength` = the target's length: `PostsMergedIn`, then each post's `Post` copy and its `Edit`s. Read these from the source chain, with the frame actor, time, flags and revision kept. Renumber `reply_to`. `ErrLogMoved` here only means a post landed, so recompute and retry.
   - **Finish.** `finishMoves(ctx)` finds unfinished `post_moves` rows: a null `target_ordinal`, or a copy whose revision is below `source_revision`. Before acting, it confirms each one against the target's index. Then, under `moveMu`, in ascending source ordinal, it appends the missing `PostsMergedIn`, each missing copy, and each copy's missing `Edit`s (the source revisions above the copy's current one). There is no permission check: the move was admitted when its source record was written.
     - It runs at startup, after projection catch-up and before the listener opens.
     - It runs after a move whose target write failed. That request answers `unavailable`, saying the move will finish.
     - It runs every minute, beside the flag sweeper.
     - A quarantined target waits.
7. **Every read path treats a moved post as gone** (docs/15 "by no other path"):
   - `view.go`: `Deleted` includes `EntryMoved`. No body or attachments go to anyone, and history serves no revisions.
   - `revealRemoved` never reveals a moved post.
   - The moderator queue (`queue.go` `stateGone`) serves no body for a moved post, and closes its open flags as moot.
   - Blobs: `attachments.go` already skips `EntryMoved`.
   - Quotes of a moved post are tombstones, because `Tombstoned` uses `Visible()`.
   - Notifications already need `moved = 0`.
   - `/users/{h}/posts` and `/participated` already exclude moved posts.
8. **Quotes in copies** (`internal/render`): add `render.Context.Foreign`. When it is set, every `[quote=#N]` draws as the tombstone "Quote from another thread", with no text and no link.
   - `view.go` sets it for an entry carrying `EntryCopied`, and so does `POST /render` when it previews an edit of one.
   - Bump `renderer_version`.
9. **Edge rules:**
   - `slowModeWait` takes the author's newest `CreatedMS`, not their last ordinal.
   - `ketju fsck` (`cmd/ketju/fsck.go` `checkThread`) checks each `PostsSplitOut`: ordinals exist, nothing is moved twice, and the target exists. It reports an unfinished move as a warning, not damage, because the server finishes it at start.
   - `ketju redact` adds the `PostsSplitOut` edges to `copySet`'s graph. It refuses while a move is unfinished, telling the operator to start the server once.

### Tests

- Format:
  - `TestMoveBodiesRoundTrip`: CBOR and JSON, with key names pinned by a golden.
  - `TestMoveBodyMalformed`: empty, duplicate, 0 and an id of 0.
  - `TestMoveRecordFitsAtCap`: 10000 ordinals above 65536 fit well inside `MaxFrameSize`. A 209000-ordinal body still fits, which pins docs/03's ceiling.
  - `TestEveryCatalogueTypeIsAdvisory` stays green.
- Fold:
  - `TestSplitOutMarksMoved`: ordinals are unchanged and every source permalink still resolves (criterion 1).
  - `TestSplitOutRefusesUnknownOrMovedOrdinal`.
  - `TestCopyCarriesOriginalAuthorTimeAndFlags`.
  - `TestCopyReplyToRenumbered`: inside the set and outside it.
  - `TestCopyCarriesEdits`: the edited and grace flags and the history are kept.
  - `TestEntryCopiedSet`.
  - `TestAppendRunContiguous`.
  - `TestAppendRunAtLength`.
- Recovery:
  - `TestMoveCrashAfterSourceRecordFinishes`.
  - `TestMoveCrashMidRunFinishes`: a prefix of the copies is on disk.
  - `TestMoveCrashBetweenPostAndEditsFinishes`: a copied `Post` is on disk without its `Edit`s. Recovery appends them, and the copy serves the source's current revision and full history.
  - `TestSplitCrashAfterThreadCreatedLeavesUnlistedThread`.
  - `TestFinishMovesIdempotent`: a second run writes no byte.
  - `TestFinishMovesBeforeServing`.
- Projection:
  - `TestMoveProjectionsRebuildFromLogs`: `ketju reindex` of both threads equals the live projection, including `post_moves`, counts and `last_post_*`.
  - `TestMergeDoesNotMoveLastPostBackwards`.
  - `TestMergeOfNewerPostDoesNotBump`: a copy newer than every target post leaves `last_post_*` unchanged.
  - `TestSplitThreadLastPostIsNewestCopy`.
  - `TestSplitRecomputesSourceLastPost`.
  - `TestPostsMovedAuditRow`.
- Invariants. Each builds a cross-forum move with a viewer who reads only the source forum, and checks that nothing reaches them:
  - `TestMovedPostNotOnPageOrPost`: the page, the post, replies and the moderator lens, for a moderator of the source forum only.
  - `TestMovedPostHistoryEmpty`.
  - `TestMovedPostNotInSearch`.
  - `TestMovedPostNoNotification`.
  - `TestMovedPostBlobRefused`.
  - `TestMovedPostNotInUserPosts`.
  - `TestQuoteOfMovedPostIsTombstone`.
  - `TestQueueServesNoMovedBody`.
  - `TestMovedPostSSEHintOnly`.
  - `TestCopyReadUnderTargetRules`: a target-only reader reads the copy, and a reader of neither forum gets `not-found`.
- `TestMoveNeedsModerateInBoth` (through `movePosts`). Each case writes nothing to either log when refused:
  - moderator role: allowed;
  - moderate through a group grant in both forums: allowed;
  - moderate in the source only: refused;
  - moderate in the target only: refused;
  - same forum through a group grant: allowed.
- `TestMoveSerializesWithThreadMove`: a thread move holding `moveMu` makes a concurrent split re-admit and be refused.
- `TestMoveRefusesArchived`: source or target, moderators and admins included.
- `TestQuoteInCopyIsForeign`.
- `TestSlowModeUsesNewestTime`.
- `TestFsckReportsUnfinishedMoveAsWarning`: a backup taken mid-move still passes `ketju fsck`.
- `TestRedactRefusesUnfinishedMove`.
- `TestRedactFollowsSplitEdges`.

### Out of scope

- The split route, the per-viewer `moved_to`, the notices and the client: TKT-01M29CYSAM.
- The merge route and its notice: TKT-01M29CYSBA.
- Reply as a new thread: TKT-01M29CYSC0, which has an open decision.
- Recording quote backlinks: TKT-01M3YME9AZ.
- The `ketju backup` command: TKT-01M29CXK35.

## Notes

**agent:claude-code/phase4-groom-moving-posts** at 2026-10-02T17:21:04Z

run decision: pending maintainer review. docs/03 'How a move is written' (2026-10-02). The source log gets PostsSplitOut and the target log gets PostsMergedIn followed by the Post copies and their Edits. A split writes PostsMergedIn too. Copies keep the original frame's actor, time, revision and machine flag. reply_to is renumbered inside the moved set and dropped outside it. The order is new ThreadCreated, then the source record (the commit), then the target run. Recovery reads the logs alone. A move holds at most 10000 posts. Index bit 8 is 'copied'. Stop class 2 does not apply: docs/03 fixes every body field, and Post.origin is already in the table and in PostBody. Contradiction noted, not a stop: docs/03 calls 20 and 21 advisory because a skipped split is 'stale, not a disclosure'. That holds for every released binary, since v1.0 is the first build that writes and reads them. Only a rollback to a pre-release build would show a cross-forum move's posts in the source again.
