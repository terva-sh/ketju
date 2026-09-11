---
schema: 3
id: TKT-01M29BSPEFVY4JEZK2MXSTXXB0
title: Decide session implementation and pre-login state storage
type: spike
status: draft
status_reason: null
priority: high
due_on: null
labels:
  - area:identity
  - effort:S
  - decision
assignees: []
milestone: phase-1
parent: TKT-01M29BKBKZW26HNHFM1YFFGVZC
origin: null
dependencies: []
blocks_on: none
references:
  - ref: design:open-questions-db
    path: docs/10-open-questions.md
claim: null
archive: null
created_at: 2026-09-11T23:10:45Z
updated_at: 2026-09-11T23:10:45Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:terva/mieli
  name: Mieli
extensions: {}
---

## Description

Questions 7 and 8 in docs/10. They are one ticket because they are one design: where session-adjacent state lives.

Sessions: `alexedwards/scs/v2` for its middleware ergonomics, or roughly 200 lines hand-rolled. The lean is hand-rolled, for exact control over id hashing, immediate revocation, and the pre-user session variant that approval mode needs. A library that does not model a session without a user is a library we would fight.

Pre-login OIDC state, nonce, and PKCE verifier: an encrypted short-lived cookie, or an anonymous server-side session row. The lean is the cookie, because an abandoned login should not cost a database write.

## Acceptance criteria

- [ ] Both decisions are recorded in docs/05 with the reasoning
- [ ] If scs wins, confirm in writing how it models the pre-user session before the approval epic depends on it
