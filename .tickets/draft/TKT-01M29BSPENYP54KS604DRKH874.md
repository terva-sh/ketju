---
schema: 3
id: TKT-01M29BSPENYP54KS604DRKH874
title: Decide the handle change policy
type: spike
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:identity
  - effort:S
  - decision
assignees: []
milestone: phase-2
parent: TKT-01M29BKBKZW26HNHFM1YFFGVZC
origin: null
dependencies: []
blocks_on: none
references:
  - ref: design:open-questions-identity
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

Question 9 in docs/10. Never, once, or freely with redirects.

The lean is allowed with a 30-day cooldown and old handles reserved for 90 days so an abandoned handle cannot be grabbed to impersonate. Mentions are stored by user id, so history stays correct across a change either way, which is what makes this a policy question rather than a data question.

Decide before the user settings screens are built, because "change handle" is either on that screen or it is not.

## Acceptance criteria

- [ ] The policy is recorded in docs/05, including the cooldown and reservation periods if any
- [ ] The decision names what happens to a URL of the form /u/{old-handle} after a change
