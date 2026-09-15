---
schema: 3
id: TKT-01M29C0WE5GNEYS35WWVCRBWMY
title: Implement the open-thread LRU cache
type: task
status: done
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
updated_at: 2026-09-15T06:44:38Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/storage
  name: ""
extensions: {}
---

## Description

An LRU over open threads, default 512, each holding a decoded index and its actor. Nothing is opened until requested, so startup cost does not scale with the number of threads on disk.

Archived threads drop first and eagerly, because they are read-only and will not be written again. Eviction has to flush the index header and stop the actor cleanly, since an evicted thread that left a stale header costs a replay on the next open.

## Acceptance criteria

- [x] Opening more threads than the bound evicts the least recently used and the evicted thread's index header is flushed first
- [x] An archived thread is evicted ahead of a live one regardless of recency
- [x] Re-opening an evicted thread trusts its index with no replay
- [x] The cache bound is configurable and its default is 512

## Summary

New package `internal/store/threadcache`: 10 tests, green under -race.

Bound defaults to 512 and is configurable; a negative bound is refused rather
than treated as unbounded. Nothing opens until asked for, proven by a fresh
cache over a populated root reporting zero opens.

Eviction prefers archived over recency, then least recently used. The index
header flush is checked against the file — the evicted thread's `.idx` header
must name the whole log — rather than inferred from having called Close, and
a reopened thread is asserted to come back `Trusted` with `Replayed == 0`,
which is the only thing that makes evicting cheaper than keeping.

Archived-ness comes from the caller. The log knows it, since it ends in a
ThreadArchived record, but the index deliberately carries no thread-level
state and finding out would mean a replay on every open — the exact cost the
index exists to avoid. So `Options.Archived` is consulted at open and
`MarkArchived` covers a thread archived while open. Once docs/04's database
exists its projection is the authority.

A referenced thread is never evicted, so the cache sits over its bound while
that many threads are genuinely in use. That is the right way round: the bound
is a memory target, and closing a thread mid-read is a use-after-close.

Concurrent Acquire of one cold thread opens it once. Two actors over one log
would break the single-writer invariant the whole engine rests on, so the
entry goes into the map before the I/O starts and later callers wait on a
channel, taking their reference before the wait so the thread cannot be
evicted between being opened and being handed out.

Two of my own test bugs, both caught by running them: the in-use test asserted
the cache would stay at two threads when the newly created one was the only
evictable one, and an archived-eviction test carried a leftover stat of a path
that could never exist.
