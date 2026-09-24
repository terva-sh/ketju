---
schema: 3
id: TKT-01M29BW9PB11V2AHE52C0F49JG
title: Confirm the TLS, backup, and single-process posture
type: spike
status: done
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
updated_at: 2026-09-24T05:05:12Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude/phase1-groom
  name: ""
extensions: {}
---

## Description

Questions 22, 23, and 24 in docs/10. The three operator-facing commitments that shape the deployment story.

TLS: a reverse proxy is assumed, with built-in ACME as a later phase. Confirm that the "just run it" case is allowed to require Caddy in front for v1.

Backup: document `rsync` of the data directory plus `VACUUM INTO`, or ship a `ketju backup` command that pauses thread actors, checkpoints the WAL, and snapshots consistently. The lean is to ship the command in Phase 4, with the documented procedure carrying earlier phases.

Single process: a lock file in the data directory refuses a second `serve`. Horizontal scaling is out of scope by design. Confirm that is acceptable for the target communities before it becomes a hard assumption everywhere.

## Acceptance criteria

- [x] All three are recorded in docs/02 with their reasoning
- [x] If single-process stays, the assumption is stated in the README rather than only in a design document

## Notes

**agent:claude/phase1-groom** at 2026-09-24T05:05:12Z

The lock file itself is TKT-01M29CCBZN. The backup command is TKT-01M29CXK35 in Phase 4.

## Summary

Decided with the user on 2026-09-24, on the documented leans. In v1 a reverse proxy terminates TLS, and built-in ACME comes later. ketju backup ships in Phase 4; until then the written procedure is rsync plus VACUUM INTO. There is one process per data directory, enforced by a lock file. Recorded in docs/02 (Shape of the deployment), with docs/10 Q22 to Q24 marked decided. The single-process assumption is stated in README.md.
