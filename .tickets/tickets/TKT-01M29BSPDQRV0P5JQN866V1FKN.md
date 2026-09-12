---
schema: 3
id: TKT-01M29BSPDQRV0P5JQN866V1FKN
title: Settle the Phase 0 thread log format decisions
type: spike
status: ready
status_reason: null
priority: urgent
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
  - ref: design:open-questions-storage
    path: docs/10-open-questions.md
claim: null
archive: null
created_at: 2026-09-11T23:10:45Z
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

Questions 1, 2, and 3 in docs/10, all of which must be answered before the format is frozen and none of which can be answered from a document.

Body encoding: CBOR through fxamacker/cbor, or JSON. The lean is CBOR because bodies are written once and read many times. If measured decode cost is close, JSON's debuggability may win.

Index entry width: 40 bytes carrying author, created_at, and reply_to, or 16 bytes with the offset alone. The lean is 40. It costs 2 MB per 50k posts and buys the author, chain, and since lenses without a database query, which is most of the argument for the whole lens design.

Default fsync policy: `group` with a window of 5 ms or so, or `always`. The lean is `group`, with `always` and `never` remaining configurable.

## Acceptance criteria

- [ ] Decode cost of CBOR and JSON bodies is measured on realistic post sizes and the numbers are recorded on this ticket
- [ ] Append latency under `group` and `always` fsync is measured on the target storage and recorded
- [ ] All three decisions are written into docs/03 and docs/10 is updated to say which option won and why
