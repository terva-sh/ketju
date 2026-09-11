---
schema: 3
id: TKT-01M29CCBY0Q99HSV87CC68GKC1
title: Implement the response cache keyed on viewer class
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
  - TKT-01M29CA9AXKRTS6KMN5CTH7461
blocks_on: none
references:
  - ref: design:api-thread-shape
    path: docs/07-api-and-clients.md
claim: null
archive: null
created_at: 2026-09-11T23:20:57Z
updated_at: 2026-09-11T23:20:57Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:terva/mieli
  name: Mieli
extensions: {}
---

## Description

A byte-bounded LRU of serialized JSON keyed on thread, page, lens, log_length, renderer version, and viewer class, where viewer class is anon, member, or moderator.

The key is invalid by construction when the thread changes, because log_length is in it. No invalidation logic exists and none should be added: a new log length is a new key and the old entry ages out.

Viewer class exists because moderators see tombstone contents. Three classes is the whole per-viewer variation in the cached part, and the read position and own-post markers stay outside it in the viewer block.

## Acceptance criteria

- [ ] A mutation changes the log length and therefore the key, with no explicit invalidation call anywhere
- [ ] A moderator never receives a cached member response and the reverse, tested
- [ ] The cache is bounded in bytes with its hit ratio exposed as a metric
- [ ] Viewer-specific data is never stored in a cached payload
