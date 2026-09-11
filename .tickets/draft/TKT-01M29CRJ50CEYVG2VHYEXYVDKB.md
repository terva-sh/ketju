---
schema: 3
id: TKT-01M29CRJ50CEYVG2VHYEXYVDKB
title: Implement per-thread slow mode
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:moderation
  - effort:S
assignees: []
milestone: phase-2
parent: TKT-01M29BN36JH4M1W5819D06PGPP
origin: null
dependencies:
  - TKT-01M29CA9B9CEVWZ6A3CH3DZRMF
blocks_on: none
references:
  - ref: design:moderation-primitives
    path: docs/08-moderation.md
claim: null
archive: null
created_at: 2026-09-11T23:27:36Z
updated_at: 2026-09-11T23:27:36Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:terva/mieli
  name: Mieli
extensions: {}
---

## Description

A minimum number of seconds between posts per user in a thread, set by a moderator. Stored in the database rather than the log, because it is a live moderation control rather than part of the thread's history.

Slow mode is the tool for a thread that is heating up, and it works by making people wait rather than by removing anything. Moderators are exempt.

## Acceptance criteria

- [ ] A post inside the interval is refused with a problem response naming the remaining wait
- [ ] Moderators are exempt
- [ ] The setting is per thread, survives a restart, and is visible to members in the composer
- [ ] Turning slow mode off takes effect immediately
