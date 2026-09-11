---
schema: 3
id: TKT-01M29CYSAMEVQ7MJG41HR7HPRH
title: Implement split posts into a new thread
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:moderation
  - effort:L
assignees: []
milestone: phase-4
parent: TKT-01M29BN36JH4M1W5819D06PGPP
origin: null
dependencies:
  - TKT-01M29CXJZZZN7X4EZX37JJBF91
  - TKT-01M29CEE6V358M5KCBJGHD58HM
blocks_on: none
references:
  - ref: design:split-merge
    path: docs/06-thread-views.md
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

Moderator multi-select with a checkbox per post in mod mode, then split to a new thread with a title prompt. The source shows "5 posts were moved to <link>" at the position of the first moved post, and the new thread carries a "split from <link>" header.

Drift is the known cost of flat threads, and moving posts is the answer to it. This is the feature that lets a moderator fix a conversation instead of scolding people for having it.

## Acceptance criteria

- [ ] Multi-select works at both viewport sizes, including on a phone where checkboxes compete for width
- [ ] The source thread shows the moved notice at the first moved ordinal and keeps every ordinal
- [ ] The new thread carries a link back to the source and the posts keep their original authors and timestamps
- [ ] Both threads read correctly after a reindex from logs alone
