---
schema: 3
id: TKT-01M39973KGR32SBX6WT8G00QK9
title: Reconcile docs/03 and the reader on reserved record types
type: chore
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area:storage
assignees: []
milestone: phase-2
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-24T08:41:20Z
updated_at: 2026-09-29T04:50:59Z
created_by:
  id: agent:claude-code/phase1-run-415af6aa
  name: ""
updated_by:
  id: agent:claude-code/phase1-run-438e26c1
  name: ""
extensions: {}
---

## Description

docs/03 "Reserved record type numbers" says that a reader meeting `PostsSplitOut` (20) or `PostsMergedIn` (21) "refuses rather than skipping it". The code does the opposite, and does it deliberately:

- `DecodeBody` returns `ErrUnknownRecordType` for a reserved type, and `SkipUnknown` returns true for it, so the index fold (`threadlog.Index.Apply`) steps over it.
- `TestEveryCatalogueTypeIsAdvisory` gives the reason: "skipping a split or merge leaves a stale render rather than a disclosure, so it stays advisory".
- `TestReaderSkipsAdvisoryAndRefusesCritical` feeds a reserved type in and expects it skipped.

The projection writer (TKT-01M29C2WW5) follows the code, because a projection that refused where the index skips would leave two folds of one log disagreeing.

The document and the code need to agree. Deciding which one is right is a format question, so it was not settled inside the run. If the code is right, edit docs/03 line 102. If the doc is right, the change touches the frozen v1 reader behaviour, and docs/03's version procedure applies.

## Notes

**agent:claude-code/phase1-run-438e26c1** at 2026-09-29T04:50:59Z

Decided 2026-09-28 in Phase 2 grooming, on the recommendation the maintainer accepted: the code is right. A reserved split/merge type is advisory, because skipping it leaves a stale render, not a disclosure; that is the advisory class's test. The index fold and the projection writer agree only because both skip it. Making it critical would change the frozen v1 reader behaviour, and would refuse whole threads for a record type that never discloses anything. docs/03 'Reserved record type numbers' is corrected. The same edit fixes its list of unassigned numbers, which still called the whole critical range free after 0x80 was claimed for Redacted (PR #133).

## Summary

docs/03 now agrees with the code: the reserved types 20 and 21 are advisory and skipped. It also records that 0x80 is claimed. No code change.
