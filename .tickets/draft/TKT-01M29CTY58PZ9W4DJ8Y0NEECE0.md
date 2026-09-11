---
schema: 3
id: TKT-01M29CTY58PZ9W4DJ8Y0NEECE0
title: Implement the author lens and the only-OP button
type: task
status: draft
status_reason: null
priority: normal
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

`?lens=author:handle` filtering on the index's author_id, with "Only OP" as a one-click button that applies it for the thread author. Question 15 in docs/10 leans toward the button living in the thread header, and this ticket settles it.

The header shows "showing 7 of 212 posts", which is the pattern every lens uses so nobody loses track of what they are not seeing.

No database query. The author id is in the index entry, which is the concrete payoff for the 40-byte entry decision.

## Acceptance criteria

- [ ] The lens runs from the in-memory index with no database query, proven by a query counter in a test
- [ ] Only OP is one click from the thread header and produces the same URL as the author lens
- [ ] The header shows shown and total counts
- [ ] An author with no posts left after moderation yields an empty state rather than an error
