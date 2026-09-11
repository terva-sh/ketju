---
schema: 3
id: TKT-01M29BSPE2ZP7X2C736K2KVPBA
title: Decide hard redaction semantics
type: spike
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:storage
  - effort:S
  - decision
assignees: []
milestone: phase-2
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
updated_at: 2026-09-11T23:10:45Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:terva/mieli
  name: Mieli
extensions: {}
---

## Description

Question 4 in docs/10. Hard redaction is the one operation that is not append-only, so its shape needs settling before anyone writes it.

Two options. Zero the body in place, which breaks the CRC of that frame and makes the reader treat a CRC failure as a redaction marker. Or rewrite the log with a new `Redacted` record type, which keeps every CRC valid and keeps "a bad CRC means corruption" as an invariant worth trusting.

The lean is the rewrite. A format where a checksum failure is sometimes normal is a format where corruption hides.

Needed before Phase 2 because GDPR-style erasure requests are a legal deadline rather than a feature request.

## Acceptance criteria

- [ ] The decision is recorded in docs/03 with the reasoning
- [ ] If the rewrite option wins, the `Redacted` record type is added to the type table before the format freeze reserves its number
