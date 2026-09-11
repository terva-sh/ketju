---
schema: 3
id: TKT-01M29C0WE5GNEYS35WWVCRBWMY
title: Implement the open-thread LRU cache
type: task
status: draft
status_reason: null
priority: high
due_on: null
labels:
  - area:storage
  - effort:S
assignees: []
milestone: phase-0
parent: TKT-01M29BKBKP8C66GXXQK4HPEZ35
origin: null
dependencies:
  - TKT-01M29BYYGYZ8FQB2W2XC14S2T3
blocks_on: none
references:
  - ref: design:storage-caching
    path: docs/03-thread-storage.md
claim: null
archive: null
created_at: 2026-09-11T23:14:40Z
updated_at: 2026-09-11T23:14:40Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:terva/mieli
  name: Mieli
extensions: {}
---

## Description

An LRU over open threads, default 512, each holding a decoded index and its actor. Nothing is opened until requested, so startup cost does not scale with the number of threads on disk.

Archived threads drop first and eagerly, because they are read-only and will not be written again. Eviction has to flush the index header and stop the actor cleanly, since an evicted thread that left a stale header costs a replay on the next open.

## Acceptance criteria

- [ ] Opening more threads than the bound evicts the least recently used and the evicted thread's index header is flushed first
- [ ] An archived thread is evicted ahead of a live one regardless of recency
- [ ] Re-opening an evicted thread trusts its index with no replay
- [ ] The cache bound is configurable and its default is 512
