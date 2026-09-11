---
schema: 3
id: TKT-01M29C8018NBCR238GXMMBH73J
title: Map errors to RFC 9457 problem details
type: task
status: draft
status_reason: null
priority: high
due_on: null
labels:
  - area:api
  - effort:S
assignees: []
milestone: phase-1
parent: TKT-01M29BKBMCNDSYHYF9WXAPDNRN
origin: null
dependencies:
  - TKT-01M29C5Y3SAD25GYYEMHYPDF24
blocks_on: none
references:
  - ref: design:api-principles
    path: docs/07-api-and-clients.md
claim: null
archive: null
created_at: 2026-09-11T23:18:33Z
updated_at: 2026-09-11T23:18:33Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:terva/mieli
  name: Mieli
extensions: {}
---

## Description

One error mapping for the whole API: `application/problem+json` with a stable `type` code the client can switch on, plus a human-readable title and detail.

Stability of the code is the part that matters. The client branches on it, the future HTML client turns it into a flash message, and changing one is a breaking change. A catalogue of codes in one file, rather than strings scattered through handlers, is what keeps that true.

Internal detail must not leak. A database constraint name in a problem detail tells an attacker the schema and tells a member nothing.

## Acceptance criteria

- [ ] Every error response carries application/problem+json with a type from a single catalogue
- [ ] A domain error maps to its status code in one place rather than at each call site
- [ ] No internal error text, SQL, or stack detail reaches a response body
- [ ] The code catalogue is documented and a golden test covers each one
