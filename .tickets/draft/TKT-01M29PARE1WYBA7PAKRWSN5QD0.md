---
schema: 3
id: TKT-01M29PARE1WYBA7PAKRWSN5QD0
title: Define what a reader does with an unknown record type
type: task
status: draft
status_reason: null
priority: high
due_on: null
labels:
  - area:storage
  - effort:S
  - decision
assignees: []
milestone: phase-0
parent: TKT-01M29BKBKP8C66GXXQK4HPEZ35
origin: null
dependencies: []
blocks_on: none
references:
  - ref: design:storage-format
    path: docs/03-thread-storage.md
claim: null
archive: null
created_at: 2026-09-12T02:14:50Z
updated_at: 2026-09-12T02:14:50Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:terva/mieli
  name: Mieli
extensions: {}
---

## Description

Found while writing the agent-native positions in TKT-01M29GGG3W. The format is explicit about forward compatibility everywhere except here.

Unknown frame flag bits round-trip untouched, guaranteed by `frame.go:65` and enforced by `TestFrameUnknownFlagBitsRoundTrip`. Unknown reserved header bytes are not an error, guaranteed by `header.go:82`. But nothing in docs/03 or in the code says what a reader does with a **record type** it does not recognise, and docs/10 item 4 already plans to add one: the hard-redaction lean is "rewrite with a new type, keep CRCs valid".

So an older binary will meet a record type from a newer one, and the behaviour is undefined today.

The catalogue assigns 15 of 255 values in deliberate bands: 1-5 post lifecycle, 10-16 thread events, 20-21 split and merge, 30 archive. The banding is visible but docs/03 never states it as a rule, and there is no registry and no reserved range.

This has to be settled before TKT-01M29C5Y1E freezes the format, because "skip unknown types" and "refuse the log" are not interchangeable once data exists. Note that skipping is mechanically easy here, since the frame length lets a reader step over a record it cannot interpret without understanding it. The hard part is deciding when skipping is safe: a skipped `Delete` would render a post that a moderator removed.

## Acceptance criteria

- [ ] docs/03 states what a reader does with an unrecognised record type, and the choice is justified rather than asserted
- [ ] The rule distinguishes a type that can be skipped safely from one that cannot, or states that no such distinction is possible and why
- [ ] docs/03 states how a new record type is claimed: whether the bands are a rule, and whether any range is reserved for future use
- [ ] internal/store/threadlog or its caller implements the rule, with a test that feeds a reader an unknown type
- [ ] TKT-01M29C5Y1E does not close until this is decided
