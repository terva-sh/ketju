---
schema: 3
id: TKT-01M29C0WDWVPADC8A2J4VHCGP2
title: Implement tail recovery and index repair on open
type: task
status: done
status_reason: null
priority: urgent
due_on: null
labels:
  - area:storage
  - effort:M
assignees: []
milestone: phase-0
parent: TKT-01M29BKBKP8C66GXXQK4HPEZ35
origin: null
dependencies:
  - TKT-01M29BYYGYZ8FQB2W2XC14S2T3
  - TKT-01M29BYYGNN34ZP3AMFZ540R2H
blocks_on: none
references:
  - ref: design:storage-recovery
    path: docs/03-thread-storage.md
claim: null
archive: null
created_at: 2026-09-11T23:14:40Z
updated_at: 2026-09-15T03:22:08Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/storage
  name: ""
extensions: {}
---

## Description

Opening a thread is: read both headers, and if the index's recorded log length equals `stat(log).size` the index is trusted. Otherwise replay records from that length to the end and repair. A missing or corrupt index is rebuilt by a full replay, which is a sequential read of a 100 MB log and takes well under a second.

A torn final frame with a bad length or a bad CRC is truncated to the last good frame boundary. That is safe precisely because we are the only writer and every frame is CRC'd, and it is the one place where the engine deletes bytes on purpose.

## Acceptance criteria

- [x] An index whose recorded log length matches the file size is trusted with no replay, counted in a test
- [x] A short index triggers replay from its recorded length rather than a full rebuild
- [x] A torn final frame is truncated to the last good boundary and the thread opens with a valid prefix
- [x] A missing or CRC-failing index is rebuilt by full replay and matches the incrementally maintained index

## Implementation plan

Two files. `apply.go` holds `(*Index).Apply`, the single definition of what one
record does to the index. `recovery.go` holds `Open`, which is the docs/03 open
path, plus `(*Recovery).Truncate`.

Apply is deliberately shared rather than duplicated. The index is built two ways
— by full replay here, and incrementally by the thread actor in TKT-01M29C0WD8 —
and criterion 4 requires them to agree. Sharing the fold makes them agree by
construction instead of by two people implementing the same table twice. It also
means TKT-01M29C2WT7's property test is guarding against future divergence
rather than discovering present divergence, which is the right job for it.

`Open` reports which of three things happened — Trusted, Extended, Rebuilt —
because they cost a stat comparison, a few kilobytes, and a whole-file read
respectively, and an operator seeing Rebuilt on every open has a problem no
error will ever mention.

Recovery reports the valid prefix; `Truncate` performs the cut. Splitting them
keeps the one destructive act in this engine explicit and separately testable.

## Notes

**agent:claude-code/storage** at 2026-09-15T03:21:43Z

The first implementation had a real bug and the test caught it, which is worth
recording because the failure was silent and expensive.

Distinguishing a torn write from corruption was written as "do the remaining
bytes reach EOF". In a single-window read `rest` ALWAYS reaches EOF, so every
frame looked like the tail, and a single flipped byte in frame 2 of a 12-frame
log truncated it to one post and returned success. Truncating a torn tail
discards bytes nobody was promised; truncating corruption discards posts that
were acknowledged durable. Those are not interchangeable and the first version
treated them as the same thing.

What makes it decidable is the shape of a pure truncation. The log is cut inside
the final frame, so either the 4-byte length field survived intact — it then
reports the true length and the file is too short to hold it — or it did not, and
there are fewer than 32 bytes left so the header will not parse. BOTH are
short-buffer shapes. Neither is a checksum failure.

So a checksum failure means every byte the frame claimed was present and one is
wrong: corruption. The single exception is a frame ending exactly at EOF, where
a partially flushed final write can leave something whole in length and wrong in
content, and that is the case docs/03 says the CRC covers the length field for.

One ambiguity is NOT resolved and should not be papered over: a frame whose
length field claims more than the file holds is the ordinary signature of a cut
tail, and is also exactly what a corrupted length field in a mid-log frame looks
like. Nothing at this layer can tell them apart — the CRC that would settle it
covers bytes the frame says are present and are not. Treated as a tear here,
because that is overwhelmingly the common case, and flagged in the code for
`ketju fsck` (TKT-01M29C2WVT), which has the whole file and an operator to
answer to.

Smaller things a reviewer should know:

The replay window is 2 * MaxFrameSize rather than 1 MB, so a single frame can
never straddle two windows. That turns "nothing whole in this window" into a
statement about the file instead of about the buffer, which is what the tear
logic needs.

docs/03 gives the frame a 4-byte revision and the index entry 2. `revisionOf`
saturates at 65535 rather than wrapping: a wrapped revision reads as 0 and
breaks the render cache key, while a stuck maximum only stops an "edited N
times" counter climbing. 65535 edits to one post is not a real thread.

`Apply` does not touch CreatedMS or Author on an Edit. CreatedMS is revision 1's
timestamp so "since my last visit" does not resurface a four-year-old post
because somebody fixed a typo; Author stays the post's author so the author lens
keeps answering about authorship rather than about who last edited.

This is where `FrameFlags.ToEntryFlags` from TKT-01M2BXKF8F gets its first
caller, and the test asserts a machine-authored frame flag reaches the entry.

## Summary

`apply.go` and `recovery.go`. Open reads both headers, trusts an index whose
recorded log length equals stat(), extends a short one from where it stopped,
and rebuilds a missing or corrupt one by full replay. A torn final write is cut
back to the last good frame boundary. All four criteria met, 17 tests.

The costs are measured rather than asserted, through a read-counting reader. A
trusted index costs exactly the 64-byte file header and not one byte more. An
extended index reads only from its recorded length to the end and never
re-reads the prefix. The torn-tail test sweeps EVERY offset inside the final
frame, not a chosen one.

(*Index).Apply is the single definition of what a record does to the index, and
it is shared on purpose: the index is built by full replay here and will be
built incrementally by the actor in TKT-01M29C0WD8, and criterion 4 requires
the two to agree. Sharing the fold makes them agree by construction rather than
by two people implementing the same table twice, which also means
TKT-01M29C2WT7's property test guards against future divergence instead of
discovering present divergence.

The first implementation had a bug the tests caught, and it is the one worth
remembering. Telling a tear from corruption was written as "do the remaining
bytes reach EOF", which is true of every frame when the window covers the whole
log — so one flipped byte in frame 2 of a 12-frame log truncated it to a single
post and returned success. Discarding a torn tail loses bytes nobody was
promised; discarding corruption loses posts acknowledged as durable.

The rule that replaced it comes from what a truncation actually looks like.
Cut inside the final frame, either the length field survived and reports a
length the file cannot hold, or it did not and there are under 32 bytes left:
both are short-buffer shapes, neither is a checksum failure. So a checksum
failure means the frame was all there and one byte is wrong — corruption —
except when the frame ends exactly at EOF, which is the partially-flushed final
write the CRC covers the length field for.

One ambiguity is left open and documented rather than hidden: a length field
claiming more than the file holds is both the ordinary cut-tail signature and
exactly what a corrupted mid-log length looks like. Nothing at this layer can
separate them. Treated as a tear because that is the common case, and flagged
in the code for ketju fsck (TKT-01M29C2WVT), which has the whole file and an
operator to answer to.

Also the first real caller of FrameFlags.ToEntryFlags from TKT-01M2BXKF8F: a
machine-authored frame flag is asserted to reach the index entry.
