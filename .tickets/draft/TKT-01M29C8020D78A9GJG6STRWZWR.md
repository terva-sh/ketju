---
schema: 3
id: TKT-01M29C8020D78A9GJG6STRWZWR
title: Add security headers, CSP nonces, and request limits
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
  - ref: design:api-security
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

CSP with per-response nonces and no inline scripts, `img-src 'self' data:`, no third-party origins, `frame-ancestors 'none'`, `Referrer-Policy: strict-origin-when-cross-origin`, and `X-Content-Type-Options: nosniff`.

Request limits belong with them: 64 KB for a post body and a configurable attachment cap defaulting to 10 MB.

A strict CSP is easy to add on day one and painful to retrofit, because every inline handler written in the meantime has to be found. The SPA shell is a Go template that injects the nonce, so this ticket and the embedding ticket have to agree on that mechanism.

## Acceptance criteria

- [ ] Every response carries the documented headers, tested
- [ ] The CSP nonce is per response and the SPA shell template injects it
- [ ] No inline script or style is needed by the client, verified with the CSP in report-only off
- [ ] A post body over 64 KB and an upload over the cap are refused with a problem response rather than by a truncated read
