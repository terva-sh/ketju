---
schema: 3
id: TKT-01M29CP2MGC6FD35PJHR2Y2WNA
title: Implement moderator application review
type: task
status: draft
status_reason: null
priority: high
due_on: null
labels:
  - area:identity
  - area:moderation
  - effort:L
assignees: []
milestone: phase-2
parent: TKT-01M29BKBM38VTYY4Y91018GTH6
origin: null
dependencies:
  - TKT-01M29CKRDQHB528ZW1FFBN4SA1
  - TKT-01M29C7ZZR3FJMZ7P7VA0CWNJ2
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

`/admin/applications` showing the answers, the identity's provider, email, and verified flag, prior applications from the same identity, and notes from other moderators. Actions are approve, deny with a note, and deny with "block this subject".

Approve creates the users row with status active, confirms or chooses the handle, links the identity, notifies the applicant, and writes audit_log. Deny with block keeps the identity with user_id NULL and a blocked marker so re-application is refused.

Approval is the only path from a pre-user session to a member session, and it goes through the single session-creation function like everything else.

## Acceptance criteria

- [ ] Approve creates the user, links the identity, and notifies the applicant in one transaction or none
- [ ] Deny with block refuses a later application from the same subject
- [ ] Every decision writes an audit_log row with the deciding moderator and the note
- [ ] Two moderators deciding the same application concurrently produce one decision and a clear message for the second
