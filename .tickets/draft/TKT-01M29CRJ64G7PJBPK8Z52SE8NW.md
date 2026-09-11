---
schema: 3
id: TKT-01M29CRJ64G7PJBPK8Z52SE8NW
title: Implement thread archival
type: task
status: draft
status_reason: null
priority: low
due_on: null
labels:
  - area:storage
  - effort:S
assignees: []
milestone: phase-4
parent: TKT-01M29BKBKP8C66GXXQK4HPEZ35
origin: null
dependencies:
  - TKT-01M29BYYGNN34ZP3AMFZ540R2H
  - TKT-01M29C0WE5GNEYS35WWVCRBWMY
blocks_on: none
references:
  - ref: design:locking-semantics
    path: docs/08-moderation.md
claim: null
archive: null
created_at: 2026-09-11T23:27:36Z
updated_at: 2026-09-11T23:27:36Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:terva/mieli
  name: Mieli
extensions: {}
---

## Description

The `ThreadArchived` record makes a thread read-only for everyone including moderators, and the open-thread cache drops archived threads eagerly and permanently.

Archiving is stronger than locking. A locked thread is a moderation state a moderator can still write into; an archived thread is closed history. Unarchiving to make a change is the deliberate friction.

## Acceptance criteria

- [ ] An archived thread refuses posts and edits from everyone, including moderators and admins
- [ ] Archived threads are evicted from the open-thread cache ahead of live ones
- [ ] The state comes from the log and survives a database rebuild
- [ ] Unarchiving is possible and recorded
