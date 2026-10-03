---
schema: 3
id: TKT-01M29CXJZZZN7X4EZX37JJBF91
title: Implement split and merge records in the thread log
type: task
status: done
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
  - ref: pr:383
    path: null
  - ref: pr:387
    path: null
claim: null
archive: null
created_at: 2026-09-11T23:30:21Z
updated_at: 2026-10-03T15:25:23Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase4-run-orchestrator
  name: ""
extensions: {}
---

## Description

`PostsSplitOut` and `PostsMergedIn`, the hardest events in the format and the reason their type numbers were reserved at the freeze.

Posts are copied into the target thread's log as new Post records carrying an `origin {thread_id, ordinal}`, and the source posts get a moved flag and render as a pointer. Ordinals in the source are never renumbered, which is what preserves every permalink ever issued.

Two logs change, and they cannot change atomically. Decide the order and the recovery: which log is written first, and what a crash between them leaves behind.

## Acceptance criteria

- [x] Source ordinals are unchanged after a split and every source permalink still resolves
- [x] Copied posts carry their origin reference and render with attribution to the original author
- [x] Projections for both threads are correct after replay from logs alone
- [x] PostsSplitOut and PostsMergedIn bodies match docs/03's fields, with key names pinned by a golden test, and both types stay advisory at 20 and 21
- [x] A move between forums is refused unless the actor holds moderate in both, by role or through a group, and a refusal writes nothing (TestMoveNeedsModerateInBoth)
- [x] A moved post is served under the target forum's rules and by no other path: the TestMovedPost* tests cover the page, post, replies, history, search, notifications, blobs, user posts, quotes, the moderator queue and live events
- [x] A move into or out of an archived thread is refused, moderators and admins included (TestMoveRefusesArchived)
- [x] ketju fsck passes a data directory holding an unfinished move and reports it as a warning; ketju redact refuses while a move is unfinished
- [x] docs/03's index table and flags.go assign bit 8 (copied), and docs/04 and audit.Actions list posts.moved
- [x] A crash after the source's PostsSplitOut, part-way through the target's copies, or between a copied Post and its Edits is finished from the logs alone: TestMoveCrashAfterSourceRecordFinishes, TestMoveCrashMidRunFinishes, TestMoveCrashBetweenPostAndEditsFinishes and TestFinishMovesIdempotent pass, and recovery runs before the server serves

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

### Revised at claim

The approach below stands, except where this section says otherwise. Reading the code at claim (2026-10-02) found gaps that the approach as groomed would turn into bugs. Each item says what changes. Items marked **run decision** are pending maintainer review.

**Index**
- **The source index file records the moved flag before its header.** `commit` writes an index entry only for the frame's own ordinal, which is 0 for a `PostsSplitOut`. A trusted index would then still call the moved posts visible on the next open, which is a disclosure. So `commit` writes and fsyncs the entry of every ordinal a `PostsSplitOut` names, before it writes the header that covers them. `changesEntry` reports a split as changing entries.

**Actor**
- **The actor refuses any record that names a moved post.** That holds when the post was moved by a `PostsSplitOut` staged earlier in the same batch, too. `frameFor` adds `EntryMoved` to every non-`Post` record's `Refuse`, as it adds `EntryRedacted`. `checkState` folds staged splits as well as records on the same ordinal. This is what keeps "a moved post accepts no later record" true. The offline `ketju redact` writes without an actor and is unaffected.
- **`frameFor` validates 20 and 21 before anything is written.**
  - Frame ordinal 0.
  - 1 to 10000 ordinals, none 0, no repeats.
  - A non-zero thread id that is not this thread's.
  - For a split, every ordinal exists and is shown as itself after the records staged ahead.
- **Run decision: `Index.Apply` is lenient on state but strict on range.** A split sets moved on every ordinal it names, already moved or not, and an ordinal past the last post is `ErrMalformed`. Alternative: refusing a moved or hidden ordinal in Apply lost, because one bad record would make the whole thread unreadable on replay, while the actor already refuses to write one. `ketju fsck` reports a split that names a post that was not shown, or one moved twice.
- `AppendRun` is a new kind of request with one reply. It stages every record of the run or none, and `AtLength` applies to the first. The cache's write path gains the same method, and it publishes one event carrying the run's final log length.

