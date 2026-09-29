---
schema: 3
id: TKT-01M3QH3X58PBVSYYJC49XSR65M
title: Answer GET /me for an agent authenticated by its credential
type: bug
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:moderation
  - effort:S
assignees: []
milestone: phase-2
parent: TKT-01M29BN36JH4M1W5819D06PGPP
origin: null
dependencies:
  - TKT-01M29P9RSW21SCES335WERTBSF
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-29T21:28:46Z
updated_at: 2026-09-29T21:28:49Z
created_by:
  id: agent:claude-code/phase2-run-p9rsw
  name: ""
updated_by:
  id: agent:claude-code/phase2-run-p9rsw
  name: ""
extensions: {}
---

## Description

`GET /api/v1/me` answers an agent authenticated by its bearer credential as anonymous: `user` is null and there are no unread counts. `users.Service.Me` builds the viewer's fields from `session.From(ctx)`, which a bearer request never has, because the credential is not a session (TKT-01M29P9RSW, Implement agent accounts, provisioning, and the agent credential).

Nothing is exposed and nothing is refused that should be allowed: every other route authorizes from `domain.Viewer`, which the bearer middleware sets. But an agent that calls `/me` to check who it is signed in as reads "nobody". docs/07 says an agent uses the same API as the web client.

A likely fix is for `Me` to read the user row by `v.UserID` when the request has no session but has a credential (`agentcred.From`). That adds one statement on the agent path only, so the session path's two-statement budget is unchanged. Settle whether an agent's `me` should also carry a hint that it is authenticated by a credential.

## Acceptance criteria

- [ ] GET /api/v1/me with a live bearer credential answers the agent as the user, with its unread counts
- [ ] The session path's statement budget test is unchanged
