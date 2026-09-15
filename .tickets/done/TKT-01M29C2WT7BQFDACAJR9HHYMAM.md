---
schema: 3
id: TKT-01M29C2WT7BQFDACAJR9HHYMAM
title: Write property tests for index and replay equivalence
type: task
status: done
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
updated_at: 2026-09-15T06:50:13Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/storage
  name: ""
extensions: {}
---

## Description

Generate random valid record sequences, including edits, deletes, restores, and thread events in any order, then assert that rebuilding the index by full replay equals the index maintained incrementally by the actor.

This is the property the whole design rests on. If the two ever disagree, then either the index lies or reindexing loses data, and both failures are silent in production until somebody notices a post that will not load.

## Acceptance criteria

- [x] Random sequences of every record type produce byte-identical indexes from replay and from incremental maintenance
- [x] Sequences include edits and deletes of already-edited posts, and out-of-order ordinal references
- [x] The generator's seed is reported on failure so a case is reproducible
- [x] The suite runs in CI within a bounded time and a longer run is available behind a flag

## Summary

`internal/store/threadlog/property_test.go`. Random sequences of every record
type — posts, edits, deletes, restores and all nine thread events — appended
through the actor, then the log replayed from scratch with a nil index, and
the two compared byte for byte. A third comparison checks the sidecar the
actor wrote against its own in-memory index, since that file is what an
operator's next start actually reads.

Ordinals for edits, deletes and restores are drawn uniformly from every post
that exists rather than from the tail, so a post being edited three times, or
edited long after later posts arrived, falls out of the generator rather than
having to be written by hand.

Bounded by default at 300 sequences, which runs inside the ordinary -race step
in about half a second. CI also runs a wider sweep of 5000 sequences up to 120
records, about four seconds. A failure prints the generating sequence, the
first differing entry field by field, and the exact flags to reproduce it.

Validated by mutation rather than assumed. Making the actor apply a record one
byte off its true offset is caught on the first sequence, with a field-level
diff; making it write a sidecar entry one slot along is caught by the on-disk
comparison.

The limitation is recorded in the test and is worth repeating: both paths call
the same Apply, so a bug inside Apply changes both identically and this suite
stays green. Mutating Apply and watching it pass is how that was confirmed.
This is a test of the two paths agreeing, not of either being right — Apply's
own correctness rests on the unit tests beside it and on the recovery suites.
