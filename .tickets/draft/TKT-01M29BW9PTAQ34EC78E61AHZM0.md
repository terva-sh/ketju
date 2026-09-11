---
schema: 3
id: TKT-01M29BW9PTAQ34EC78E61AHZM0
title: Implement the log header, record frame, and CRC32C codec
type: task
status: draft
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
  - TKT-01M29BSPDQRV0P5JQN866V1FKN
blocks_on: none
references:
  - ref: design:storage-format
    path: docs/03-thread-storage.md
claim: null
archive: null
created_at: 2026-09-11T23:12:10Z
updated_at: 2026-09-11T23:12:10Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:terva/mieli
  name: Mieli
extensions: {}
---

## Description

The 64-byte file header and the record frame from docs/03, with CRC32C over both.

The frame splits deliberately: a fixed 32-byte header holding type, encoding, flags, ordinal, revision, timestamp, and actor, then the variable body. The split is what lets the reindexer and the index builder walk an entire log without decoding a single body.

Reserved bytes exist in both structures and stay reserved. They are the escape hatch for segment rollover if a log ever approaches the tens-of-GB range.

## Acceptance criteria

- [ ] Encode and decode round-trip every field of the header and the frame
- [ ] A frame with a corrupted body fails its CRC check and is reported rather than returned
- [ ] A frame header can be parsed and skipped without decoding the body, and a test walks a log doing exactly that
- [ ] Reserved bytes are written as zero and a non-zero value in them is not an error, so a future version stays readable
