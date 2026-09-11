---
schema: 3
id: TKT-01M29CP2N0KR7F7PFGCWHQTQCH
title: Notify moderators and applicants about applications
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:identity
  - effort:S
assignees: []
milestone: phase-2
parent: TKT-01M29BKBM38VTYY4Y91018GTH6
origin: null
dependencies:
  - TKT-01M29BYYHQ7PTYNYA3VBBD4PDB
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

The `new_application` notification to everyone in moderators and admins, and `application_decided` to the applicant.

In-app only. docs/10 keeps SMTP out of v1, which means an applicant learns the decision by returning to the status page, and the notification is what a moderator sees when they next open the site. That is a real limitation worth stating in the admin UI rather than discovering.

## Acceptance criteria

- [ ] Every moderator and admin receives new_application when one is submitted
- [ ] The applicant's decision notification is visible on the status page they already have access to
- [ ] Notification counts appear in the moderator navigation badge
- [ ] No email is sent or required
