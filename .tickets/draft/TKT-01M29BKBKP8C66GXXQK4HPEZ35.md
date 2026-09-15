---
schema: 3
id: TKT-01M29BKBKP8C66GXXQK4HPEZ35
title: Thread log storage engine
type: epic
status: draft
status_reason: null
priority: urgent
due_on: null
labels:
  - area:storage
  - effort:XL
assignees: []
milestone: phase-0
parent: null
origin: null
dependencies: []
blocks_on: children
references:
  - ref: design:thread-storage
    path: docs/03-thread-storage.md
  - ref: design:roadmap-phase-0
    path: docs/09-roadmap.md
claim: null
archive: null
created_at: 2026-09-11T23:07:17Z
updated_at: 2026-09-15T07:33:06Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/storage
  name: ""
extensions: {}
---

## Description

`internal/store/threadlog`: the append-only record log and sidecar offset index that hold every post body. This is the only clever part of the system and the riskiest, so docs/09 puts it first and proves it before any web code exists.

The log is the source of truth for content. The database and every index file are derived and rebuildable. The log length doubles as the thread's version number, which drives ETags, the response cache key, and live-update hints.

Scope: file header and record frame formats, CRC32C verification, the 40-byte index entry, the single-writer thread actor with group commit, the concurrent read path, the open-thread LRU, tail recovery, and the `dump` and `fsck` commands. Format freeze at version 1 is the exit gate for Phase 0.

## Acceptance criteria

- [ ] Format version 1 is frozen with reserved bytes, and every later change is a version bump with a migration path
- [ ] Property tests show a full replay rebuild of the index equals the incrementally maintained index for random record sequences
- [ ] Crash tests truncate a log at every byte offset of the tail frame and recovery yields a valid prefix every time
- [ ] Benchmark on a 50k-post synthetic thread meets append p99 under 10 ms with group fsync, warm random-ordinal read under 100 us, and index rebuild of 100 MB under 2 s
- [ ] A thread is fully reconstructible from its log alone with the index and all database rows deleted

## Notes

**agent:claude-code/storage** at 2026-09-15T07:33:06Z

Phase 0 is complete as of 2026-09-15. This epic is not, and should not be
closed yet.

All 19 of this epic's Phase 0 children are done, ending with the format freeze
TKT-01M29C5Y1E. The four that remain are later phases and are correctly still
in draft:

  TKT-01M29BSPE2  phase-2  Decide hard redaction semantics
  TKT-01M29CXJZ9  phase-2  Build the offline hard-redaction tool
  TKT-01M29CRJ64  phase-4  Implement thread archival
  TKT-01M29CXJZZ  phase-4  Implement split and merge records in the thread log

This epic blocks on its children, so it closes when those land rather than
when Phase 0 does. Leaving it open is the accurate state: the storage engine
has a frozen format and a complete Phase 0 surface, and it still owes
redaction, archival, and split/merge.

Two things the freeze fixed that those four inherit.

Split and merge (TKT-01M29CXJZZ) now has a hard ceiling. A single
PostsSplitOut record can name at most about 349 000 ordinals below 65536, or
209 000 above, because every record type shares MaxFrameSize at 1 MiB. A
larger split must be carried by more than one record. Types 20 and 21 are held
rather than unassigned, so the numbers are waiting.

Hard redaction (TKT-01M29BSPE2, TKT-01M29CXJZ9) is the one operation in this
design that is not append-only, and docs/03 item 4 in open questions records
that a Redacted marker takes a number in the CRITICAL range 0x80-0xFF, not the
advisory one. A reader that skips a redaction marker shows content that was
ordered removed, which is the exact case the critical partition exists for.
