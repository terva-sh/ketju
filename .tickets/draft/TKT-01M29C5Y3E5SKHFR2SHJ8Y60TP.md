---
schema: 3
id: TKT-01M29C5Y3E5SKHFR2SHJ8Y60TP
title: Build the server-side session store
type: task
status: draft
status_reason: null
priority: urgent
due_on: null
labels:
  - area:identity
  - effort:M
assignees: []
milestone: phase-1
parent: TKT-01M29BKBKZW26HNHFM1YFFGVZC
origin: null
dependencies:
  - TKT-01M29BSPEFVY4JEZK2MXSTXXB0
  - TKT-01M29BYYH73ZBC9WTS7HN03PKJ
blocks_on: none
references:
  - ref: design:identity-sessions
    path: docs/05-identity-and-accounts.md
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

Sessions in the database rather than in a signed cookie, so that suspension and sign-out-everywhere take effect on the next request rather than at the next expiry.

The cookie is `__Host-ketju_session`, HttpOnly, Secure, SameSite=Lax, carrying a random 256-bit id whose hash is what the server stores. Idle expiry 30 days, hard expiry 90, and `last_used_at` slides at most once every five minutes so an active reader does not generate a write per request.

The `kind` column distinguishes a user session from the pre-user session that approval mode needs, and middleware has to treat the second as authenticated-but-not-a-member rather than as signed in.

## Acceptance criteria

- [ ] Session ids are stored hashed and a database dump contains nothing usable as a cookie value
- [ ] Idle and hard expiry are both enforced, tested at their boundaries
- [ ] last_used_at writes are rate limited to once per five minutes per session
- [ ] Deleting a user's session rows signs them out on the next request, tested end to end
- [ ] A pre-user session never satisfies a check for a signed-in member
