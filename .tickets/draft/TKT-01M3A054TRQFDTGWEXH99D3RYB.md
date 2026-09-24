---
schema: 3
id: TKT-01M3A054TRQFDTGWEXH99D3RYB
title: Report projection drift and quarantined threads in ketju fsck
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels: []
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-24T15:22:13Z
updated_at: 2026-09-24T15:22:13Z
created_by:
  id: agent:claude-code/phase1-run-415af6aa
  name: ""
updated_by:
  id: agent:claude-code/phase1-run-415af6aa
  name: ""
extensions: {}
---

## Description

`ketju fsck` has a `DriftBytes` placeholder and prints "projection drift not checked: there is no database yet". The database exists now, and startup catch-up (TKT-01M29C5Y1S) classifies each thread the same way fsck would need to:
- a log longer than its projection is drift;
- a log shorter than its projection, or a projection with no log, is corruption.

Grooming for C5Y1S settled that a corrupt thread is "reported by /healthz and `ketju fsck`" (docs/03 "Durability and recovery"). Healthz is covered by TKT-01M29CCBZ8. Nothing covers fsck.

### Approach
Open the database read-only when it exists. Reuse `projection.Writer`'s walk and classification, or factor them out of `catchup.go` into something fsck can call without a Writer. Fill `DriftBytes`, set `DriftChecked`, exit 3 on drift, and report a corrupt thread by name as damage.

### Open items
fsck's help says it is safe against a live directory. Reading `projection_state` while serve writes it is fine under WAL, but the sizes and the rows are not read at one instant, so fsck may see drift that catch-up would never see. Say so in the output rather than lock.
