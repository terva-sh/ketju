---
schema: 3
id: TKT-01M29CTY5TZ1QMWK6BTGA21BWQ
title: Implement the reply chain lens
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:views
  - effort:M
assignees: []
milestone: phase-3
parent: TKT-01M29BN36D34YVKVRPNQD3PHDV
origin: null
dependencies:
  - TKT-01M29CRJ1PY355KX3HX7A5N785
blocks_on: none
references:
  - ref: design:lenses
    path: docs/06-thread-views.md
claim: null
archive: null
created_at: 2026-09-11T23:28:54Z
updated_at: 2026-09-11T23:28:54Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:terva/mieli
  name: Mieli
extensions: {}
---

## Description

`?lens=chain:42` showing the ancestor line of post 42, found by following reply_to upward, plus its direct replies, in ordinal order, with omitted ranges rendered as "… 5 posts …" dividers.

This is the lens that answers what a tree would have answered, without becoming one. The posts stay in ordinal order and the gaps are visible, so a reader always knows they are looking at a slice rather than the thread.

A reply_to cycle should be impossible, but the walk needs a bound anyway, because a corrupt index is a thing that happens and an infinite loop in a request handler is worse than a wrong answer.

## Acceptance criteria

- [ ] The ancestor walk is bounded and terminates on a malformed reply_to chain
- [ ] Results are in ordinal order with gap dividers showing the count omitted
- [ ] The lens runs from the index alone with no database query
- [ ] A post with no reply_to and no replies yields itself with a clear empty-chain message
