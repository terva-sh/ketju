---
schema: 3
id: TKT-01M29CP2KZPR0880MGFV3CPXR4
title: Implement application submit, status, and withdraw
type: task
status: draft
status_reason: null
priority: high
due_on: null
labels:
  - area:identity
  - area:api
  - effort:M
assignees: []
milestone: phase-2
parent: TKT-01M29BKBM38VTYY4Y91018GTH6
origin: null
dependencies:
  - TKT-01M29CKRG9XYJS4HM1VY1QSJ1V
  - TKT-01M29CKRDQHB528ZW1FFBN4SA1
blocks_on: none
references:
  - ref: design:approval-mode
    path: docs/05-identity-and-accounts.md
claim: null
archive: null
created_at: 2026-09-11T23:26:15Z
updated_at: 2026-09-11T23:26:15Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:terva/mieli
  name: Mieli
extensions: {}
---

## Description

`GET /apply/form`, `POST /apply`, `GET /apply/status`, and `POST /apply/withdraw`, all reachable from a pre-user session only.

Re-visiting the site with a pending identity lands on the status page. A pending applicant can withdraw and re-apply once per 24 hours, which is the anti-spam limit rather than a courtesy.

## Acceptance criteria

- [ ] Submitting stores the answers against the active form version and sets status pending
- [ ] Returning with a pending identity always lands on the status page
- [ ] Withdraw and re-apply is limited to once per 24 hours per identity
- [ ] A blocked identity is refused at the form rather than after submission
