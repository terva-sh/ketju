---
schema: 3
id: TKT-01M29C5Y1E782FS135JRVBFRJ6
title: Freeze the thread log format at version 1
type: chore
status: done
status_reason: null
priority: urgent
due_on: null
labels:
  - area:storage
  - effort:S
  - gate
assignees: []
milestone: phase-0
parent: TKT-01M29BKBKP8C66GXXQK4HPEZ35
origin: null
dependencies:
  - TKT-01M29C2WT7BQFDACAJR9HHYMAM
  - TKT-01M29C2WTJ9M9MAAKM0PQ2XZT5
  - TKT-01M29C2WTVSF6BJT5H71GQKBVX
  - TKT-01M29C2WV6G5AS18B6B5S3MG8W
  - TKT-01M29PARE1WYBA7PAKRWSN5QD0
  - TKT-01M29GGG3W51QT2BBABXW5AMVD
  - TKT-01M2BXKF8FWX1WFGJG5E4D48J1
blocks_on: none
references:
  - ref: design:roadmap-phase-0
    path: docs/09-roadmap.md
claim: null
archive: null
created_at: 2026-09-11T23:17:26Z
updated_at: 2026-09-15T07:30:24Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/storage
  name: ""
extensions: {}
---

## Description

The Phase 0 exit gate. Declare the format frozen at version 1 and commit to the consequence: every change after this is a version bump with a migration path, and `ketju migrate-logs` rewriting into a new directory then swapping is the shape that migration takes.

This is a decision to record rather than code to write, and it only means something if it happens on purpose at a moment when all the evidence is in. That is why it depends on the property tests, the crash tests, the fuzzing, and the benchmark. A format frozen before the benchmark is a format frozen on hope.

Check the reserved bytes before signing. They are the only cheap room for a later change, and the segment rollover scenario is the one they exist for.

## Acceptance criteria

- [x] docs/03 records the format as frozen at version 1 with the date
- [x] Reserved bytes in the file header, frame header, and index entry are confirmed sufficient for segment rollover
- [x] Every reserved record type number is documented, including those held for split and merge
- [x] The version bump and migration procedure is written down before it is needed

## Notes

**agent:claude-code/review** at 2026-09-12T22:48:58Z

Re-examine MaxFrameSize before the freeze. Requested by the owner when
accepting TKT-01M29BW9PT (Implement the log header, record frame, and CRC32C
codec) out of review.

`internal/store/threadlog` caps the total on-disk size of one frame at 1 MiB.
That number was chosen while implementing the codec, on the reasoning that
docs/07 caps a post body at 64 KB and attachments live in blobs rather than in
the log, making 1 MiB roughly sixteen times the largest legitimate frame. It
was a defensive bound against a corrupt length field being believed and sized
from, not a considered format parameter.

It is a format parameter now, because a reader that refuses a frame the writer
was willing to produce is a compatibility break. Worth settling explicitly at
the freeze:

- Whether 16x headroom over the 64 KB body cap is the right margin, given that
  every record type shares this one bound, not only posts.
- Whether any record type in the catalogue (TKT-01M29BYYGN) can legitimately
  carry a body far larger than a post body, which would make 1 MiB tight rather
  than generous.
- Whether the cap belongs in the frozen format at all, or is a reader-side
  policy that a future version may raise without a version bump. These are
  different commitments and the freeze should say which one it is making.

The current check is `l > MaxFrameSize-8` in ParseFrameHeader, bounding the
length field before anything is allocated from it.

## Summary

The thread log format is frozen at version 1 as of 2026-09-15. docs/03 carries
a "Format freeze" section stating the date, what frozen means, the evidence it
rests on, the reserved-space audit, the MaxFrameSize decision, which record
type numbers are held, and the migration procedure.

Reserved space audited before signing. File header has 24 bytes spare, ample.
Index entry has 4 bytes at offset 36, which is the uint32 segment number
segment rollover needs, and that is sufficient. Two structures have no spare
bytes and both are fine for reasons now written down rather than left to be
rediscovered: the frame fixed part needs none, because a frame does not record
its own position and rollover changes where frames are found rather than what
they contain; the index header needs none, because the index is derived and
disposable, so changing its layout costs a magic bump and one rebuild per
thread, which fsck already does at 100 MB in 178 ms.

MaxFrameSize settled at 1 MiB, answering the review note from TKT-01M29BW9PT.
It stands, but the margin is not what was assumed. The original reasoning was
16x headroom over a 64 KB post body; that is true of Post and false of the
format, because every record type shares the bound. The binding case is
PostsSplitOut carrying ordinals[]: a split naming all 50 000 posts of the
largest plausible thread is about 150 KB with ordinals under 65536 and 250 KB
above it, so the real margin is 4.2x to 7.2x. Comfortable, and not 16x.

The ceiling that implies is now recorded for whoever implements split in Phase
4: one PostsSplitOut can name at most about 349 000 ordinals below 65536 or
209 000 above, and a larger split needs more than one record. Cheaper to write
down than to discover from a refused frame.

20 and 21 are documented as held rather than free — named in the catalogue,
reported by RecordType.Reserved, refused rather than skipped by a reader. Every
other number is unassigned rather than reserved, and the difference is stated.

Migration written down before it is needed: bump the version and the magic,
migrate-logs into a new root never in place, verify the new root with fsck,
swap by rename with the old tree surviving, rebuild indexes rather than
migrating them. The swap being a rename is what makes the rollback a rename.

The constant in frame.go now carries the same reasoning and points at the
freeze section, so the number and the document cannot drift apart.
