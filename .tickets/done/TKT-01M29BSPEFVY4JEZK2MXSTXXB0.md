---
schema: 3
id: TKT-01M29BSPEFVY4JEZK2MXSTXXB0
title: Decide session implementation and pre-login state storage
type: spike
status: done
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
updated_at: 2026-09-24T05:05:03Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude/phase1-groom
  name: ""
extensions: {}
---

## Description

Questions 7 and 8 in docs/10. They are one ticket because they are one design: where session-adjacent state lives.

Sessions: `alexedwards/scs/v2` for its middleware ergonomics, or roughly 200 lines hand-rolled. The lean is hand-rolled, for exact control over id hashing, immediate revocation, and the pre-user session variant that approval mode needs. A library that does not model a session without a user is a library we would fight.

Pre-login OIDC state, nonce, and PKCE verifier: an encrypted short-lived cookie, or an anonymous server-side session row. The lean is the cookie, because an abandoned login should not cost a database write.

## Acceptance criteria

- [x] Both decisions are recorded in docs/05 with the reasoning
- [x] If scs wins, confirm in writing how it models the pre-user session before the approval epic depends on it

## Notes

**agent:claude/phase1-groom** at 2026-09-24T05:05:03Z

The user took this decision directly during Phase 1 grooming (the conversation of 2026-09-23 and 2026-09-24), choosing both leans. Criterion 2 is conditional ('if scs wins'). scs did not win, so there is nothing to confirm; it is ticked as satisfied by the condition failing, not by any work. Approval mode's pre-user session is one of the reasons the hand-rolled store won, and docs/05 says so.

## Summary

Decided with the user on 2026-09-24. Sessions use a hand-rolled store of about 200 lines, not scs/v2. Pre-login OIDC state lives in a short-lived cookie sealed with authenticated encryption, not in an anonymous server-side row. The reasoning is recorded in docs/05 (Login flow; Sessions and CSRF), and docs/10 Q7 and Q8 are marked decided. Both follow the documented lean. The session store TKT-01M29C5Y3E and the OIDC flow TKT-01M29C7ZZ9 now build on this.
