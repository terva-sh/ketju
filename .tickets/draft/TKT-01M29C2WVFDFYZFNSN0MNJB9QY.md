---
schema: 3
id: TKT-01M29C2WVFDFYZFNSN0MNJB9QY
title: Ship ketju dump
type: task
status: draft
status_reason: null
priority: high
due_on: null
labels:
  - area:storage
  - area:ops
  - effort:S
assignees: []
milestone: phase-0
parent: TKT-01M29BKBKP8C66GXXQK4HPEZ35
origin: null
dependencies:
  - TKT-01M29BYYGNN34ZP3AMFZ540R2H
blocks_on: none
references:
  - ref: design:storage-recovery
    path: docs/03-thread-storage.md
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

Print a thread log as JSON lines, one object per record, for inspection. This is the debugging tool for every storage bug that follows, so it is worth having before the bugs arrive.

It reads a log directly without opening the database or trusting the index, which is what makes it useful when the index or the database is the thing that is wrong.

## Acceptance criteria

- [ ] Output is one JSON object per line carrying the frame header fields and the decoded body
- [ ] The command works on a log whose index is missing or corrupt
- [ ] A record failing CRC is reported on its own line rather than aborting the dump
- [ ] Offsets are included so a line can be correlated with an index entry
