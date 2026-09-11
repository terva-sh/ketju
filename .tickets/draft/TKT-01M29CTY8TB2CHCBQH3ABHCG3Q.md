---
schema: 3
id: TKT-01M29CTY8TB2CHCBQH3ABHCG3Q
title: Build the search API and results screen
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:search
  - effort:M
assignees: []
milestone: phase-3
parent: TKT-01M29BN36D34YVKVRPNQD3PHDV
origin: null
dependencies:
  - TKT-01M29CRJ3BEMZREJQRGAK3AVGK
  - TKT-01M29CEE4KNQBFP49FVSCRNZA4
blocks_on: none
references:
  - ref: design:api-resources
    path: docs/07-api-and-clients.md
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

`GET /search?q=&forum=&author=&after=&before=` returning FTS5 snippets, and the results screen behind it.

Permission filtering has to happen in the query rather than after it, or a result count leaks the existence of posts in a restricted forum even when the bodies are withheld.

Snippets come from FTS5 rather than from re-reading logs, which keeps a search over half a million posts off the storage path entirely.

## Acceptance criteria

- [ ] Results exclude forums the viewer cannot read, filtered inside the query rather than after it
- [ ] Snippets highlight matched terms and come from the index
- [ ] Filters by forum, author, and date range combine correctly
- [ ] A query returning nothing gives a useful empty state rather than a blank page
