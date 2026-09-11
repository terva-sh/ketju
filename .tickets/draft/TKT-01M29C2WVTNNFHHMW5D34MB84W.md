---
schema: 3
id: TKT-01M29C2WVTNNFHHMW5D34MB84W
title: Ship neoforum fsck
type: task
status: draft
status_reason: null
priority: high
due_on: null
labels:
  - area:storage
  - area:ops
  - effort:M
assignees: []
milestone: phase-0
parent: TKT-01M29BKBKP8C66GXXQK4HPEZ35
origin: null
dependencies:
  - TKT-01M29C0WDWVPADC8A2J4VHCGP2
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

Walk every log, verify every CRC, rebuild every index, and report drift between logs and database projections.

The drift report is the part that earns its keep. An index can be rebuilt whenever, but a projection silently behind its log is the failure mode that looks like "a post vanished" to a member and looks like nothing at all in the logs.

## Acceptance criteria

- [ ] Every log is walked and every CRC verified, with a summary naming any damaged thread
- [ ] Indexes are rebuilt and the command is safe to run against a live data directory
- [ ] Drift between each log length and its projection_state is reported per thread
- [ ] Exit code distinguishes clean, drift found, and corruption found