**Projection**
- **`post_moves` rows are upserts from either side.** Each thread is projected on its own, so a target's `PostsMergedIn` and copies can be projected before the source's `PostsSplitOut`, live or in a reindex. `split_offset` and `source_revision` are nullable until the source's split is projected.
- A row is finished when `target_ordinal` is set and the copy's revision has reached `source_revision`. An unknown `source_revision` counts as unfinished.
- **Run decision: an `Edit` is carried, not new, when its revision is at most the move's `source_revision`, or when that revision is not yet known.** A carried `Edit` writes no `post.edited` audit row and notifies nobody, as a copied `Post` does. The same rule serves the projection's audit and the notifier.
- **Run decision: a thread a move opened takes its `last_post_*` from its newest copy.** A split thread's `ThreadCreated` sets `last_post_at` to the thread's creation time, and every copy is older than that. So a copy in a thread its `PostsMergedIn` opened sets `last_post_*` when the thread has no last post yet, or when the copy is at least as new as the last post. A copy merged into an existing thread only raises `post_count`, through a query of its own.

**Service**
- **Run decision: archival takes `moveMu` and is refused (`conflict`) while the thread has an unfinished move into or out of it.** Otherwise the finisher would meet `ErrArchived` forever. `Archive` also takes the lock, which is the "whichever lands second" item.
- **The unfinished-copy refusal covers every writer of a post record:**
  - edit, delete and restore;
  - the flag hide and `ConfirmHide`;
  - the queue's resolutions;
  - a suspension's `HidePostsOf` and `RestorePostsOf`;
  - the auto-hide sweep, which passes over an unfinished copy.
- **Run decision: `movePosts` takes the split's new-thread spec from its caller,** that is, title, slug, tags and forum. Its author is the moderator. A split's `ThreadCreated` writes no `continues` (maintainer decision 2026-10-02, TKT-01M29CYSC0). The routes are TKT-01M29CYSAM and TKT-01M29CYSBA.
- **Run decision: a copy's attachments raise no blob reference count.** The source posts keep their references, a moved post is never deleted, and blob GC scans the logs (docs/03 "Blob GC"). Alternative: raising the counts lost, because a finish that ran twice after a crash would count twice.
- If the target write fails, the request answers `unavailable`. The move is committed and the finisher completes it.
- If `finishMoves` fails at startup, the failure is logged and serve still starts, as the startup auto-hide sweep does. The move stays hidden in the source until the minute job finishes it.

**Tests and docs**
- Tests that used 20 as the example of an unknown advisory type now use 22, which is unassigned. `Reserved()` is removed, because nothing is held any more. docs/03's "Reserved record type numbers" now says 20 and 21 are implemented.

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
   - A row is finished when its copy's `post_meta.current_revision` is at least `source_revision`, which counts every carried `Edit`.
   - A later edit of the finished copy takes it past `source_revision` and leaves the row finished.
   - Until the row is finished, the service refuses `Edit`, `Delete`, `Restore` and a flag hide of the copy as `conflict`, so its revisions up to the source's are always the carried ones (docs/03).
   - Apply `PostsSplitOut`: set `post_meta.moved = 1` and upsert the `post_moves` rows. Recompute the source's `last_post_*` with the rule `applyPost` uses: its newest post that is not moved and is not a copy merged into it (a copy whose `post_moves` row has `opens_thread` false). Deleted posts count as they do today.
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
  - `TestEditedCopyStaysFinished`: a finished copy is edited past the source revision. `finishMoves`, `ketju reindex`, `ketju fsck` and `ketju redact` all treat the move as finished.
  - `TestUnfinishedCopyRefusesEdit`.
  - `TestFinishMovesBeforeServing`.
