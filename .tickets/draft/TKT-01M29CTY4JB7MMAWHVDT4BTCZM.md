---
schema: 3
id: TKT-01M29CTY4JB7MMAWHVDT4BTCZM
title: Implement the unread lens and jump to first unread
type: task
status: draft
status_reason: null
priority: high
due_on: null
labels:
  - area:views
  - effort:S
assignees: []
milestone: phase-3
parent: TKT-01M29BN36D34YVKVRPNQD3PHDV
origin: null
dependencies:
  - TKT-01M29CRJ1PY355KX3HX7A5N785
  - TKT-01M29BW9P4PXMFKF4VAP8K9QPD
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

`?lens=unread` showing posts after the viewer's last read ordinal, paged, and the jump-to-first-unread action that targets it.

This is the lens a daily reader uses most, and it has an awkward property worth handling deliberately: reading it moves the position that defines it. Decide whether the lens freezes its boundary for the duration of the view or re-evaluates on each page, and write the choice down, because the alternative is a lens that empties itself while somebody reads it.

## Acceptance criteria

- [ ] The lens boundary behavior under active reading is decided, documented, and tested
- [ ] Jump to first unread lands on the right post through the canonical view as well as the lens
- [ ] An anonymous viewer gets a clear message rather than an empty lens
- [ ] A fully read thread shows an empty state offering the full thread
