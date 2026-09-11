---
schema: 3
id: TKT-01M29C0WEEPGNGE9EDDTK1AZHN
title: Add quote syntax, linked blockquotes, and quote backlinks
type: task
status: draft
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
  - ref: design:composer
    path: docs/06-thread-views.md
claim: null
archive: null
created_at: 2026-09-11T23:14:40Z
updated_at: 2026-09-11T23:14:40Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:terva/mieli
  name: Mieli
extensions: {}
---

## Description

A goldmark extension for the `[quote=#37]` attribution line, rendering it as a collapsible blockquote linked to post 37, and recording the quote as a backlink so the quoted post can show who quoted it.

Quoting is how a linear thread refers backwards without indenting, so it carries weight the syntax does not suggest. A quote of a post that was later deleted must render as a quote of a tombstone rather than leaking the removed text.

## Acceptance criteria

- [ ] A quote block renders with a link to the quoted ordinal and is collapsible in the client
- [ ] Quoting a deleted post renders the tombstone rather than the original text
- [ ] Backlinks are emitted from the render call so the projection writer can record them
- [ ] A quote of an ordinal that does not exist renders as a plain blockquote rather than failing the post