- Projection:
  - `TestMoveProjectionsRebuildFromLogs`: `ketju reindex` of both threads equals the live projection, including `post_moves`, counts and `last_post_*`.
  - `TestMergeDoesNotMoveLastPostBackwards`.
  - `TestMergeOfNewerPostDoesNotBump`: a copy newer than every target post leaves `last_post_*` unchanged.
  - `TestSplitAfterMergeDoesNotBump`: a merged copy is newer than the thread's latest post, and an unrelated older post is then split out. `last_post_*` stays on the ordinary post.
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

**agent:claude-code/phase4-groom-moving-posts** at 2026-10-02T19:54:49Z

A split's ThreadCreated writes no continues (docs/03, maintainer decision 2026-10-02, TKT-01M29CYSC0). Only reply as a new thread writes that key. A split's provenance is its PostsMergedIn.

**agent:claude-code/phase4-run-cxjzz** at 2026-10-03T00:21:02Z

Implementation decisions taken in the service, fsck and redact work. Where this note and the plan's "Revised at claim" differ, this note supersedes the plan.

### The unfinished-copy refusal covers Edit only

This supersedes the plan item "The unfinished-copy refusal covers every writer of a post record".

`copyPending` refuses an `Edit` of a copy while its move is unfinished. It is called from `changePostAppended` and `appendEvent` whenever the record is an Edit.

A `Delete` or `Restore` takes no revision. A carried Edit lands on a hidden copy the way it lands on any hidden post, because the finisher sets no `Require` or `Refuse`. So a hide, an auto-hide or a suspension that reaches a copy breaks nothing. Refusing them would let a suspended author's copy stay visible, and the auto-hide sweep would log a conflict every minute.

docs/03's "Recovery reads the logs alone" now says this.

`copyPending` projects the target before it reads `post_moves`, because a run cut short may not have been projected.

### Redact refuses while any move is unfinished, and adds no split edges

`originEdges` also collects every `PostsSplitOut`. If any split's target lacks a copy at the original's revision, redact refuses with `errDrift` (exit 3) and tells the operator to start serve once.

Adding split edges to `copySet` lost. Once a move is finished, every copy carries an origin edge, so a split edge adds nothing. Before it is finished, the copy that is still to be written has no ordinal that an edge could name.

The refusal covers every split in the data directory, not only the copy set. The copy set cannot be known until the moves are finished.

### fsck

- An unfinished move is a `MOVING` line plus `unfinished_moves` in the JSON.
- A split that names a post moved twice, or a post not shown as itself, is a `WARN` line plus `move_warning_threads`. Shown is judged from the final index, with the moved flag cleared.
- Neither of these changes the exit code.
- A `PostsSplitOut` body that does not decode, or that fails `CheckMove`, is damage, because the index cannot fold it.

### Finisher

- `FinishMoves` groups the `UnfinishedMoves` rows by (source, `split_offset`). It skips a row whose source split is not projected yet, and the next pass reaches it.
- It reads the split frame back with `Reader.RecordAt`, and each moved post's chain with `Reader.Chains`.
- It appends one run, with `AtLength` set to the target's length, and retries on `ErrLogMoved`.
- `PostsMergedIn` carries the split frame's actor and flags.
- serve runs it after the startup auto-hide sweep, before the scheduler and the listener, and as the minute job "move finisher".

### Other run decisions

- **A split into a new thread.** The thread is opened only once the source decision has passed. If a record lands before the `PostsSplitOut`, the move is decided again. If that second decision refuses, the empty thread is left behind. It holds no post, so no list shows it.
- **Splitting out of an archived forum.** `movePosts` does not refuse a source in an archived forum, matching `Move`, which lets moderators take a thread out of one. The target forum's archived flag is refused except to an admin.
- **Edit preview.** Plan item 8's `POST /render` preview of an edit to a copy does not set `Foreign`. The route has no ordinal to tell it the post is a copy, so a preview draws such quotes as same-thread and the saved post draws them as foreign. Left for the split route (TKT-01M29CYSAM) or later.
- **Blobs.** As the plan decided, a copy raises no blob reference count.
- **Projection layer.**
  - `post_moves` is `WITHOUT ROWID`.
  - After a split, the source's last post is its newest remaining post by ordinal. A thread emptied of every post returns to `last_post_ordinal` 0.
  - A malformed move record, or a malformed origin, is refused as a log fault.
  - The notifier skips a carried Edit using `IsCarriedEdit`.

