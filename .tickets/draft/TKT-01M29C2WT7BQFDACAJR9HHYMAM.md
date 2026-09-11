---
schema: 3
id: TKT-01M29C2WT7BQFDACAJR9HHYMAM
title: Write property tests for index and replay equivalence
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
  - TKT-01M29C0WD88M1C8EW4QQTX76NJ
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

Generate random valid record sequences, including edits, deletes, restores, and thread events in any order, then assert that rebuilding the index by full replay equals the index maintained incrementally by the actor.

This is the property the whole design rests on. If the two ever disagree, then either the index lies or reindexing loses data, and both failures are silent in production until somebody notices a post that will not load.

## Acceptance criteria

- [ ] Random sequences of every record type produce byte-identical indexes from replay and from incremental maintenance
- [ ] Sequences include edits and deletes of already-edited posts, and out-of-order ordinal references
- [ ] The generator's seed is reported on failure so a case is reproducible
- [ ] The suite runs in CI within a bounded time and a longer run is available behind a flag
