---
schema: 3
id: TKT-01M3VHSYFR176FWWBAPV8PHSEK
title: Write back index entries an Extended replay changes before trusting it
type: bug
status: in-progress
status_reason: null
priority: high
due_on: null
labels:
  - area:storage
assignees: []
milestone: phase-3
parent: TKT-01M29BKBKP8C66GXXQK4HPEZ35
origin: null
dependencies: []
blocks_on: none
references:
  - ref: pr:306
    path: null
  - ref: pr:312
    path: null
claim:
  actor: agent:claude-code/phase3-run-vhsyfr
  branch: run/vhsyfr-extended-writeback
  worktree: /home/sothr/workspace/git.local.sothr.com/terva-sh/ketju/.claude/worktrees/agent-ad7c0e59b3133396e
  commit: b55ff738a2ca832472bb498e59226009e8413911
  session: null
  claimed_at: 2026-10-01T13:19:33Z
  expires_at: null
archive: null
created_at: 2026-10-01T10:57:46Z
updated_at: 2026-10-01T13:24:29Z
created_by:
  id: agent:claude-code/phase3-run-crj3b
  name: ""
updated_by:
  id: agent:claude-code/phase3-run-vhsyfr
  name: ""
extensions: {}
---

## Description

Found while building the search indexer (TKT-01M29CRJ3B, Build the FTS5 search index and indexer), 2026-10-01.

### What happens

When a thread opens with outcome `Extended`, `threadlog.Open` replays the frames after the `.idx` header's recorded length into the in-memory index. If one of those frames is a `Delete`, `Restore`, `Edit` or `Redacted` record, the replay changes an existing entry in memory. Nothing writes that entry back to the `.idx` file. The actor starts with `headerDirty` false. Its next append writes only its own ordinal's entry, then `flushHeader` advances the header's `LogLength` to cover the whole log. The `.idx` on disk now claims to cover every frame, but the entry the replay changed is stale on disk. The next open trusts it.

### How to reach it

The actor commits in this order (`internal/store/threadlog/actor.go`, `commit`): write the frames to the log, fsync the log, apply them to the index, write their entries, flush the header. A crash or power loss after the log fsync and before the entry write leaves the log holding a `Delete` that the `.idx` does not show. This is the case `Extended` exists for.

A scratch test reproduced it on main at 3890b38:

1. Create a thread and post 1. Close the cache, and keep a copy of the `.idx`.
2. Reopen, append a `Delete` of post 1, close. Put the old `.idx` back, which simulates the crash.
3. Reopen (the outcome is `Extended`, and post 1 is deleted in memory). Append post 2. Close.
4. Reopen. The outcome is `trusted`, and post 1's entry has flags 0 and is `Visible()`.

The deleted post is served again to every reader, and so is a moderator's hide or an auto-hide. After a hard redaction, the post comes back with blanked text and without its redacted tombstone. `ketju redact` writes a full `.idx` after its swap (`reindex` in `cmd/ketju/redact.go`), so the command itself is not exposed. A crash during normal serving is.

### Likely fix

When `Open` returns `Extended` or `Rebuilt`, the thread cache (or the actor at start) writes every entry the replay touched, or the whole index, before the first header flush can mark it trusted. Mark the header dirty from the start in that case. Add a crash test shaped like the steps above.

### Why it is filed here

It is outside the search ticket's scope: the search index reads the same entries, so it inherits whatever the thread page shows. It touches what readers can see (docs/14 "Invariants, added"), so it is filed as a bug with priority high, for the maintainer or the orchestrator to place.

## Acceptance criteria

