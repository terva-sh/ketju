---
schema: 3
id: TKT-01M29BW9PB11V2AHE52C0F49JG
title: Confirm the TLS, backup, and single-process posture
type: spike
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:ops
  - effort:S
  - decision
assignees: []
milestone: phase-1
parent: TKT-01M29BN36Q9D32WQGYPFS336J0
origin: null
dependencies: []
blocks_on: none
references:
  - ref: design:open-questions-ops
    path: docs/10-open-questions.md
claim: null
archive: null
created_at: 2026-09-11T23:12:10Z
updated_at: 2026-09-11T23:12:10Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:terva/mieli
  name: Mieli
extensions: {}
---

## Description

Questions 22, 23, and 24 in docs/10. The three operator-facing commitments that shape the deployment story.

TLS: a reverse proxy is assumed, with built-in ACME as a later phase. Confirm that the "just run it" case is allowed to require Caddy in front for v1.

Backup: document `rsync` of the data directory plus `VACUUM INTO`, or ship a `neoforum backup` command that pauses thread actors, checkpoints the WAL, and snapshots consistently. The lean is to ship the command in Phase 4, with the documented procedure carrying earlier phases.

Single process: a lock file in the data directory refuses a second `serve`. Horizontal scaling is out of scope by design. Confirm that is acceptable for the target communities before it becomes a hard assumption everywhere.

## Acceptance criteria

- [ ] All three are recorded in docs/02 with their reasoning
- [ ] If single-process stays, the assumption is stated in the README rather than only in a design document
