---
schema: 3
id: TKT-01M29CCC0X7SY4SHJ46FV40GNW
title: Implement the settings endpoints and session management
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:api
  - effort:M
assignees: []
milestone: phase-3
parent: TKT-01M29BKBMCNDSYHYF9WXAPDNRN
origin: null
dependencies:
  - TKT-01M29C801M9WBEAVCCC6AXNZ4Z
  - TKT-01M29C5Y3E5SKHFR2SHJ8Y60TP
blocks_on: none
references:
  - ref: design:identity-sessions
    path: docs/05-identity-and-accounts.md
claim: null
archive: null
created_at: 2026-09-11T23:20:57Z
updated_at: 2026-10-01T06:45:52Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase3-run-orchestrator
  name: ""
extensions: {}
---

## Description

`GET /settings` and `PATCH /settings` for preferences, plus `GET/DELETE /settings/sessions` and `GET/DELETE /settings/identities/{id}`.

The session list is the user-facing half of server-side sessions: see where you are signed in and sign out one device or all of them. Identity management is where a user links a second provider or removes one.

Removing the last identity from an account has to be refused, or the user locks themselves out of an account that still owns posts.

## Acceptance criteria

- [ ] A user sees their sessions with device, IP, and last use, and can revoke one or all
- [ ] Revoking the current session signs the user out immediately
- [ ] Removing the last remaining identity is refused with a clear reason
- [ ] Preference changes take effect without a re-login