- [x] threadcache.openThread writes the whole recovered index to the .idx (temp file beside it, rename, fsync of file and directory unless SyncNever) before the actor starts whenever the outcome is not Trusted; TestAReplayedRecordIsWrittenBackBeforeTheIndexIsTrusted passes for Extended, Rebuilt with no .idx and Rebuilt with a broken header, for author Delete, moderator hide, suspension hide, auto-hide, Redacted, Edit and Restore, each reopen Trusted and equal entry for entry to a full rebuild
- [ ] The PR quotes TestAReplayedRecordIsWrittenBackBeforeTheIndexIsTrusted failing on main (a commit holding only the test) for Delete, moderator hide, auto-hide and Redacted under both Extended and Rebuilt, and passing on the reviewed head
- [x] TestAWriteBackIsTrustedWithoutAnAppend and TestAFailedWriteBackNeverMarksTheIndexTrusted pass: an Extended open closed with no append reopens Trusted with 0 bytes replayed, and a failed write-back leaves the .idx header short of the log so the deleted post stays hidden
- [x] The actor writes no index header after a failed entry write, a failed index Sync or a diverged Apply, for the rest of its life including Close; TestAFailedEntryWriteLeavesTheHeaderUnwritten and TestAFailedIndexSyncLeavesTheHeaderUnwritten pass
- [x] A batch that changes an existing entry fsyncs the index between its entry writes and its header write unless SyncNever, and a batch of new posts only adds no index fsync; TestAMutationIsSyncedToTheIndexBeforeTheHeaderCoversIt passes, and a ticket note records BenchmarkAppendLatency for a post and a delete on main and on the head
- [x] ketju serve calls threadcache.DiscardUnverifiedIndexes before threadcache.New: without <data>/index-writeback it removes every .idx and then writes the marker, with it present it removes nothing, and a removal failure stops serve from starting; TestDiscardUnverifiedIndexesRemovesEveryIndexOnce and TestServeDiscardsIndexesWrittenBeforeWriteBack (the thread page serves a hand-poisoned deleted post as a tombstone) pass
- [x] docs/03 write path step 4 and the index open path describe the write-back, the header stop after a failed index write, and the mutation fsync, and no longer call the crash case a harmless replay
- [x] No change to the log format or the .idx layout (format.go, header.go and the index.go field tables unchanged), and none to web/client or the committed dist
- [ ] go test -race ./... passes in CI on the merged head, with the step log naming the new tests

## Implementation plan

Groomed 2026-10-01 for the Phase 3 run (docs/14), against main at d80c2330.

### Settled in grooming

- **Placement.** The orchestrator placed this in Phase 3 on 2026-10-01: it breaks docs/14 "Invariants, added" (search, notifications, lens results and live events never widen access), and the thread page, every lens and the search scan read the same entry flags. Parent: the storage engine epic, TKT-01M29BKBKP.
- **Reproduced on main at d80c2330 for both outcomes**, with a scratch test in `internal/store/threadcache` shaped like the description's steps:
  - `Extended` (the old `.idx` put back after a `Delete` of post 1): open is `extended`; after one more post and a close, the reopen is `trusted` and post 1 has flags `0x0`, `Visible()`.
  - `Rebuilt` (the `.idx` removed): open is `rebuilt`; the actor writes only the new post's entry into a fresh file, so ordinals before it are zero entries under a header that covers the whole log. The reopen is `trusted` and post 1 is visible again. A log restored from a backup without its `.idx` reaches this on its first append.
- **The trust rule this ticket enforces.** A header that covers log length L may reach the `.idx` only when every entry in the file equals the in-memory entry for the log up to L. On main it breaks three ways:
  1. a non-`Trusted` open, because nothing writes the replayed or rebuilt entries back (`threadcache.openThread`, `threadlog.NewActor`);
  2. in `Actor.commit`, a failed `writeEntry` or a diverged `ix.Apply` sets `headerDirty = true`, and the unconditional `a.headerDirty = true; a.flushHeader()` that follows then writes a header covering the record whose entry is wrong;
  3. a power cut, because no fsync orders an in-place entry write before the header write, so the header page can persist while a `Delete`'s entry page does not.
