---
schema: 3
id: TKT-01M29BW9PJBD9NRW4G06VDDAX6
title: Implement thread ids and the sharded thread directory layout
type: task
status: done
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
updated_at: 2026-09-15T06:36:40Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/storage
  name: ""
extensions: {}
---

## Description

64-bit time-sortable thread ids: a millisecond timestamp in the high bits and a per-process sequence in the low bits. Path sharding on the first two bytes of the hex form so no directory exceeds a few thousand entries.

Also the base32 short form used in URLs, which is what `/t/h6ute4a1000001/why-we-left-slack` carries. Ids are time-sortable so that a listing of the threads directory is chronological without reading a single file.

## Acceptance criteria

- [x] Ids generated in the same millisecond are distinct and monotonic within a process
- [x] The base32 form round-trips to the same 64-bit id, and an invalid form is rejected rather than silently coerced
- [x] Path for an id is derived by pure function, with no directory scan

## Implementation plan

New package `internal/store/threadid`, a sibling of `threadlog`, holding the
id type and the path derivation together because the shard is a pure function
of the id and splitting them would invite a directory scan.

Bit layout: 48-bit millisecond timestamp in the high bits, 16-bit per-process
sequence in the low bits. 48 bits of milliseconds runs to the year 10889; 16
bits is 65536 ids per millisecond, which is 65M/s and unreachable here. Time
in the high bits is what makes a directory listing chronological.

Monotonicity is defended rather than assumed:

- Same millisecond: increment the sequence.
- Sequence exhausted: spin to the next millisecond and reset. Under 65536
  ids in one millisecond this never runs.
- Clock steps backwards: hold the previous timestamp rather than emit an id
  that sorts before one already handed out. The id stays monotonic and the
  timestamp is briefly stale, which is the right trade for an identifier.

Base32: Crockford, 13 characters, lowercase canonical. The doc's example
`h6ute4a1000001` is 14 characters, which cannot be a 64-bit value at 5 bits
per character, and does not decode to the tree's `3f7a9c21e0000001` under any
standard alphabet. Treating it as illustrative and choosing Crockford, which
drops I, L, O and U so a person can read an id off a screen and type it back.
Decoding accepts uppercase and maps I/L to 1 and O to 0; everything else is
refused rather than coerced, including the wrong length and a leading
character that would overflow 64 bits.

`New` returns an error only for a timestamp beyond 48 bits. It cannot fire
before the year 10889, but masking would wrap and silently break the ordering
the type exists to provide.

Paths: `<root>/threads/<b0>/<b1>/<hex>.log` and `.idx`, sharding on the first
two bytes of the 16-character hex form, derived by pure function.

## Summary

New package `internal/store/threadid`: 26 tests, green under -race.

Layout: 48-bit millisecond in the high bits, 16-bit sequence in the low bits.
Monotonicity is defended on three paths, each with its own test — same
millisecond increments the sequence, an exhausted sequence borrows the next
millisecond, and a clock that steps backwards holds the last one rather than
emit an id that sorts before one already handed out.

`advance` does not wait for the clock. The first version spun until the clock
passed, which hung the test suite outright against a frozen clock, and would
have held the generator's mutex for the rest of the millisecond in production.
Borrowing the next millisecond is wait-free and the backwards-clock path
already absorbs the consequence.

Base32 is Crockford, 13 characters, lowercase canonical, accepting uppercase
and mapping I/L to 1 and O to 0. Everything else is refused rather than
coerced, including a leading character above 15, which would overflow 64 bits
and silently alias another thread. A fuzz target checks the round trip, not
only the absence of a panic, and runs in CI for 20s.

The doc's URL example `h6ute4a1000001` was not implementable: 14 characters
cannot carry a 64-bit value at 5 bits each, and it does not decode to the
tree's `3f7a9c21e0000001` under any standard alphabet. Treated as
illustrative; docs/03 updated to a real encoding of the id it sits beside.

Paths shard on the first two bytes of the hex form, derived purely — proven by
deriving under a root that does not exist and asserting nothing was created.
The shard rate is measured rather than asserted: the second byte turns over
every 2^32 ms, about 50 days, and the first every 2^40 ms, about 35 years. The
comment originally claimed 111 days and 78 years, and the test that caught it
was itself wrong first, measuring the distance from an arbitrary instant to
the next boundary instead of the period between two.