### Criterion 6 is left unticked

Covered:
- page, post, replies, history, source and user posts: `TestSplitOpensThread` and `TestMergeCopiesChains`, for a source-forum-only reader and moderators;
- quotes of a moved post: `TestSplitOpensThread`;
- search: `TestSplitDropsMovedPosts`;
- notifications: `TestMoveNotifiesNobody`.

Not covered by a test of a move:
- blobs;
- the moderator queue;
- live events.

These three rely on the `EntryMoved` branches that were already there (`attachments.go`, `queue.go`, the SSE hint path), which this ticket did not change. The tests are also not named `TestMovedPost*`.

### Criterion 10

`TestFinishMovesBeforeServing` was not written. The ordering is shown only by `cmd/ketju/serve.go`: `FinishMoves` runs before `sched.Start` and before the listener opens.

**agent:claude-code/phase4-run-cxjzz** at 2026-10-03T00:37:07Z

Terva review 2007 (run 1f16261f, head cfd73478, base 4936e55c) raised three findings. All three are fixed.

1. **High: an unfinished copy could be moved on.** Moving it on would leave the first move unfinished for good. `decideMove` now runs `copyPending` on each post it is asked to move. Covered by `TestUnfinishedCopyDoesNotMoveOn`.
2. **High: archiving was allowed while a committed move's source was unprojected.** If the source's projection failed after its `PostsSplitOut`, `post_moves` did not name the move, so the target could be archived.
   - Once the source record is written, `movePosts` now always goes on to `finishMove`, and a failure there answers `unavailable`.
   - `Service.movesPending` holds, in memory, every move this process committed and has not finished. It is guarded by `moveMu`.
   - `moveUnfinished` and `FinishMoves` read `movesPending` beside `post_moves`.
   - After a restart, startup catch-up projects every source before `FinishMoves` runs.
   - Covered by `TestArchiveWaitsForUnprojectedMove`.
3. **Medium: redact matched copies by origin alone.** A post with a copy in another thread could make a finished move read as unfinished. Redact now finds a copy by its origin and its thread together. Covered by `TestRedactFindsAMovedCopyAmongOthers`.

**agent:claude-code/phase4-run-cxjzz** at 2026-10-03T00:55:50Z

Terva review 2011 (run 0626e0fb, head e0834d0e, base 4936e55c) confirmed that 2007's three findings are resolved. It raised one new finding, which is fixed.

**High: every Post with an origin was taken for a move's copy.**

The projection upserted a `post_moves` row for any `Post` carrying an origin. That meant a copy written by hand, which the redact tests write, became an unfinished move that nothing could ever finish. It also meant a stray copy of a moved post could overwrite that move's target.

The fix replaces `UpsertCopyMove` with `SetCopyMove`, which updates only and never inserts. A `Post` counts as the move's copy only when all of these hold:
- its row's `PostsMergedIn` is in the same thread;
- that `PostsMergedIn` sits ahead of the `Post` in the log (`merged_offset < copy offset`);
- it is the first `Post` naming that origin after the `PostsMergedIn` (`target_ordinal IS NULL OR >= this ordinal`).

Any other `Post` with an origin is projected as an ordinary post.

- Test: `TestOriginWithoutMoveIsNoCopy`.
- docs/04 `post_moves` and the migration comment now say this.

**agent:claude-code/phase4-run-cxjzz** at 2026-10-03T01:12:42Z

Terva review 2014 (run d0d07727, head fb971a6e, base bb6faf74) confirmed that 2011's finding is resolved. It raised two new medium findings, and both are fixed.

