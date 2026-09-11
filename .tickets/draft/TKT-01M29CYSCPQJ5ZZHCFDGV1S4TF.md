---
schema: 3
id: TKT-01M29CYSCPQJ5ZZHCFDGV1S4TF
title: Implement thread tags
type: task
status: draft
status_reason: null
priority: low
due_on: null
labels:
  - area:moderation
  - effort:S
assignees: []
milestone: phase-4
parent: TKT-01M29BN36JH4M1W5819D06PGPP
origin: null
dependencies:
  - TKT-01M29CKRH92JGZ1WY58JXC4T2G
blocks_on: none
references:
  - ref: design:storage-records
    path: docs/03-thread-storage.md
claim: null
archive: null
created_at: 2026-09-11T23:31:00Z
updated_at: 2026-09-11T23:31:00Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:terva/mieli
  name: Mieli
extensions: {}
---

## Description

Tags on threads, set at creation and changed by moderators, shown as chips in thread lists and filterable.

The `ThreadTagged` record replaces the whole set rather than adding or removing one, which keeps the log record self-contained and makes a rebuild trivial. That shape is already in the format and this ticket honors it rather than working around it.

Whether members can tag their own threads is the open call here.

## Acceptance criteria

- [ ] Tags render as chips in thread lists and on the thread page, and filter a forum list
- [ ] A tag change writes a full replacement record
- [ ] The tag vocabulary is either free or constrained by a setting, decided and documented here
- [ ] Tag chips meet the enlarged hit area rule on phones without growing visually
