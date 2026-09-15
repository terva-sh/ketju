---
schema: 3
id: TKT-01M29C2WTJ9M9MAAKM0PQ2XZT5
title: Write crash tests truncating the tail frame at every offset
type: task
status: done
status_reason: null
priority: urgent
due_on: null
labels:
  - area:storage
  - area:test
  - effort:M
assignees: []
milestone: phase-0
parent: TKT-01M29BKBKP8C66GXXQK4HPEZ35
origin: null
dependencies:
  - TKT-01M29C0WDWVPADC8A2J4VHCGP2
blocks_on: none
references:
  - ref: design:testing
    path: docs/02-architecture.md
claim: null
archive: null
created_at: 2026-09-11T23:15:46Z
updated_at: 2026-09-15T06:56:02Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/storage
  name: ""
extensions: {}
---

## Description

Take a valid log, truncate it at every byte offset within the final frame, and assert that recovery yields a valid prefix each time. Also truncate mid-header, and corrupt single bytes in the final frame rather than only cutting it short.

A power cut lands wherever it lands. The interesting failures are the ones where the truncated length field still parses as a plausible number, which is exactly why the CRC covers the length.

## Acceptance criteria

- [x] Every truncation offset within the final frame recovers to a valid prefix with no error escaping to a caller
- [x] A truncation that leaves a plausible but wrong length field is caught by CRC rather than accepted
- [x] Recovery of a log truncated inside the 64-byte file header is refused with a clear error rather than producing an empty thread
- [x] The recovered post count is exactly the number of frames that were fully durable

## Summary

`internal/store/threadlog/crash_test.go`, eight tests.

Every cut offset in the whole log, not only the final frame: a crash during a
group commit leaves the tail of a multi-frame write missing, so the cut can
land several frames back. Each one must recover to the largest frame boundary
at or below the cut, hold exactly the records that were fully durable, and
read back end to end. The expected index is replayed to that boundary rather
than counted from the frame list — buildLog writes a thread event, six posts,
an edit, two deletes, a restore and a rename, and my first version asserted
its own arithmetic about that instead of the recovery, which is what the first
failure was.

Plausible-but-wrong lengths: every shorter length that still leaves a complete
frame inside the file, 40 of them, each cut exactly where the forged length
says the frame ends so the frame is complete and wrong rather than short. All
either refused or recovered to the boundary before.

Single-byte corruption at every position, which the ticket asked for beyond
truncation. In the final frame this is indistinguishable from a partly flushed
write, so falling back is correct. Before the final frame it must be refused.

The finding worth carrying into the freeze: that last rule holds everywhere
except the four-byte length field. A length that overruns EOF is exactly what
an interrupted write leaves, and a flipped bit makes the same shape; the CRC
cannot help, because it sits at the end of the frame and the length is what
says where that is. Measured: 35 of 762 byte positions before the final frame
can be flipped and accepted, and every one is inside a length field. My first
test asserted the stronger rule, failed, and the diagnosis is this rather than
a bug to fix.

The consequence is bounded and the bound is what makes it acceptable. A
rebuild never invents a record and never reports a prefix longer than the
truth, so the failure is losing durable records rather than serving wrong
ones, and both bounds are asserted. A rebuild is also the rare path: with a
valid sidecar an ordinary open trusts the index and never walks a frame, so a
corrupt length mid-log truncates nothing — also asserted, for every length
byte.

Recorded in docs/03 under Durability and recovery, since it is a property of
the format rather than of this test, and fsck is where it is meant to be
caught.
