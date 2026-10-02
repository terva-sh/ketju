---
schema: 3
id: TKT-01M3VRFRFW9M4XY8D14FXAJA23
title: Order a new post's index entry before the header that covers it
type: bug
status: in-progress
status_reason: null
priority: low
due_on: null
labels:
  - area:storage
assignees: []
milestone: phase-4
parent: null
origin: null
dependencies: []
blocks_on: none
references:
  - ref: pr:362
    path: null
claim:
  actor: agent:claude-code/phase4-run-vrfrfw
  branch: run/vrfrfw-index-entry-order
  worktree: /home/sothr/workspace/git.local.sothr.com/terva-sh/ketju/.claude/worktrees/agent-ad5c11892789defa3
  commit: 75695ad8059e75e7dfb0c60b887b5bd71211e379
  session: null
  claimed_at: 2026-10-02T21:22:01Z
  expires_at: null
archive: null
created_at: 2026-10-01T12:54:32Z
updated_at: 2026-10-02T21:22:02Z
created_by:
  id: agent:claude-code/phase3-vhsyfr
  name: ""
updated_by:
  id: agent:claude-code/phase4-run-vrfrfw
  name: ""
extensions: {}
---

## Description

Found while grooming TKT-01M3VHSYFR (Write back index entries an Extended replay changes before trusting it), 2026-10-01.

### What can happen

`Actor.commit` (`internal/store/threadlog/actor.go`) writes a new post's index entry and then the index header, and fsyncs neither until `Close`. A power cut can persist the header page and not the page holding the new entry. On the next open, the header covers the log, so the index is trusted, and the new post's entry reads as zeros: offset 0, frame length 0, author 0, flags 0.

TKT-01M3VHSYFR fsyncs the index before the header for a batch that changes an existing entry, so a hide, delete, edit or redaction is never lost this way. It deliberately adds no fsync to a batch of new posts, because that would double the fsyncs on the most common write.

### Why it does not widen access

A post's entry starts visible, and every record that hides it is a mutation, which that ticket syncs. A zeroed entry reveals nothing hidden. The likely effect is a read error for that one post (a zero-length frame read at offset 0) until `ketju fsck --rebuild` or a discarded index rebuilds it. The filesystem may also refuse the case outright: with ext4 `data=ordered`, a size extension is not committed before its data.

### Possible fixes

- On open, check that every entry's offset and frame length name a frame boundary inside the log. This is cheap only with sampling, since checking every entry is the replay the trusted path avoids.
- Have the header lag: never let it cover a batch until a later index fsync has made that batch's entries durable.
- Measure first: try to provoke the case on ext4 and xfs before deciding it is worth code.

## Acceptance criteria

- [ ] TestATrustedOpenNeverServesALostNewEntry passes over every crash image of three posts-only batches, holes included, and the PR quotes it failing on main (a commit holding only the test) for the image with the header persisted and the last entry zeroed
- [ ] TestEveryWrittenIndexPassesTheEntryCheck passes over the property test's random sequences, and TestAZeroedNewEntryIsRebuiltOnOpen passes through a real threadcache.Cache (Rebuilt, then Trusted)
- [ ] decodePost refuses a frame whose ordinal differs from the one asked for; TestAnEntryPointingAtAnotherPostsFrameIsAReadError passes
- [ ] A ticket note records BenchmarkOpenTrusted on a 50 000-post index on main and on the head
- [ ] docs/03 write path step 4 and the Index file format open paragraph describe the entry check; no change to the log format, the .idx layout, header.go or the actor's fsync policy
- [ ] ParseIndex refuses with ErrMalformed (RebuildRequired true) an entry with Revision 0, Offset below FileHeaderSize, FrameLength outside [FrameFixedSize, MaxFrameSize], or a frame not ending inside the header's LogLength, checked without overflow (Offset > LogLength, then FrameLength > LogLength - Offset); TestAnImpossibleEntryIsMalformed passes, including an Offset near math.MaxUint64 whose sum would wrap

## Implementation plan

Groomed 2026-10-02 for the Phase 4 run (docs/15), against main at ed82f15e.

### Settled in grooming

