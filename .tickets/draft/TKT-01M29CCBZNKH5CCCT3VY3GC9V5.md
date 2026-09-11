---
schema: 3
id: TKT-01M29CCBZNKH5CCCT3VY3GC9V5
title: Add the single-process lock file
type: task
status: draft
status_reason: null
priority: high
due_on: null
labels:
  - area:ops
  - effort:S
assignees: []
milestone: phase-1
parent: TKT-01M29BN36Q9D32WQGYPFS336J0
origin: null
dependencies:
  - TKT-01M29BW9PB11V2AHE52C0F49JG
blocks_on: none
references:
  - ref: design:open-questions-ops
    path: docs/10-open-questions.md
claim: null
archive: null
created_at: 2026-09-11T23:20:57Z
updated_at: 2026-09-11T23:20:57Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:terva/mieli
  name: Mieli
extensions: {}
---

## Description

A lock file in the data directory that refuses a second `serve` against the same data.

The entire storage design assumes one writer per log file. Two processes sharing a data directory would corrupt threads quietly, and the operator who does it by accident during a botched deploy deserves a refusal rather than a rescue. A stale lock from a killed process has to be detectable, or the refusal becomes the outage.

## Acceptance criteria

- [ ] A second serve against the same data directory exits non-zero naming the holding process
- [ ] A lock left by a killed process is detected as stale and does not require manual deletion
- [ ] The lock is released on graceful shutdown
- [ ] The refusal message tells the operator what to check rather than only that it failed