- **Formats.** The log format is frozen (docs/03 "Format freeze") and is not touched. The `.idx` layout is not changed either: the fix uses the existing header and entries, so docs/03's format procedure is not needed.
- **The `.idx` is derived** (docs/03 "Index file format": "a derived, rebuildable file"). Discarding one is always safe, because the next open rebuilds it from the log. Repairing indexes on existing deployments is therefore not docs/14 stop-rule class 2, and is decided below.
- **Cases beyond hiding.** An `Edit` replayed and not written back leaves the entry pointing at the previous revision, so a stale index serves superseded text: a leak of the same kind. A `Restore` not written back keeps a restored post hidden, which narrows rather than widens. Writing the whole index back covers both. No record sets `EntryMoved` yet (`apply.go` has no move case), so there is no moved case to test; the whole-index write covers it when one exists.
- **Run decision: one-time discard of existing indexes** (docs/03 "Durability and recovery", Decided by the run 2026-10-01, pending maintainer review). `ketju serve` removes every `.idx` once per data directory, before the thread cache opens anything, then writes the marker `<data>/index-writeback`. Each thread rebuilds its index from its log on first open and writes it back by step 1 below. Alternatives:
  - leaving it to the operator to run `ketju fsck --rebuild` lost, because nothing tells an operator an index is poisoned, and the run cannot touch the dogfood deployment (docs/14 class 3);
  - verifying every index against its log on every open lost, because that is the full replay the trusted path exists to avoid (docs/03 "Durability and recovery");
  - rebuilding every index eagerly at startup lost, because startup would then scale with total log bytes, while a lazy rebuild costs the same once per thread;
  - extending `ketju reindex` lost, because it rebuilds database projections, refuses to run beside serve, and still needs an operator.

  `ketju fsck --rebuild` stays the manual repair and is unchanged.

### Approach

1. **Write back on every non-trusted open** (`internal/store/threadcache/cache.go`, `openThread`). After `threadlog.Open` and `rec.Truncate`, and before `idxf` is opened, when `rec.Outcome != threadlog.Trusted`, write `rec.Index.AppendTo(nil)` to `id.IndexPath(root)` whole: a temp file beside it (`<id>.idx.writeback`), then rename. Under `SyncGroup` and `SyncAlways` use `fsutil.WriteFile`, which fsyncs the file and the directory. Under `SyncNever` write and rename without the fsyncs, matching the actor's own policy, so that test suites creating many threads do not pay for them. The header written carries `rec.LogLength`, so the next open is `Trusted` even if no append follows. `Create` opens through the same path, so a new thread's empty index is written once.
2. **A write-back that fails never leads to trust.** If step 1 fails, log it at warning with the thread id (add `Options.Logger`, nil meaning `slog.Default()`), and start the actor over an unexported `discardIndex` writer whose `WriteAt` and `Sync` succeed and write nothing. The stale file keeps its old header, so the next open replays again and retries the write-back. The thread serves from its in-memory index, which is correct. `closeThread` tolerates the missing `*os.File`.
3. **The actor stops writing headers once the file may be wrong** (`internal/store/threadlog/actor.go`). Add a field `untrusted bool`, owned by the actor goroutine and sticky for the actor's life. Set it:
   - when `ix.Apply` fails in `commit` (index diverged);
   - when `writeEntry`'s `WriteAt` fails;
   - when an index `Sync` fails (step 4 and `shutdown`).

   `flushHeader` returns without writing once it is set. Replace the `a.headerDirty = true` in those error paths, which today make the next `flushHeader` write a header over the bad entry. `Close` still returns the first error, as now.
4. **Order a changed entry before the header that covers it** (`Actor.commit`). When any applied record in the batch changed an existing entry (`Edit`, `Delete`, `Restore`, or a post `Redacted`, which is every type `postFlagEffect` or `applyEdit` touches), call `a.index.Sync()` after the entry writes and before `flushHeader`, unless the policy is `SyncNever`. A batch of new posts only adds no fsync. Measure `BenchmarkAppendLatency` for a post and for a delete (add a delete sub-benchmark beside it) on main and on the branch, and record both in a ticket note. A mutation pays one index fsync; a post pays nothing new.
5. **One-time discard on existing deployments** (the run decision above). Add `threadcache.DiscardUnverifiedIndexes(root string) (removed int, err error)`:
   - if `<root>/index-writeback` exists, it returns at once;
   - otherwise it lists threads with `threadid.WalkLogs`, removes each `id.IndexPath(root)` that exists, fsyncs each shard directory it removed from, then writes the marker with `fsutil.WriteFile`.

   A crash midway leaves no marker, so the next start repeats it, which is harmless. `cmd/ketju/serve.go` calls it in the `"database"` step, after the lock, the pending-redaction check and the `threads/` mkdir, and before `threadcache.New`. A failure stops serve from starting (`fail("discarding thread indexes written before write-back", err)`), because a poisoned index left in place would widen access. It logs the count at info.
