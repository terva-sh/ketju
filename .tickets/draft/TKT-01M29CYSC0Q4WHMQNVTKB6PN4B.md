---
schema: 3
id: TKT-01M29CYSC0Q4WHMQNVTKB6PN4B
title: Implement reply as a new thread
type: task
status: draft
status_reason: null
priority: low
due_on: null
labels:
  - area:moderation
  - effort:M
assignees: []
milestone: phase-4
parent: TKT-01M29BN36JH4M1W5819D06PGPP
origin: null
dependencies:
  - TKT-01M29CA9B9CEVWZ6A3CH3DZRMF
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

The member-facing version of split: open a composer for a new thread that quotes the source post, and leave a small "continued in <link>" note under it.

This is how drift gets handled before a moderator has to. Someone who notices they are about to take a thread sideways gets a one-click way to do it properly, which is cheaper than any amount of guidance about staying on topic.

## Acceptance criteria

- [ ] The new thread opens with the source post quoted and attributed
- [ ] The source post gains a continued-in note linking to the new thread
- [ ] The note is a thread event in the log so it survives a rebuild
- [ ] The action is available to members, not only moderators
