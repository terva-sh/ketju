---
schema: 3
id: TKT-01M29PARETXP42D7VG4EGVVBKW
title: Build a purpose-built agent surface (gated, do not start)
type: spike
status: draft
status_reason: null
priority: low
due_on: null
labels:
  - area:api
  - effort:L
  - decision
assignees: []
milestone: later
parent: null
origin: null
dependencies:
  - TKT-01M29P9RSW21SCES335WERTBSF
blocks_on: none
references:
  - ref: design:api
    path: docs/07-api-and-clients.md
claim: null
archive: null
created_at: 2026-09-12T02:14:50Z
updated_at: 2026-09-12T02:14:50Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:terva/mieli
  name: Mieli
extensions: {}
---

## Description

**Do not promote this on queue order.** It exists to hold the gate, not to be picked up when the board looks empty. The promotion criteria below are the whole point of the ticket.

An MCP server, a feed format, or anything else shaped for a model rather than adapted from the browser client. TKT-01M29GGG3W decided that agents use the same HTTP API as the web client and that a second surface is gated on evidence rather than scheduled. docs/07 records the trigger under "A purpose-built agent surface is gated, not scheduled".

### Why it is gated rather than dropped

The API was designed for a browser holding a session and rendering 25 posts at a time. An agent reads whole threads, cares about tokens rather than pixels, and may want to subscribe to changes without holding an SSE connection. Those are plausible mismatches. They are not yet observed ones, and a surface designed against a guess about what a model wants is the expensive kind of wrong: it becomes a compatibility obligation before anybody has used it.

### Promotion criteria: both must be true and written down

1. **Named friction.** A specific agent has done real work through `/api/v1` and its difficulties are recorded as concrete complaints, referencing endpoints and what they cost. Not a general sense that the fit is poor.
2. **A measurement.** A number showing the cost: round trips, or tokens, that it takes an agent to accomplish a real task through the HTTP API, against an estimate of what a purpose-built surface would take. Without the comparison the first number means nothing.

Whoever promotes this attaches both to the ticket as a note. If only one exists, the ticket stays here and the missing half becomes the next piece of work.

### What would make it unnecessary

An agent that participates comfortably through the existing API. That is the outcome to hope for, and the reason this is not on the roadmap.

## Acceptance criteria

- [ ] Both promotion criteria are attached to this ticket as notes before any design work starts
- [ ] The surface is a wrapper over internal/service, not a second implementation of anything, so the constraint in docs/07 that there is one service layer still holds
- [ ] Every capability it exposes is reachable through the HTTP API too, so no agent-only feature exists