6. **docs/03.**
   - Write path, step 4: drop "the worst case on crash is an index that claims to be older than it is, which triggers a harmless replay". Say instead that a replayed or rebuilt index is written back whole before the actor starts, that a batch changing an existing entry fsyncs the index before its header, and that a failed index write stops header writes until the next open.
   - The open path paragraph under "Index file format": "replay ... and repair" names the write-back.
7. **No client, dist, log or `.idx` layout change.**

### Tests

`internal/store/threadcache/cache_test.go` and a new `writeback_test.go`. Every case follows the description's steps through a real `Cache`:
1. create a thread with posts 1 and 2, close the cache, and copy the `.idx`;
2. reopen, append the record, and close;
3. make the crash (see below);
4. reopen, append one post, and close;
5. reopen and assert.

- `TestAReplayedRecordIsWrittenBackBeforeTheIndexIsTrusted`, table-driven. Records:
  - author `Delete`;
  - moderator hide (`Delete` `by_moderator`);
  - suspension hide (`Delete` `by_moderator` with `suspension`);
  - auto-hide (`Delete` `auto`);
  - `Redacted`. The log is rewritten with `threadlog.RedactPosts` and swapped in with the pre-redaction `.idx` kept, which is a `ketju redact` crash after its swap;
  - `Edit`, where the reader must serve the new source;
  - `Restore` of an auto-hidden post.

  Crashes: `Extended` (the old `.idx` put back), `Rebuilt` with the `.idx` removed, and `Rebuilt` with the `.idx` header CRC broken and its old entries intact.

  Assert that the last reopen is `Trusted`, that its index equals a full rebuild (`threadlog.Open(log, size, nil)`) entry for entry, and that for each hiding case the entry is not `Visible()` and `Reader.Range` returns a tombstone with no source. Commit this test first, alone, and quote its failure on main for Delete, moderator hide, auto-hide and Redacted under both outcomes.
