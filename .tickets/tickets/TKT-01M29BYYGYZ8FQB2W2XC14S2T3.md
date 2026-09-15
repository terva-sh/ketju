---
schema: 3
id: TKT-01M29BYYGYZ8FQB2W2XC14S2T3
title: Implement the offset index file and the in-memory index
type: task
status: ready
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
  - TKT-01M29BW9PTAQ34EC78E61AHZM0
blocks_on: none
references:
  - ref: design:storage-index
    path: docs/03-thread-storage.md
claim: null
archive: null
created_at: 2026-09-11T23:13:37Z
updated_at: 2026-09-15T00:52:59Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/storage
  name: ""
extensions: {}
---

## Description

The 32-byte index header and the 40-byte entry per ordinal, so the entry for ordinal n sits at `32 + 40*(n-1)` and a seek to any post is arithmetic rather than a scan.

The entry carries more than an offset on purpose: reply_to, author, created_at, revision, and flags. That is what lets the chain, author, and since lenses run without a database query, and what lets a tombstone be answered without reading the log at all.

The header records the log length the index is valid up to. That single field is what makes trusting an index a comparison against `stat()` rather than an act of faith.

## Acceptance criteria

- [ ] Entry for ordinal n is read with one pread at the computed offset with no scan
- [ ] Header CRC covers the header and a corrupt header forces a rebuild rather than a bad read
- [ ] Tombstoned and moved posts are answerable from index flags with no log read, proven by a test that opens the log file read-only and counts reads
- [ ] The in-memory form of a 50k-post index stays within the 2 MB stated in docs/03
