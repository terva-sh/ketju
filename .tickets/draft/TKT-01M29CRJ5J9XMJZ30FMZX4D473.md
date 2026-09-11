---
schema: 3
id: TKT-01M29CRJ5J9XMJZ30FMZX4D473
title: Allow moderators to edit and delete any post
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:moderation
  - effort:S
assignees: []
milestone: phase-2
parent: TKT-01M29BN36JH4M1W5819D06PGPP
origin: null
dependencies:
  - TKT-01M29CA9BP9QTMN2JEDPM0YS67
blocks_on: none
references:
  - ref: design:moderation-primitives
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

A moderator may edit or soft-delete any post, with the Edit or Delete record recording them as the actor rather than the author.

A moderator edit of somebody else's words is the most invasive routine action in the system, and the only thing that makes it acceptable is that it is visible. The actor is in the record, the edit shows in history, and the history is public by default.

## Acceptance criteria

- [ ] A moderator edit records the moderator as the record actor while the post keeps its author
- [ ] The post shows an edited marker attributing the edit to a moderator
- [ ] Moderator actions bypass the author windows
- [ ] Every moderator edit and delete is in audit_log as well as the thread log
