---
schema: 3
id: TKT-01M29CYSBAVXNFWDZVAK5PD71S
title: Implement merge into an existing thread
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

The mirror of split: selected posts move into an existing thread, appended in ordinal order, with bidirectional notices.

Merging two active threads is the common case, and it produces posts arriving out of chronological order in the target. Decide whether merged posts sort by their original timestamps or land at the end, and say so, because readers will notice either way.

Reversal is by the opposite operation rather than an undo, which is worth telling a moderator before they commit.

## Acceptance criteria

- [ ] Merged posts keep their original authors and timestamps and carry an origin reference
- [ ] The ordering rule for merged posts is decided, documented, and applied consistently
- [ ] Both threads carry notices linking to each other
- [ ] Reply_to references inside the merged set still resolve after the move
