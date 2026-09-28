---
schema: 3
id: TKT-01M3N24RHDFMR8MHX0CCENN6R7
title: Make archived forums read-only in permission evaluation
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:db
  - effort:S
assignees: []
milestone: phase-2
parent: TKT-01M29BN36JH4M1W5819D06PGPP
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-28T22:28:37Z
updated_at: 2026-09-28T22:28:37Z
created_by:
  id: agent:claude-code/phase1-run-438e26c1
  name: ""
updated_by:
  id: agent:claude-code/phase1-run-438e26c1
  name: ""
extensions: {}
---

## Description

An archived forum is read-only (docs/04 "permissions", decided 2026-09-28 in TKT-01M39788AT, Decide what an archived forum does to permissions). `read` is unchanged. `create_thread`, `reply` and `upload` are denied to everyone except admins, and that includes moderators.

`internal/perm` reads the forum row already, so the evaluation change is small. `POST /forums/{slug}/threads` answers `conflict` for an archived forum to everyone today (docs/07). It must let an admin through, and must keep answering `conflict` for everyone else, so that the refusal still says why.

## Acceptance criteria

- [ ] In an archived forum, create_thread, reply and upload are denied to every role except admin, and read is unchanged
- [ ] Creating a thread or replying in an archived forum answers conflict to a non-admin and succeeds for an admin
- [ ] docs/04 and docs/07 describe the behaviour as built
