---
schema: 3
id: TKT-01M29C5Y3SAD25GYYEMHYPDF24
title: Build the HTTP server, router, and middleware chain
type: task
status: draft
status_reason: null
priority: urgent
due_on: null
labels:
  - area:api
  - effort:M
assignees: []
milestone: phase-1
parent: TKT-01M29BKBMCNDSYHYF9WXAPDNRN
origin: null
dependencies:
  - TKT-01M29BW9QFZ2C7Q24KN1JW5MFW
  - TKT-01M29BW9QYZCT9JNZ00WNXFKR8
blocks_on: none
references:
  - ref: design:architecture-deployment
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

`net/http` with the standard library router, and the middleware chain from docs/02: session resolution, rate limiting, CSRF posture, request logging, panic recovery, and body limits.

The route table is where the URL scheme in docs/07 becomes real, and it covers more than `/api`: `/auth/*`, `/blobs/*`, `/events`, `/assets/*`, and the SPA shell catch-all. Getting the catch-all right matters, because it must serve the shell for an unknown application path and a 404 for an unknown API path.

Order in the chain is not cosmetic. Rate limiting before session lookup means an unauthenticated flood does not cost a database query each.

## Acceptance criteria

- [ ] Every URL prefix in docs/02 routes to its handler group, with a test per prefix
- [ ] An unknown path under /api returns a problem response and an unknown application path returns the SPA shell
- [ ] A panic in a handler returns 500 and is logged without taking down the server
- [ ] Middleware order is documented next to the chain with the reason for each position
