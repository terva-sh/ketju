---
schema: 3
id: TKT-01M29CA9BP9QTMN2JEDPM0YS67
title: Implement edit, soft delete, and restore
type: task
status: draft
status_reason: null
priority: high
due_on: null
labels:
  - area:api
  - effort:M
assignees: []
milestone: phase-1
parent: TKT-01M29BKBMCNDSYHYF9WXAPDNRN
origin: null
dependencies:
  - TKT-01M29C0WD88M1C8EW4QQTX76NJ
  - TKT-01M29C2WWGF4ERSR2AM2QNEH8C
blocks_on: none
references:
  - ref: design:storage-records
    path: docs/03-thread-storage.md
claim: null
archive: null
created_at: 2026-09-11T23:19:48Z
updated_at: 2026-09-11T23:19:48Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:terva/mieli
  name: Mieli
extensions: {}
---

## Description

`PATCH /threads/{id}/posts/{ordinal}` writing an Edit record, `DELETE` writing a Delete record, and `POST …/restore` writing a Restore.

Every one of these is an append. Nothing overwrites a byte in the log, which is why edit history and soft deletion fall out of the format for free rather than needing a history table.

The author window rules land in the moderation epic. What this ticket owns is that the records are written correctly and that the index entry for the post moves to the current revision.

## Acceptance criteria

- [ ] An edit appends a record, increments the revision, and repoints the index entry at the new revision
- [ ] A delete leaves the original Post bytes intact and sets the deleted flag in the index
- [ ] A restore clears the flag and the post returns at its current revision
- [ ] Editing a deleted post is refused rather than silently reviving it
