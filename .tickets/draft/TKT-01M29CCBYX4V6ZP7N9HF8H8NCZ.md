---
schema: 3
id: TKT-01M29CCBYX4V6ZP7N9HF8H8NCZ
title: Implement neoforum serve with graceful shutdown
type: task
status: draft
status_reason: null
priority: urgent
due_on: null
labels:
  - area:ops
  - effort:M
assignees: []
milestone: phase-1
parent: TKT-01M29BN36Q9D32WQGYPFS336J0
origin: null
dependencies:
  - TKT-01M29C5Y3SAD25GYYEMHYPDF24
  - TKT-01M29C0WE5GNEYS35WWVCRBWMY
  - TKT-01M29BW9Q17KEBMHJ91G6QHW36
blocks_on: none
references:
  - ref: design:background-work
    path: docs/02-architecture.md
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

The command that wires everything together and takes it down cleanly: stop accepting, drain in-flight requests for up to 10 seconds, flush every thread actor so its index header is current, then close the database.

Flush order is the part worth testing. A shutdown that closes the database before the actors flush leaves indexes claiming a shorter log than the file, which is harmless but means every thread replays on the next start. Doing it right is the difference between a clean restart and a slow one.

## Acceptance criteria

- [ ] SIGTERM drains in-flight requests within the timeout and then exits zero
- [ ] Every open thread's index header is flushed before exit, so a restart trusts every index with no replay
- [ ] Startup order brings up config, database, stores, event subscribers, then the listener, and a failure in any step exits non-zero with a clear message
- [ ] A shutdown that exceeds the drain timeout still flushes actors rather than aborting