- **Placement.** docs/15 "Scope" places this ticket in Phase 4, with the other small unplaced tickets. It follows TKT-01M3VHSYFR (Write back index entries an Extended replay changes before trusting it, done in #312). That ticket's run decision, the write-back and the one-time discard, is not reopened.
- **How the case arises today.** `Actor.commit` (`internal/store/threadlog/actor.go`) writes a new post's entry with `WriteAt` at `EntryFileOffset(n)`, past the end of the file, and then writes the header at offset 0. A batch of new posts only does not fsync the index, by TKT-01M3VHSYFR's design. A power cut leaves the file in one of three states:
  1. The header persisted and the file length did not. `ParseIndex` already refuses this. The header's `PostCount` does not match the entries the file holds, so `ErrMalformed` leads to `Rebuilt`. That is safe.
  2. The file length persisted and the entry's bytes did not, so the entry reads as zeros. On ext4 `data=writeback`, and on any filesystem that does not order data before a size change, `ParseIndex` accepts the file, `OpenIndex` trusts it, and the post reads as offset 0, length 0. This is the bug.
  3. Both persisted, or neither. That is correct.

  The same can happen to every new-post batch since the index's last fsync, which is the last mutation batch or `Close`. The lost entries are always at the tail of the file.
- **Why it does not widen access.** A new post's entry starts visible, and TKT-01M3VHSYFR fsyncs every batch that hides one. The cost is availability: the post fails to read until something rebuilds the index. If the filesystem exposes stale blocks rather than zeros, an entry could also point at another post's frame. The reader does not check this today: `decodePost` in `reader.go` never compares the frame's ordinal with the ordinal it was asked for.
- **The fix: refuse an entry that cannot describe a frame in the log its header covers.** `ParseIndex` already decodes every entry into memory, on both the trusted and the extended path, so a range check costs a few comparisons per entry and reads no log bytes. An entry is impossible when any of these holds:
  - `Revision == 0`, because `revisionOf` never returns 0;
  - `Offset < FileHeaderSize`;
  - `FrameLength < FrameFixedSize` or `FrameLength > MaxFrameSize`;
  - the frame does not end inside the covered log. The check must not overflow: refuse `Offset > Header.LogLength` first, then `uint64(FrameLength) > Header.LogLength - Offset`. Never compute `Offset + FrameLength`, which can wrap for an `Offset` near the largest `uint64` (Terva review 1946).

  A zeroed entry fails the first two. An impossible entry is `ErrMalformed`. That satisfies `RebuildRequired`, so `Open` rebuilds from the log, and `threadcache.openThread` writes the result back (TKT-01M3VHSYFR).

  In addition, `decodePost` refuses a frame whose header ordinal differs from the one it was asked for. A refusal is a `ReadError` wrapping `ErrMalformed`, the same as for an index that points at a non-post record. With that, a stale but plausible entry can never serve another post's text.

  Alternatives:
  - a header that lags, written only after an index fsync, lost. Between fsyncs, the on-disk header's `PostCount` would be behind the file's entries, so `ParseIndex` would refuse it. Every thread that took posts since its last mutation would then need a full rebuild after any crash, including a plain process kill that loses nothing. That is worse than today for the commonest crash.
  - an index fsync on every batch of posts lost. TKT-01M3VHSYFR measured it and refused it, because it doubles the fsyncs on the most common write.
  - sampling entries against the log on open lost. Sampling can miss the one lost entry. A full check against the log is the replay the trusted path exists to avoid, and the range check above needs no log read at all.
  - measuring first on ext4 and xfs lost. The outcome depends on the mount options an operator chooses (`data=writeback`), so a measurement on one machine proves nothing about another. The check is cheap enough to need no measurement to justify it.
- **What happens after the fix.** A power cut that loses a new entry costs one full replay of that thread on its next open, then a write-back. That is the cost `Rebuilt` already has, and it happens only after a crash that actually lost bytes.
- **Formats.** No change to the log format, the `.idx` layout or `header.go`. `.idx` is derived (docs/03 "Index file format"). The parser becomes stricter, and every index ketju writes satisfies the check, so no existing index is refused. `TestEveryWrittenIndexPassesTheEntryCheck` proves that.

### Approach

1. `internal/store/threadlog/index.go`: add `func (e IndexEntry) possibleIn(logLength uint64) error`, returning an `ErrMalformed` that names the field. Call it from `ParseIndex` for each entry, against `h.LogLength`. `OpenIndex` and the extended path in `recovery.go` both go through `ParseIndex`, so both get the check, and so do `internal/events/search/scan.go`, `cmd/ketju/redact.go` and `fsck`, which open through `threadlog.Open`.
2. `internal/store/threadlog/reader.go` `decodePost`: after `ParseFrame`, if `h.Ordinal != ordinal`, return a `ReadError` with `fmt.Errorf("%w: index entry %d points at the frame of ordinal %d", ErrMalformed, ordinal, h.Ordinal)`. Check that every path into `decodePost` (`ReadEntries`, `One`, `HiddenEntry`) passes the requested ordinal. A blanked `Redacted` post keeps its frame's ordinal. The redaction tests prove it.
3. docs/03:
   - In the write path, step 4, extend the bullet that ends "a new entry lost that way hides nothing (TKT-01M3VRFRFW)". Say that the open path refuses an index holding an entry that cannot describe a frame in the log it covers, so a lost new entry is rebuilt rather than read as zeros, and that the reader checks the frame's ordinal.
   - Under "Index file format", where the paragraph says "if `idx.log_length == stat(log).size` the index is trusted", add "and every entry is possible".
4. Measure the trusted open on a 50 000-post index on main and on the branch. A new `BenchmarkOpenTrusted` in `bench_test.go` calls `threadlog.Open` with the index bytes and a log size that matches. Record both figures in a ticket note. The expected change is under 5 %.

### Tests

The crash property goes in `internal/store/threadlog/actor_trust_test.go`, on top of `journalIndex`:
- Extend `crashImages` with a hole model. For a pending write that extended the file, the image may also have the new length with zero bytes where the write's data would be. That is the "length persisted, data did not" state above. Every pending write that extended the file can independently be absent, present, or a hole.
- `TestATrustedOpenNeverServesALostNewEntry`: start from a durable two-post thread (`twoPostThread`). Append three posts in three separate batches under `SyncGroup`. For every crash image:
  - if `threadlog.Open(log, size, img)` is `Trusted`, its index equals a full rebuild (`Open(log, size, nil)`) entry for entry (`diffIndexes`);
  - otherwise it equals a full rebuild after replay.
  - Every post reads back through `NewReader` with the source it was written with.

  Commit this test alone first, and quote its failure on main for the image with the header persisted and the last entry a hole.
- `TestAnImpossibleEntryIsMalformed` (`index_test.go`), table-driven. These are each refused with `ErrMalformed`, and `RebuildRequired` is true for each:
  - a zero entry;
  - `Offset` 0;
  - `Offset` inside the file header;
  - `FrameLength` below `FrameFixedSize`;
  - `FrameLength` above `MaxFrameSize`;
  - a frame ending one past `LogLength`;
  - `Offset` near `math.MaxUint64` with a valid `FrameLength`, whose sum wraps (`Offset = math.MaxUint64 - 10`, `FrameLength = FrameFixedSize`);
  - `Offset` past `LogLength`;
  - `Revision` 0.

  An entry exactly at the covered end is accepted.
- `TestEveryWrittenIndexPassesTheEntryCheck`: the property test's random sequences from `checkOneSequence` (`property_test.go`) include edits, deletes, restores and redactions. For every sequence, the index the actor wrote and the one a full rebuild writes (`AppendTo`) both parse. The thread events-only case (`TestThreadEventsOnlyStillAgree`) parses as well.
- `TestAnEntryPointingAtAnotherPostsFrameIsAReadError` (`reader_test.go`): an index whose entry 3 holds entry 2's offset and length makes `Range(1, 3)` and `One(3)` return a `ReadError` wrapping `ErrMalformed`, not post 2's text.
- `TestAZeroedNewEntryIsRebuiltOnOpen` (`internal/store/threadcache/writeback_test.go`), through a real `Cache`:
  1. Create a thread with posts 1 to 3 and close it.
  2. Zero the last entry in the `.idx`, keeping its header. This simulates case 2 above.
  3. Reopen. The outcome is `Rebuilt` and post 3 reads its source.
  4. Close and reopen. The outcome is `Trusted`.
- Existing tests keep passing:
  - `TestAReplayedRecordIsWrittenBackBeforeTheIndexIsTrusted`;
  - `TestAMutationIsSyncedToTheIndexBeforeTheHeaderCoversIt`;
  - `TestReplayEqualsIncrementalMaintenance`;
  - the redaction suite (`redact_test.go`, `TestRedactedIndexExtendedEqualsRebuilt`);
  - `crash_test.go`;
  - `TestServeDiscardsIndexesWrittenBeforeWriteBack`.

### Out of scope

- Ordinal bounds on 32-bit builds: TKT-01M3VVZE4G (Compare ordinals as unsigned in index and page bounds on 32-bit builds). Both tickets touch `index.go`, in different functions (`Entry` and `Entries` there, `ParseIndex` here), so whichever lands second rebases.
- A move record setting `EntryMoved`: the moving-posts lane (TKT-01M29CXJZZ, Implement split and merge records in the thread log). The check is on offsets and lengths, which a moved entry keeps.
- Any change to when the actor fsyncs.
