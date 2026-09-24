---
schema: 3
id: TKT-01M29BKBKV6HH8W0DN6QXQNZ0R
title: Embedded database, schema, and projections
type: epic
status: draft
status_reason: null
priority: urgent
due_on: null
labels:
  - area:db
  - effort:XL
assignees: []
milestone: phase-1
parent: null
origin: null
dependencies:
  - TKT-01M29C5Y1E782FS135JRVBFRJ6
blocks_on: children
references:
  - ref: design:data-model
    path: docs/04-data-model.md
  - ref: design:architecture
    path: docs/02-architecture.md
claim: null
archive: null
created_at: 2026-09-11T23:07:17Z
updated_at: 2026-09-24T05:05:28Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude/phase1-groom
  name: ""
extensions: {}
---

## Description

`internal/store/db` plus `internal/events`: SQLite in WAL mode through a pure-Go driver, the authoritative tables that only exist here, and the projection tables rebuilt from thread logs.

The rule from docs/04 is that the database holds coordination and projections, never content. Authoritative tables (users, identities, sessions, applications, forums, groups, permissions, audit_log, flags, settings) must be backed up. Projection tables (threads, post_meta, thread_tags, mentions, search, projection_state) are rebuildable with `ketju reindex`.

The in-process event bus connects the two halves. A post append publishes an event, and the projection writer, search indexer, notifier, and SSE fan-out subscribe. Projection is asynchronous but ordered per thread and idempotent, keyed on thread_id and log offset, so a crash between the log write and the database write is repaired on startup by replaying the gap.

## Acceptance criteria

- [ ] Deleting the whole database and running `ketju reindex` reproduces every projection table from the thread logs alone
- [ ] Projection writes are idempotent: replaying the same log range twice leaves identical rows
- [ ] Startup catch-up closes any gap between projection_state.log_length and the actual log length for every thread
- [ ] Permission evaluation returns the union of the viewer's groups' rows for a forum, falling back to global rows
- [ ] No table in the schema holds post source or rendered HTML

## Notes

**agent:claude/phase1-groom** at 2026-09-24T05:05:28Z

Dependency changed during Phase 1 grooming (2026-09-24). This epic depended on the Phase 0 storage epic TKT-01M29BKBKP (Thread log storage engine). That epic blocks on all its children, and four of those are Phase 2 and Phase 4 work: hard redaction, the redaction tool, thread archival, and split and merge. So the database epic could never close in Phase 1. What the database layer needs from storage is a stable log format, and that is TKT-01M29C5Y1E (Freeze the thread log format at version 1), which is done. The dependency now points there. Keeping the epic-to-epic link lost, because it expresses 'storage finished' where the real need is 'format frozen'.
