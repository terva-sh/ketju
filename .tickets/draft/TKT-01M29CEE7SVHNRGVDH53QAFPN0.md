---
schema: 3
id: TKT-01M29CEE7SVHNRGVDH53QAFPN0
title: Add the metrics endpoint
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:ops
  - effort:M
assignees: []
milestone: phase-4
parent: TKT-01M29BN36Q9D32WQGYPFS336J0
origin: null
dependencies:
  - TKT-01M29CCBYX4V6ZP7N9HF8H8NCZ
blocks_on: none
references:
  - ref: design:observability
    path: docs/02-architecture.md
claim: null
archive: null
created_at: 2026-09-11T23:22:04Z
updated_at: 2026-09-11T23:22:04Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:terva/mieli
  name: Mieli
extensions: {}
---

## Description

`/metrics` in Prometheus text format behind an admin token or a separate bind address, carrying request latency, cache hit ratios, thread store fsync latency, and projection lag summed as log length minus projected length.

Projection lag is the metric that matters most and the one no generic middleware would give you. It is the number that turns "a post seems to be missing" into a graph.

## Acceptance criteria

- [ ] The endpoint is not reachable without an admin token or a separate bind address
- [ ] Projection lag, fsync latency, and cache hit ratios are all exported
- [ ] Metric names and labels follow Prometheus conventions and are documented
- [ ] Scraping does not take locks that affect the request path
