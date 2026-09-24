---
schema: 3
id: TKT-01M3AFHGJDQTMQ67CB8832F4AQ
title: Test the OIDC callback against a pending user
type: chore
status: draft
status_reason: null
priority: normal
due_on: null
labels: []
assignees: []
milestone: null
parent: TKT-01M29BKBKZW26HNHFM1YFFGVZC
origin: null
dependencies:
  - TKT-01M29C7ZZ9KFSNZ8WC8YP1HAYV
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-24T19:51:07Z
updated_at: 2026-09-24T19:51:19Z
created_by:
  id: agent:claude-code/phase1-run-415af6aa
  name: ""
updated_by:
  id: agent:claude-code/phase1-run-415af6aa
  name: ""
extensions: {}
---

## Description

TKT-01M29C7ZZR ("Route every session creation through one status-checking function") has AC3: "The OIDC callback path is tested explicitly against a pending, a suspended, and a deactivated user". The callback does not exist until TKT-01M29C7ZZ9, whose own AC4 tests suspended and deactivated users. A pending user is in neither.

docs/05 says approval mode never creates a pending user row, but the schema allows `status = 'pending'` (docs/04), and docs/09's Gitea row is exactly the case where a path the design says cannot happen does. So the callback should be tested against it too.

`session.IssueUser` already refuses a pending user with a `*NotActiveError{Status: "pending"}`, tested in internal/auth/session/issue_test.go. This ticket is about the callback's handling of that refusal.

## Acceptance criteria

- [ ] With the fake IdP, signing in with the identity of a user whose status is pending sets no session cookie, leaves the sessions row count unchanged, and shows the same unavailable page as suspended and deactivated
