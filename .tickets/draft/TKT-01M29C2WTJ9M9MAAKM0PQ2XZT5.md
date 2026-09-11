---
schema: 3
id: TKT-01M29C2WTJ9M9MAAKM0PQ2XZT5
title: Write crash tests truncating the tail frame at every offset
type: task
status: draft
status_reason: null
priority: urgent
due_on: null
labels:
  - area:storage
  - area:test
  - effort:M
assignees: []
milestone: phase-0
parent: TKT-01M29BKBKP8C66GXXQK4HPEZ35
origin: null
dependencies:
  - TKT-01M29C0WDWVPADC8A2J4VHCGP2
blocks_on: none
references:
  - ref: design:testing
    path: docs/02-architecture.md
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

Take a valid log, truncate it at every byte offset within the final frame, and assert that recovery yields a valid prefix each time. Also truncate mid-header, and corrupt single bytes in the final frame rather than only cutting it short.

A power cut lands wherever it lands. The interesting failures are the ones where the truncated length field still parses as a plausible number, which is exactly why the CRC covers the length.

## Acceptance criteria

- [ ] Every truncation offset within the final frame recovers to a valid prefix with no error escaping to a caller
- [ ] A truncation that leaves a plausible but wrong length field is caught by CRC rather than accepted
- [ ] Recovery of a log truncated inside the 64-byte file header is refused with a clear error rather than producing an empty thread
- [ ] The recovered post count is exactly the number of frames that were fully durable
