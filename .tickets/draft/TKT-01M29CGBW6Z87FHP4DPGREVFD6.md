---
schema: 3
id: TKT-01M29CGBW6Z87FHP4DPGREVFD6
title: Build the inline reply expander
type: task
status: draft
status_reason: null
priority: high
due_on: null
labels:
  - area:client
  - effort:M
assignees: []
milestone: phase-1
parent: TKT-01M29BN367F3YQZESM63VN26GP
origin: null
dependencies:
  - TKT-01M29CEE6V358M5KCBJGHD58HM
blocks_on: none
references:
  - ref: design:post-anatomy
    path: docs/06-thread-views.md
claim: null
archive: null
created_at: 2026-09-11T23:23:08Z
updated_at: 2026-09-11T23:23:08Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:terva/mieli
  name: Mieli
extensions: {}
---

## Description

The "3 replies" control that expands the replying posts inline, in place, read-only, and visually recessed, then collapses again. Ordering inside the expansion is ordinal order.

This is the whole non-tree bargain in one component. It gives a reader the reply-to-reply relationship they want without indenting the thread, and the rule that keeps it honest is that nothing on the canonical page moves.

Suppress the expander when the only reply is the post directly below, which is the same suppression rule the reply-to indicator uses.

## Acceptance criteria

- [ ] Expanding and collapsing never changes the position of any canonical post on the page
- [ ] Expanded posts are read-only and visually recessed, in ordinal order
- [ ] The expander is suppressed when the sole reply is directly below
- [ ] Expansion works for a reply that lives on another page
