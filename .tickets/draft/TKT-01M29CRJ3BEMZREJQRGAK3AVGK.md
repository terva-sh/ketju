---
schema: 3
id: TKT-01M29CRJ3BEMZREJQRGAK3AVGK
title: Build the FTS5 search index and indexer
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:search
  - effort:L
assignees: []
milestone: phase-3
parent: TKT-01M29BN36D34YVKVRPNQD3PHDV
origin: null
dependencies:
  - TKT-01M29C0WF9GFGVG7QSAYRAKAA4
  - TKT-01M29BW9Q17KEBMHJ91G6QHW36
blocks_on: none
references:
  - ref: design:data-model-search
    path: docs/04-data-model.md
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

An FTS5 table over thread_id, ordinal, revision, title, and plain source, fed from the event bus in batches.

The index is the largest object in the database, roughly the size of the plain text, and it is entirely rebuildable. Batching matters because a burst of posts should not mean a write transaction each.

Indexing plain source rather than HTML keeps markdown syntax out of the results and makes snippets readable.

## Acceptance criteria

- [ ] Posts are indexed in batches and an edit replaces rather than duplicates the entry
- [ ] Deleted posts are removed from the index and do not appear in results
- [ ] The index is built from plain source with markdown syntax stripped
- [ ] Indexing lag is observable and never blocks an append
