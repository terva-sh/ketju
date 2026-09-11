---
schema: 3
id: TKT-01M29BYYJ87H5ERS3RXC66XF44
title: Add the projection tables and projection_state
type: task
status: draft
status_reason: null
priority: urgent
due_on: null
labels:
  - area:db
  - effort:M
assignees: []
milestone: phase-1
parent: TKT-01M29BKBKV6HH8W0DN6QXQNZ0R
origin: null
dependencies:
  - TKT-01M29BW9Q17KEBMHJ91G6QHW36
blocks_on: none
references:
  - ref: design:data-model-projections
    path: docs/04-data-model.md
claim: null
archive: null
created_at: 2026-09-11T23:13:37Z
updated_at: 2026-09-11T23:13:37Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:terva/mieli
  name: Mieli
extensions: {}
---

## Description

`threads`, `thread_tags`, `post_meta`, `mentions`, and `projection_state`: every row here is derived from a thread log and every one is disposable.

`post_meta` exists only for questions that cross thread boundaries: a user's post history, reverse reply lookups when the thread is not open, and moderation lists. Within-thread questions are answered by the index file, and adding a column here that duplicates an index field is the mistake to avoid.

`threads.log_length_projected` and `projection_state.log_length` are what startup catch-up compares against the real log, so they are the mechanism rather than bookkeeping.

## Acceptance criteria

- [ ] Every table in this migration is marked as a projection in a comment naming `neoforum reindex` as its recovery path
- [ ] post_meta rows stay tiny, with the indexes from docs/04 on (author_id, created_at) and (thread_id, reply_to)
- [ ] Dropping every table in this migration and rebuilding from logs produces identical rows, tested end to end
