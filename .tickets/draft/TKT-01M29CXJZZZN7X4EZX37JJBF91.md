---
schema: 3
id: TKT-01M29CXJZZZN7X4EZX37JJBF91
title: Implement split and merge records in the thread log
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:storage
  - effort:L
assignees: []
milestone: phase-4
parent: TKT-01M29BKBKP8C66GXXQK4HPEZ35
origin: null
dependencies:
  - TKT-01M29C5Y1E782FS135JRVBFRJ6
  - TKT-01M29C0WD88M1C8EW4QQTX76NJ
blocks_on: none
references:
  - ref: design:storage-records
    path: docs/03-thread-storage.md
claim: null
archive: null
created_at: 2026-09-11T23:30:21Z
updated_at: 2026-09-11T23:30:21Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:terva/mieli
  name: Mieli
extensions: {}
---

## Description

`PostsSplitOut` and `PostsMergedIn`, the hardest events in the format and the reason their type numbers were reserved at the freeze.

Posts are copied into the target thread's log as new Post records carrying an `origin {thread_id, ordinal}`, and the source posts get a moved flag and render as a pointer. Ordinals in the source are never renumbered, which is what preserves every permalink ever issued.

Two logs change, and they cannot change atomically. Decide the order and the recovery: which log is written first, and what a crash between them leaves behind.

## Acceptance criteria

- [ ] Source ordinals are unchanged after a split and every source permalink still resolves
- [ ] Copied posts carry their origin reference and render with attribution to the original author
- [ ] A crash between the two log writes leaves a state that recovery resolves deterministically, tested
- [ ] Projections for both threads are correct after replay from logs alone
