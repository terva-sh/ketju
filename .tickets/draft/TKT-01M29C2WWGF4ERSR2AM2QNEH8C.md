---
schema: 3
id: TKT-01M29C2WWGF4ERSR2AM2QNEH8C
title: Return structured mentions, quotes, and reply_to from the render call
type: task
status: draft
status_reason: null
priority: high
due_on: null
labels:
  - area:render
  - effort:S
assignees: []
milestone: phase-1
parent: TKT-01M29BKBM7004ZPAA9T81HC939
origin: null
dependencies:
  - TKT-01M29C0WEEPGNGE9EDDTK1AZHN
  - TKT-01M29C0WEQ2SBTSZSHSV8Z989S
blocks_on: none
references:
  - ref: design:api-render
    path: docs/07-api-and-clients.md
claim: null
archive: null
created_at: 2026-09-11T23:15:46Z
updated_at: 2026-09-11T23:15:46Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:terva/mieli
  name: Mieli
extensions: {}
---

## Description

One render call returns HTML plus the structured facts the post carries: resolved mention user ids, quoted ordinals, and the validated reply_to. The composer preview endpoint returns the same shape, so the client sees before posting exactly what the server will record.

Extraction happening at render time is what keeps the projection writer, the notifier, and the search indexer from each parsing markdown in their own slightly different way.

## Acceptance criteria

- [ ] A single call returns html, mentions, quotes, and reply_to
- [ ] The preview endpoint and the stored-post path return identical structured output for identical source
- [ ] A reply_to pointing at a nonexistent or out-of-thread ordinal is rejected at validation rather than stored
- [ ] No consumer of these facts parses markdown itself
