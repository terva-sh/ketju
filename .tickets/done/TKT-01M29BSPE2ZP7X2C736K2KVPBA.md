---
schema: 3
id: TKT-01M29BSPE2ZP7X2C736K2KVPBA
title: Decide hard redaction semantics
type: spike
status: done
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
updated_at: 2026-09-28T22:01:03Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase1-run-438e26c1
  name: ""
extensions: {}
---

## Description

Question 4 in docs/10. Hard redaction is the one operation that is not append-only, so its shape needs settling before anyone writes it.

Two options. Zero the body in place, which breaks the CRC of that frame and makes the reader treat a CRC failure as a redaction marker. Or rewrite the log with a new `Redacted` record type, which keeps every CRC valid and keeps "a bad CRC means corruption" as an invariant worth trusting.

The lean is the rewrite. A format where a checksum failure is sometimes normal is a format where corruption hides.

Needed before Phase 2 because GDPR-style erasure requests are a legal deadline rather than a feature request.

## Acceptance criteria

- [x] The decision is recorded in docs/03 with the reasoning
- [x] If the rewrite option wins, the `Redacted` record type is added to the type table before the format freeze reserves its number

## Notes

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T21:32:45Z

Maintainer decision, 2026-09-28, in Phase 2 grooming (TKT-01M3M9RR26). The format: "Rewrite with a Redacted record". The scope: "Every revision + attachments". Written into docs/03 "Hard redaction" and docs/10 item 4. 0x80 is claimed for `Redacted` in the docs/03 type table, following the claiming procedure. The catalogue entry in internal/store/threadlog/records.go lands with the tool, TKT-01M29CXJZ9. Until then a build refuses a log containing it, as it would any unknown critical type.

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T22:01:03Z

Addition to the decision, from Terva review 1265 on PR #133: the redaction also follows split/merge 'origin' links in both directions and redacts every copy of the post in other threads' logs, rewriting each log it touches. Otherwise the text stays readable through the copy. docs/03 'Hard redaction' says so; TKT-01M29CXJZ9 must build it.

## Summary

Hard redaction rewrites the log offline, swaps it in by rename, and never edits in place. Every frame carrying the post's text (the Post and every Edit) keeps its header, gets an empty body, and is re-checksummed. Blobs only that post references are deleted. A critical Redacted record (0x80, claimed in docs/03) is appended, so older builds refuse rather than show removed content. Quotes elsewhere are listed for the operator. The work lands in TKT-01M29CXJZ9.
