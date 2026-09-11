---
schema: 3
id: TKT-01M29CXK35WS83Z6CAZBAVDFRD
title: Ship neoforum backup and the backup documentation
type: task
status: draft
status_reason: null
priority: high
due_on: null
labels:
  - area:ops
  - effort:M
assignees: []
milestone: phase-4
parent: TKT-01M29BN36Q9D32WQGYPFS336J0
origin: null
dependencies:
  - TKT-01M29BW9PB11V2AHE52C0F49JG
  - TKT-01M29CCBYX4V6ZP7N9HF8H8NCZ
blocks_on: none
references:
  - ref: design:open-questions-ops
    path: docs/10-open-questions.md
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

A command that takes a consistent snapshot: pause the thread actors, checkpoint the WAL, `VACUUM INTO` the database, and copy the data directory. Plus the documented `rsync` procedure for operators who prefer their own tooling.

The claim in docs/00 is that backups are `rsync`. That is true for the logs and blobs, which are immutable once written, and not quite true for the database, which is why the command exists.

A backup nobody has restored is a guess. The restore procedure is part of this ticket, tested end to end.

## Acceptance criteria

- [ ] The command produces a snapshot that restores to a working instance, tested end to end
- [ ] Thread actors are paused only for the checkpoint window and appends resume afterwards
- [ ] The documented rsync procedure is written and its consistency caveats stated
- [ ] Restore documentation covers the case where only the database is lost and content comes from reindex
