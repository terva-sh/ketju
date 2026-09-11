---
schema: 3
id: TKT-01M29C5Y2SV692CHEM91Z4YYNS
title: Add the session sweeper and blob garbage collector
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:db
  - area:ops
  - effort:S
assignees: []
milestone: phase-1
parent: TKT-01M29BKBKV6HH8W0DN6QXQNZ0R
origin: null
dependencies:
  - TKT-01M29BYYH73ZBC9WTS7HN03PKJ
  - TKT-01M29BYYJ0WJ3C7RRZQMDRP5ZB
blocks_on: none
references:
  - ref: design:background-work
    path: docs/02-architecture.md
claim: null
archive: null
created_at: 2026-09-11T23:17:26Z
updated_at: 2026-09-11T23:17:26Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:terva/mieli
  name: Mieli
extensions: {}
---

## Description

Two of the background jobs from the docs/02 table: an hourly sweep deleting expired sessions, and a daily pass deleting blobs whose refcount is zero and which are older than seven days.

The seven-day delay is the safety margin. A blob dropping to zero refcount during an edit that is then undone must not have had its bytes deleted in the meantime.

## Acceptance criteria

- [ ] Expired sessions are deleted hourly and an in-flight request holding one is not affected mid-request
- [ ] A blob at refcount zero is deleted only after seven days, and a refcount returning above zero cancels that
- [ ] Both jobs log what they removed, with counts
- [ ] Both jobs survive a failure without stopping the scheduler
