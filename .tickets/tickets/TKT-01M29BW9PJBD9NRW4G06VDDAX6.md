---
schema: 3
id: TKT-01M29BW9PJBD9NRW4G06VDDAX6
title: Implement thread ids and the sharded thread directory layout
type: task
status: ready
status_reason: null
priority: urgent
due_on: null
labels:
  - area:storage
  - effort:S
assignees: []
milestone: phase-0
parent: TKT-01M29BKBKP8C66GXXQK4HPEZ35
origin: null
dependencies: []
blocks_on: none
references:
  - ref: design:storage-layout
    path: docs/03-thread-storage.md
claim: null
archive: null
created_at: 2026-09-11T23:12:10Z
updated_at: 2026-09-11T23:57:53Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:terva/mieli
  name: Mieli
extensions: {}
---

## Description

64-bit time-sortable thread ids: a millisecond timestamp in the high bits and a per-process sequence in the low bits. Path sharding on the first two bytes of the hex form so no directory exceeds a few thousand entries.

Also the base32 short form used in URLs, which is what `/t/h6ute4a1000001/why-we-left-slack` carries. Ids are time-sortable so that a listing of the threads directory is chronological without reading a single file.

## Acceptance criteria

- [ ] Ids generated in the same millisecond are distinct and monotonic within a process
- [ ] The base32 form round-trips to the same 64-bit id, and an invalid form is rejected rather than silently coerced
- [ ] Path for an id is derived by pure function, with no directory scan
