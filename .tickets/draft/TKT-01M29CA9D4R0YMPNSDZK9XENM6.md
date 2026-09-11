---
schema: 3
id: TKT-01M29CA9D4R0YMPNSDZK9XENM6
title: Implement POST /render for the composer preview
type: task
status: draft
status_reason: null
priority: high
due_on: null
labels:
  - area:render
  - area:api
  - effort:S
assignees: []
milestone: phase-1
parent: TKT-01M29BKBM7004ZPAA9T81HC939
origin: null
dependencies:
  - TKT-01M29C2WWGF4ERSR2AM2QNEH8C
  - TKT-01M29C5Y3SAD25GYYEMHYPDF24
blocks_on: none
references:
  - ref: design:api-render
    path: docs/07-api-and-clients.md
claim: null
archive: null
created_at: 2026-09-11T23:19:48Z
updated_at: 2026-09-11T23:19:48Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:terva/mieli
  name: Mieli
extensions: {}
---

## Description

`POST /api/v1/render` takes source and returns html, mentions, reply_to, and quotes. This is why no client-side markdown parser ships and why the preview is exact rather than approximate.

The same endpoint serves the future HTML client, which posts a form and gets a preview page back. Rate limiting matters here more than on most reads, because it renders arbitrary input for anyone with a session.

## Acceptance criteria

- [ ] Output is identical to what the post read path returns for the same source
- [ ] The endpoint is rate limited per session and its body limit matches the post limit
- [ ] A member session is required, so it is not an open rendering service
- [ ] The response shape is the same structured output the create path uses
