---
schema: 3
id: TKT-01M29C800X3VVRARXAMGT8T6RK
title: Set the CSRF posture and write rate limits
type: task
status: ready
status_reason: null
priority: high
due_on: null
labels:
  - area:identity
  - area:api
  - effort:S
assignees: []
milestone: phase-1
parent: TKT-01M29BKBKZW26HNHFM1YFFGVZC
origin: null
dependencies:
  - TKT-01M29C5Y3SAD25GYYEMHYPDF24
blocks_on: none
references:
  - ref: design:identity-csrf
    path: docs/05-identity-and-accounts.md
claim: null
archive: null
created_at: 2026-09-11T23:18:33Z
updated_at: 2026-09-16T20:33:56Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/web
  name: ""
extensions: {}
---

## Description

No token plumbing in the SPA. The API is JSON-only and rejects non-JSON content types on mutating routes, cookies are SameSite=Lax, and Go's `CrossOriginProtection` from 1.25 is enabled for the form posts the non-JS client will make.

Enabling `CrossOriginProtection` now, before any form post exists, is one of the seven constraints in docs/07 that keep the second client cheap. It costs nothing today and it is awkward to retrofit once handlers exist.

Rate limits belong here too: per IP and per identity on `/auth/*` and on application submission, and per session and per IP on writes.

## Acceptance criteria

- [ ] A mutating API request with a non-JSON content type is refused
- [ ] CrossOriginProtection is enabled and a cross-origin form post is refused, tested
- [ ] Auth and write rate limits are enforced per IP and per session, and a limited response says when to retry
- [ ] No CSRF token is threaded through the SPA
