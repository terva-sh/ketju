---
schema: 3
id: TKT-01M29C0WD88M1C8EW4QQTX76NJ
title: Implement the thread actor with ordinal assignment and group commit
type: task
status: draft
status_reason: null
priority: urgent
due_on: null
labels:
  - area:storage
  - effort:L
assignees: []
milestone: phase-0
parent: TKT-01M29BKBKP8C66GXXQK4HPEZ35
origin: null
dependencies:
  - TKT-01M29BYYGNN34ZP3AMFZ540R2H
  - TKT-01M29BYYGYZ8FQB2W2XC14S2T3
blocks_on: none
references:
  - ref: design:storage-write-path
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

One goroutine owns each open thread. Appends arrive over a channel and the caller gets its answer only after the record is durable.

This buys three things at once. Ordinals are assigned without a lock dance because the actor knows post_count. There is exactly one writer per file, so offsets are known before the write and O_APPEND semantics stop mattering. And it is the natural place for group commit: several appends arriving within a few milliseconds are written and fsynced once.

Ordering within step 4 of the docs/03 write path is load-bearing. The index header is updated after the log fsync, so the worst case on a crash is an index claiming to be older than it is, which triggers a harmless replay. The reverse ordering loses data.

## Acceptance criteria

- [ ] Concurrent appends to one thread produce a gapless ordinal sequence with no duplicates, under the race detector
- [ ] Appends arriving within the group window issue one fsync, verified by counting syscalls
- [ ] The index header is written only after the log fsync returns, proven by a test that injects a failure between the two
- [ ] An append returns ordinal, revision, and the new log length to its caller
