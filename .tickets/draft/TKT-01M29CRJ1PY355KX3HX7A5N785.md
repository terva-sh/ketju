---
schema: 3
id: TKT-01M29CRJ1PY355KX3HX7A5N785
title: Build the lens framework over the in-memory index
type: task
status: draft
status_reason: null
priority: high
due_on: null
labels:
  - area:views
  - effort:M
assignees: []
milestone: phase-3
parent: TKT-01M29BN36D34YVKVRPNQD3PHDV
origin: null
dependencies:
  - TKT-01M29BYYGYZ8FQB2W2XC14S2T3
  - TKT-01M29CA9AXKRTS6KMN5CTH7461
blocks_on: none
references:
  - ref: design:lenses
    path: docs/06-thread-views.md
claim: null
archive: null
created_at: 2026-09-11T23:27:36Z
updated_at: 2026-09-11T23:27:36Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:terva/mieli
  name: Mieli
extensions: {}
---

## Description

One interface: given a thread's in-memory index and a viewer, return the ordinals to show, the count hidden, and any gap markers. Paging then applies to the filtered sequence.

A lens filters and never reorders. That constraint belongs in the interface itself, so a lens returns a selection of ordinals rather than a list of posts it could have arranged. The type makes the rule unbreakable instead of merely documented.

The `?lens=` parameter parses into a lens here, which is what makes every lens shareable by URL and available to the future HTML client for free.

## Acceptance criteria

- [ ] A lens returns ordinals in ascending order, and the interface makes any other order impossible to express
- [ ] Lens parsing from the URL is total: an unknown lens falls back to the canonical view rather than erroring
- [ ] The shown and hidden counts are returned for the lens header
- [ ] Paging applies to the filtered sequence and page numbers stay stable while the lens is held
