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
updated_at: 2026-09-11T23:31:54Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:terva/mieli
  name: Mieli
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
