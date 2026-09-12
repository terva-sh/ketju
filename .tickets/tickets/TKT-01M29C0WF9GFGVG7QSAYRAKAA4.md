---
schema: 3
id: TKT-01M29C0WF9GFGVG7QSAYRAKAA4
title: Build the in-process event bus
type: task
status: ready
status_reason: null
priority: urgent
due_on: null
labels:
  - area:db
  - effort:M
assignees: []
milestone: phase-1
parent: TKT-01M29BKBKV6HH8W0DN6QXQNZ0R
origin: null
dependencies: []
blocks_on: none
references:
  - ref: design:background-work
    path: docs/02-architecture.md
claim: null
archive: null
created_at: 2026-09-11T23:14:40Z
updated_at: 2026-09-11T23:57:53Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:terva/mieli
  name: Mieli
extensions: {}
---

## Description

`internal/events`: publish `{thread_id, ordinal, new_log_length}` after a durable append, and fan it out to the projection writer, the search indexer, the notification generator, and the SSE fan-out.

No external queue, by design. The whole system is goroutines in one process, which is what keeps the deployment to a binary and a directory.

The delivery contract needs stating here because four subscribers depend on it: ordered per thread, at-least-once, and never blocking the append path. A slow subscriber must fall behind rather than stall a writer, and falling behind must be visible as projection lag rather than silent.

## Acceptance criteria

- [ ] Events for one thread are delivered to each subscriber in log order
- [ ] A slow or failing subscriber never blocks an append, proven by a test with a deliberately stalled subscriber
- [ ] Subscriber lag is observable, and a subscriber that falls behind is logged rather than dropped silently
- [ ] Subscribers are registered at startup and the set is visible in one place
