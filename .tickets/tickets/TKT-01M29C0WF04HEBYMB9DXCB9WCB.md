---
schema: 3
id: TKT-01M29C0WF04HEBYMB9DXCB9WCB
title: Implement the rendered-post cache
type: task
status: in-progress
status_reason: null
priority: high
due_on: null
labels:
  - area:render
  - effort:M
assignees: []
milestone: phase-1
parent: TKT-01M29BKBM7004ZPAA9T81HC939
origin: null
dependencies:
  - TKT-01M29BW9Q8C8NW2MJH9FKVZA5D
blocks_on: none
references:
  - ref: design:storage-caching
    path: docs/03-thread-storage.md
claim:
  actor: agent:claude-code/phase1
  branch: rendered-post-cache
  worktree: /home/sothr/workspace/git.local.sothr.com/terva-sh/ketju
  commit: 2ba0652d8f3c8381835910093229e29561ab567d
  session: null
  claimed_at: 2026-09-16T18:51:36Z
  expires_at: null
archive: null
created_at: 2026-09-11T23:14:40Z
updated_at: 2026-09-16T18:51:36Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase1
  name: ""
extensions: {}
---

## Description

An LRU bounded by bytes, default 256 MB, keyed on thread, ordinal, revision, and renderer version. HTML lives here and nowhere else: not in the log, not in the database.

The revision in the key is why an edit invalidates exactly one entry, and the renderer version is why a pipeline upgrade invalidates everything at once without a rewrite. Bounding by bytes rather than entry count matters because post sizes vary by orders of magnitude and a count-based bound would either waste memory or blow past it.

## Acceptance criteria

- [ ] The cache is bounded by total bytes and stays within the configured limit under a mixed workload of large and small posts
- [ ] Editing a post invalidates only that post's entry
- [ ] Bumping the renderer version makes every prior entry unreachable
- [ ] Hit ratio is exposed as a metric
