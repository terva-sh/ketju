---
schema: 3
id: TKT-01M29CTY9D04J3NR7E5HH59M24
title: Ship neoforum reindex --search
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:search
  - area:ops
  - effort:S
assignees: []
milestone: phase-3
parent: TKT-01M29BN36D34YVKVRPNQD3PHDV
origin: null
dependencies:
  - TKT-01M29CRJ3BEMZREJQRGAK3AVGK
blocks_on: none
references:
  - ref: design:storage-recovery
    path: docs/03-thread-storage.md
claim: null
archive: null
created_at: 2026-09-11T23:28:54Z
updated_at: 2026-09-11T23:28:54Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:terva/mieli
  name: Mieli
extensions: {}
---

## Description

Rebuild the full-text index from the thread logs.

The search index is the largest object in the database and the most disposable. Being able to drop and rebuild it is what lets an operator recover from a corrupt index, change the tokenizer, or shrink a database without touching content.

## Acceptance criteria

- [ ] Dropping the FTS tables and rebuilding produces the same results as the incremental path
- [ ] Progress is reported and the run is interruptible without leaving a half-built index in service
- [ ] The command works while the server is stopped, using the data directory alone
