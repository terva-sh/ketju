---
schema: 3
id: TKT-01M29C2WW5N8TR3V76VEB3TGK7
title: Build the projection writer
type: task
status: draft
status_reason: null
priority: urgent
due_on: null
labels:
  - area:db
  - effort:L
assignees: []
milestone: phase-1
parent: TKT-01M29BKBKV6HH8W0DN6QXQNZ0R
origin: null
dependencies:
  - TKT-01M29C0WF9GFGVG7QSAYRAKAA4
  - TKT-01M29BYYJ87H5ERS3RXC66XF44
  - TKT-01M29C0WDJ9FETR8EFZ7ED5JYF
blocks_on: none
references:
  - ref: design:storage-write-path
    path: docs/03-thread-storage.md
  - ref: design:data-model-projections
    path: docs/04-data-model.md
claim: null
archive: null
created_at: 2026-09-11T23:15:46Z
updated_at: 2026-09-11T23:15:46Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:terva/mieli
  name: Mieli
extensions: {}
---

## Description

The subscriber that turns log records into rows: `threads`, `post_meta`, `thread_tags`, and `mentions`, advancing `projection_state.log_length` as it goes.

Ordered per thread and idempotent, keyed on thread id and log offset. Idempotence is not an optimization here. It is what makes replay after a crash safe, and every projection of every record type has to have it, including the thread events that mutate a single row.

The log is the source of truth and the database catches up to it. Nothing in this writer may ever write back into a log.

## Acceptance criteria

- [ ] Replaying the same log range twice produces identical rows, tested per record type
- [ ] Projection of one thread's records happens in log order even under concurrent events from other threads
- [ ] Every record type in docs/03 has a projection or an explicit decision that it has none
- [ ] A projection failure is retried and surfaced as lag rather than dropping the event