1. **A target projected ahead of its source lost real post.edited rows.** While a move's source revision was unknown, every Edit of the copy counted as carried. That included a moderator's edit made after the move had finished, so a rebuild that projected the target first never audited it.
   - The writer now reads the source revision from the source's log, in `learnSourceRevisions` (`movesource.go`), whenever it meets an Edit of a copy whose revision is unknown. One scan of the source log fills in every unknown move out of that thread, using the value the split would write anyway, since a moved post accepts no later record.
   - If the source log is missing or damaged, the revision stays unknown and the edit counts as carried, as before.
   - New queries: `MoveOfCopy`, `MovesWithoutSourceRevision` and `SetMoveSourceRevision`.
   - Test: `TestTargetFirstAuditsALaterEdit`.
2. **A split out of a split thread picked the new last post by ordinal.** A thread a move opened holds copies whose times do not rise with their ordinals, so the highest ordinal is not always the newest post. `NewestListedPost` now orders by `created_at DESC`, then `ordinal DESC`, which is the rule `applyCopy` already uses to pick the last post.
   - Test: `TestSplitRecomputesByTime`.

docs/04 `post_moves` says both.

**agent:claude-code/phase4-run-cxjzz** at 2026-10-03T01:20:10Z

Terva review 2018 (run 6ea0744e, head 63857b4a, base bfa50b8c) confirmed that 2014's two findings are resolved. It raised one new high finding, which is a security finding, and it is fixed.

**High (security): a MoveRequest could name both a target and a split.**
- Given both, `movePosts` checked only the split's forum. It then wrote the move into the named target, which was never checked: the request skipped both `admitTarget` and `openSplit`.
- `movePosts` now answers bad-request, before anything is written, when a request names both a target and a new thread, or names neither.
- The `MoveRequest` doc now says exactly one of `Target` and `Split` is set.
- `TestMoveNamesOneDestination` covers this. In the test, fmod, who moderates general, asks to split into general while naming the staff thread as the target. The request is refused, and neither log is written.

**agent:claude-code/phase4-run-cxjzz** at 2026-10-03T01:34:28Z

Terva review 2020 (run 9e7dbad2, head 0a299062, base bfa50b8c) confirmed that 2018's finding is resolved. It raised two medium findings. Both fixes are narrow, so this goes to a sixth round, as docs/14 "Amended" permits.

1. **A copied Edit could keep a stale reply.** An Edit's `reply_to` of zero means "keep the post's". When a post first replied to a post moved with it and was later edited to reply to one left behind, the copy kept the renumbered first reply.
   - `finishInto` now reads the post's final `reply_to` (`lastReplyTo`). When that names a post left behind, every record of the copy drops its `reply_to`, so the copy replies to nothing.
   - This is decided from the source chain alone, so a finish that resumes after a crash decides the same way.
   - docs/03 says this.
   - Test: `TestCopyDropsAReplyEditedOutside`.
2. **fsck and redact picked a copy arbitrarily.** When two copies of one origin sat in the same target, both chose between them by map order.
   - They now identify a move's copy as the projection does: the first Post naming the origin after the `PostsMergedIn` that names it, in the same log.
   - fsck keeps `awaiting` per log, and redact keeps `moveCopies`.
   - Tests: the stray copy in `TestFsckReportsUnfinishedMoveAsWarning`, and `TestRedactFindsAMovedCopyAmongOthers`, which now has a stray copy at revision 1 in the target beside the real one at revision 2.

**agent:claude-code/phase4-run-cxjzz** at 2026-10-03T01:49:21Z

PR #383 conflicted with main once main took migration 0025 for quote backlinks (TKT-01M3YME9AZ). Its final tree was carried onto main as one commit on run/cxjzz-split-merge-2, PR #387, as the brief's rule for a moved main says. post_moves is now migration 0026, and ProjectionTables and ClearProjections keep both post_quotes and post_moves. #383 is closed unmerged. Note: one push to run/cxjzz-split-merge passed --force-with-lease; it was a fast-forward (the remote head cfd73478 was its parent), so nothing was rewritten.