- `TestAWriteBackIsTrustedWithoutAnAppend`: an `Extended` open closed with no append reopens `Trusted`, with 0 bytes replayed, and equal to a full rebuild.
- `TestAFailedWriteBackNeverMarksTheIndexTrusted`: the shard directory is made read-only after setup, so the temp file cannot be created (skipped when running as root). Append and close. The `.idx` header still names the old length, and the reopen, with the directory writable again, hides the deleted post and is `Trusted` only after the write-back.
- `TestDiscardUnverifiedIndexesRemovesEveryIndexOnce`: a hand-poisoned `.idx` (header covers the log, post 1's entry has stale flags 0) and no marker. The call removes it and writes the marker, and the reopen is `Rebuilt` with post 1 hidden. A second call with the marker present removes nothing, and the next reopen is `Trusted`.

`internal/store/threadlog/actor_test.go`, with a fault-injecting and recording `IndexWriter`:
- `TestAFailedEntryWriteLeavesTheHeaderUnwritten`: the entry `WriteAt` fails. No header write follows in that batch or any later one, including `Close`, and `Close` returns the error.
- `TestAFailedIndexSyncLeavesTheHeaderUnwritten`: the same for a failed index `Sync` in a mutation batch.
- `TestAMutationIsSyncedToTheIndexBeforeTheHeaderCoversIt`, under `SyncGroup`:
  - a batch holding a `Delete` records `WriteAt(entry)`, `Sync`, `WriteAt(header)` in that order;
  - a batch of posts only records no index `Sync`;
  - replaying the recorded writes with every write after the last `Sync` dropped except the header gives an index whose deleted post is not visible.

`cmd/ketju/serve_test.go`:
- `TestServeDiscardsIndexesWrittenBeforeWriteBack`: a data directory holding a thread whose `.idx` was poisoned by hand. After the real `serve` starts, `GET /api/v1/threads/{id}` (the thread page, read anonymously in a public forum as `TestServeQuarantinesACorruptThread` reads it) serves post 1 as a tombstone, and `<data>/index-writeback` exists.

Existing tests that must keep passing unchanged: `TestReopeningAnEvictedThreadTrustsItsIndex`, `TestOpenExtendsAShortIndexRatherThanRebuilding`, `TestRedactedIndexExtendedEqualsRebuilt`, the crash suite in `crash_test.go`, and `TestShutdownFlushesEveryIndex`.

### Out of scope

- **A new post's entry lost to a power cut under a header that persisted.** No fsync orders a new-post entry before the header, and step 4 deliberately adds none to a batch of new posts. The entry would read as zeros. That reveals nothing hidden: a post's entry starts visible, and every record that hides it is a mutation, which step 4 syncs. Filed as TKT-01M3VRFRFW (Order a new post's index entry before the header that covers it), a draft with no milestone.
- **Search, projections and notifications** read the log through their own replay or through the cache. The discard in step 5 also clears any poisoned index that `internal/events/search/scan.go` would read. They need no change here.
- **`ketju fsck --rebuild` and `ketju redact`'s `reindex`** already write a whole index atomically and are unchanged.
- **A move record.** None exists yet; whichever ticket adds one inherits step 1.

## Notes

**agent:claude-code/phase3-vhsyfr** at 2026-10-01T12:54:53Z

Placed in Phase 3 by the orchestrator on 2026-10-01, because it breaks the docs/14 invariant that nothing widens access ("Invariants, added"): after a crash or with a missing .idx, a deleted, hidden or redacted post is served as visible again, which is part of the Phase 3 exit criterion. Milestone phase-3, parent TKT-01M29BKBKP (Thread log storage engine); TKT-01M3V3CFRE (the Phase 3 exit check) now depends on it.

**agent:claude-code/phase3-vhsyfr** at 2026-10-01T12:54:53Z

run decision: pending maintainer review. Existing deployments get an automatic one-time repair: ketju serve discards every .idx once per data directory (marker <data>/index-writeback) before the thread cache opens, and each thread rebuilds and writes back its index on first open. Recorded in docs/03 "Durability and recovery" with the alternatives (operator-run fsck --rebuild, verify on every open, eager rebuild at startup, extending reindex) and why each lost. Not a docs/14 stop-rule class: the .idx is derived and rebuildable (class 2 excludes it), and the change is code in the repository, not deployment configuration (class 3).

**agent:claude-code/phase3-run-vhsyfr** at 2026-10-01T13:20:15Z

BenchmarkAppendLatency, single writer, 500 iterations each. Run on this machine's disk under ~/.cache (not tmpfs) on 2026-10-01, with the branch's actor (head) and origin/main's actor (main) back to back, using the branch's bench_test.go, which adds the delete sub-benchmarks. Other lanes were running at the same time, so the tails are noisy. Read every figure against the fsync floor.

| | head p50 | head p99 | main p50 | main p99 |
|---|---|---|---|---|
| fsync floor (4 KB write and fsync) | 1.058 ms | 6.077 ms | 1.023 ms | 3.232 ms |
| post, always | 0.959 ms | 3.145 ms | 0.917 ms | 3.031 ms |
| post, group | 0.986 ms | 2.012 ms | 0.912 ms | 4.211 ms |
| delete, always | 1.454 ms | 6.894 ms | 0.899 ms | 1.532 ms |
| delete, group | 1.354 ms | 6.137 ms | 0.883 ms | 5.684 ms |

A post costs the same as before. A delete pays one more index fsync, about 0.5 ms at p50. Every figure stays under the docs/09 target of 10 ms at p99 under group fsync. An earlier run on a busier disk put the group delete p99 at 12.5 ms, with the fsync floor's own p99 at 15.6 ms on that run.

**agent:claude-code/phase3-run-vhsyfr** at 2026-10-01T13:21:09Z

Implementation notes. (1) The plan made a write-back fail by making the shard directory read-only. CI may run as root, which writes there anyway and would skip the test, so TestAFailedWriteBackNeverMarksTheIndexTrusted puts a non-empty directory where the write-back's temp file goes (<id>.idx.writeback), which fails for any user. TestDiscardUnverifiedIndexesFailsWhenAnIndexStays does the same at the .idx path, to cover the criterion's 'a removal failure stops serve from starting': the discard returns an error and writes no marker, and serve fails on that error. (2) The actor's 'changed an existing entry' test is changesEntry: Edit, Delete, Restore, or a post Redacted, with a non-zero ordinal. (3) A diverged Apply sets the same sticky flag as a failed entry write or index sync. No test provokes it, because the actor validates every record before the log write; the code path is the same flag the two failure tests prove. (4) TestAReplayedRecordIsWrittenBackBeforeTheIndexIsTrusted failed on main for all 21 cases (7 records x 3 crashes) at commit 8e997ab7, which holds the test alone over origin/main eaf54f64. The PR quotes the run.
