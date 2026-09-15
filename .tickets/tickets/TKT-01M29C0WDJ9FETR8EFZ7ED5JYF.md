---
schema: 3
id: TKT-01M29C0WDJ9FETR8EFZ7ED5JYF
title: Implement the concurrent read path with coalesced reads
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
  - TKT-01M29BYYGYZ8FQB2W2XC14S2T3
blocks_on: none
references:
  - ref: design:storage-read-path
    path: docs/03-thread-storage.md
claim: null
archive: null
created_at: 2026-09-11T23:14:40Z
updated_at: 2026-09-15T03:16:33Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/storage
  name: ""
extensions: {}
---

## Description

Reading a range of ordinals: slice the in-memory index, then `ReadAt` each record, verify its CRC, and decode. Readers never take the actor's channel, because `os.File.ReadAt` is safe for concurrent use and a reader queueing behind a writer would make a busy thread slow to read.

When consecutive entries are contiguous on disk, which is the common case for unedited posts, the reader coalesces them into one `ReadAt` and slices the buffer. That is the difference between 25 syscalls and one for a typical page.

No mmap in v1. It adds portability and lifecycle complexity for little gain at this scale, and the OS page cache already does the work. Revisit only if profiling says otherwise.

## Acceptance criteria

- [ ] A page of contiguous posts is fetched with one ReadAt, counted in a test
- [ ] Reads run concurrently with appends to the same thread under the race detector with no reader ever blocking on the actor
- [ ] A record failing its CRC returns an error naming the ordinal rather than returning bytes
- [ ] Tombstoned and moved posts return from the index without a log read
