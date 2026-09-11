---
schema: 3
id: TKT-01M29C2WV6G5AS18B6B5S3MG8W
title: Benchmark the storage engine against the Phase 0 targets
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
  - TKT-01M29C0WD88M1C8EW4QQTX76NJ
  - TKT-01M29C0WDJ9FETR8EFZ7ED5JYF
blocks_on: none
references:
  - ref: design:roadmap-phase-0
    path: docs/09-roadmap.md
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

A 50k-post synthetic thread and the four numbers docs/09 makes the Phase 0 exit criterion: append p99 under 10 ms with group fsync, random-ordinal read under 100 us warm, cold read from page cache miss, and index rebuild of a 100 MB log under 2 s.

Record the hardware. A number without the disk it was measured on is not a benchmark, and the whole point of this exercise is to know the envelope before web code makes it hard to measure.

If a target is missed, that is a finding rather than a failure. Say which target, by how much, and what would have to change.

## Acceptance criteria

- [ ] All four measurements are recorded on this ticket with the hardware and filesystem named
- [ ] The benchmark is repeatable from a committed command rather than an ad-hoc script
- [ ] Each target is either met or has a written explanation of the gap and the options
- [ ] Results are compared across the fsync policies so the default is chosen on evidence
