---
schema: 3
id: TKT-01M29C0WDWVPADC8A2J4VHCGP2
title: Implement tail recovery and index repair on open
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
  - TKT-01M29BYYGYZ8FQB2W2XC14S2T3
  - TKT-01M29BYYGNN34ZP3AMFZ540R2H
blocks_on: none
references:
  - ref: design:storage-recovery
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

Opening a thread is: read both headers, and if the index's recorded log length equals `stat(log).size` the index is trusted. Otherwise replay records from that length to the end and repair. A missing or corrupt index is rebuilt by a full replay, which is a sequential read of a 100 MB log and takes well under a second.

A torn final frame with a bad length or a bad CRC is truncated to the last good frame boundary. That is safe precisely because we are the only writer and every frame is CRC'd, and it is the one place where the engine deletes bytes on purpose.

## Acceptance criteria

- [ ] An index whose recorded log length matches the file size is trusted with no replay, counted in a test
- [ ] A short index triggers replay from its recorded length rather than a full rebuild
- [ ] A torn final frame is truncated to the last good boundary and the thread opens with a valid prefix
- [ ] A missing or CRC-failing index is rebuilt by full replay and matches the incrementally maintained index
