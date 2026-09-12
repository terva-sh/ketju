---
schema: 3
id: TKT-01M29C5Y1E782FS135JRVBFRJ6
title: Freeze the thread log format at version 1
type: chore
status: draft
status_reason: null
priority: urgent
due_on: null
labels:
  - area:storage
  - effort:S
  - gate
assignees: []
milestone: phase-0
parent: TKT-01M29BKBKP8C66GXXQK4HPEZ35
origin: null
dependencies:
  - TKT-01M29C2WT7BQFDACAJR9HHYMAM
  - TKT-01M29C2WTJ9M9MAAKM0PQ2XZT5
  - TKT-01M29C2WTVSF6BJT5H71GQKBVX
  - TKT-01M29C2WV6G5AS18B6B5S3MG8W
  - TKT-01M29PARE1WYBA7PAKRWSN5QD0
  - TKT-01M29GGG3W51QT2BBABXW5AMVD
blocks_on: none
references:
  - ref: design:roadmap-phase-0
    path: docs/09-roadmap.md
claim: null
archive: null
created_at: 2026-09-11T23:17:26Z
updated_at: 2026-09-12T02:15:23Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:terva/mieli
  name: Mieli
extensions: {}
---

## Description

The Phase 0 exit gate. Declare the format frozen at version 1 and commit to the consequence: every change after this is a version bump with a migration path, and `ketju migrate-logs` rewriting into a new directory then swapping is the shape that migration takes.

This is a decision to record rather than code to write, and it only means something if it happens on purpose at a moment when all the evidence is in. That is why it depends on the property tests, the crash tests, the fuzzing, and the benchmark. A format frozen before the benchmark is a format frozen on hope.

Check the reserved bytes before signing. They are the only cheap room for a later change, and the segment rollover scenario is the one they exist for.

## Acceptance criteria

- [ ] docs/03 records the format as frozen at version 1 with the date
- [ ] Reserved bytes in the file header, frame header, and index entry are confirmed sufficient for segment rollover
- [ ] Every reserved record type number is documented, including those held for split and merge
- [ ] The version bump and migration procedure is written down before it is needed