**agent:claude-code/phase4-run-cxjzz** at 2026-10-03T01:59:26Z

Terva review 2024 on PR #387 (run 0793885b, head edb18fbe, base bfa50b8c) is the first full review of the carried tree. It raised one medium finding, and it is fixed.

**Medium: a resumed move could leave a copy replying to the wrong post.**

The trouble came from a copy whose `reply_to` named a later moved post. `finishInto` gave that later post a predicted ordinal. If a crash cut the run and an ordinary post then arrived in the target before the finish, the later copy landed at a different ordinal, and the record that named it was already written and could not be corrected.

`reply_to` is now renumbered only when it names a moved post ahead of this one, that is, at a lower source ordinal. That post's copy is written earlier in the run, so its ordinal is fixed (in `post_moves`) before anything names it. Every other `reply_to` is dropped, and the `lastReplyTo` rule from review 2020 still applies on top of this.

A post replies only to an earlier post, and the service's Edits carry the post's own `reply_to`, so only a log written some other way loses a reply under this rule.

docs/03 says this. The tests:
- `TestFinishAfterATargetPostKeepsReplies`: a cut run, then an ordinary post in the target, then the finish. The second copy still replies to the first copy.
- `TestCopyDropsAForwardReply`.

**agent:claude-code/phase4-run-orchestrator** at 2026-10-03T15:25:23Z

Criterion 6 met after close, 2026-10-03. The three paths the merged tests lacked are covered on main: TestMovedPostBlobFollowsTarget, TestMovedPostQueueServesNoBody and TestMovedPostSSEHintOnly (TKT-01M29CYSAM, PR #397), and TestPhase4ExitMovedPostsFollowTheTargetForum through the real serve (TKT-01M3YMG0NX, PR #404): the moved blob and thumbnail answer 404 to a reader of the source alone, the queue and moderator lens carry no moved body, and a source-only live stream gets no hint naming the target.

## Summary

Split and merge are implemented below the routes. A move writes the source's `PostsSplitOut` first, then the target's `PostsMergedIn` and the copies as one contiguous run. A move left part-way is finished from the logs alone.

### Log

- **Records** (`internal/store/threadlog`):
  - Record types 20 and 21 have bodies, golden-pinned. `CheckMove` caps a move at 10000 posts.
  - Index entry flag bit 8 is `copied`.
  - The index records moved entries before its header.
  - The actor validates the move records. It refuses any later record on a moved post, including posts moved by a split staged earlier in the same batch.
- **Appends:**
  - `Actor.AppendRun` and `posts.Writer.AppendRun` write a run all or nothing, with `AtLength` taken from the first record.
  - `Reader.Chains` and `Reader.RecordAt` read moves back.

### Projection, search and notifications

- Migration 0026 adds `post_moves`. Rows are upserted from either side.
- A move writes the `posts.moved` audit row.
- Search drops moved posts.
- The notifier skips copies and carried Edits.

### Service

`internal/service/threads/move.go` has `movePosts`, `finishMove` and `FinishMoves`.
- A move between forums needs moderate in both.
- Archived threads are refused.
- Only posts shown as themselves move.
- `moveMu` is shared with `Move` and `Archive`, and `Archive` waits until no move into or out of the thread is unfinished.
- An Edit of an unfinished copy is refused.

### Reads and rendering

- Moved posts are served as deleted with no body.
- Quotes inside a copy draw as "Quote from another thread" (render Version 6).
- Slow mode measures from the newest post by time.

### Operations

- serve finishes moves at startup and every minute.
- fsck reports unfinished moves as `MOVING` and odd splits as `WARN`.
- redact refuses, as drift, while any move is unfinished.

### Still open

- Criterion 6 is unticked: blobs, the moderator queue and live events have no move-specific test.
- The split and merge routes are TKT-01M29CYSAM and TKT-01M29CYSBA.
