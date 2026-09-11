---
schema: 3
id: TKT-01M29CEE52GKX043EGKG9DWKFE
title: Build the client router and route table
type: task
status: draft
status_reason: null
priority: high
due_on: null
labels:
  - area:client
  - effort:M
assignees: []
milestone: phase-1
parent: TKT-01M29BN367F3YQZESM63VN26GP
origin: null
dependencies:
  - TKT-01M29CCC02K8F14P0SKAYFMSE6
blocks_on: none
references:
  - ref: design:url-scheme
    path: docs/07-api-and-clients.md
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

`preact-iso` routing over exactly the URL scheme in docs/07, with the admin bundle lazy-loaded.

The routes are not an internal detail. They are the paths the future HTML client will serve, so a client-only route or a piece of state that lives outside the URL is a future porting cost. Lenses and page numbers are query parameters for the same reason.

A stale slug in a thread URL redirects to the current one rather than 404ing.

## Acceptance criteria

- [ ] Every path in the docs/07 URL scheme resolves to a screen, tested by a route table test
- [ ] Lens and page live in the URL and a shared link reproduces the exact view
- [ ] A stale slug redirects to the canonical URL preserving page and lens
- [ ] The admin bundle loads only on an admin route
