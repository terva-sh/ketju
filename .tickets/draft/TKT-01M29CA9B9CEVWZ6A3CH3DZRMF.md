---
schema: 3
id: TKT-01M29CA9B9CEVWZ6A3CH3DZRMF
title: Implement create thread and reply
type: task
status: draft
status_reason: null
priority: urgent
due_on: null
labels:
  - area:api
  - effort:M
assignees: []
milestone: phase-1
parent: TKT-01M29BKBMCNDSYHYF9WXAPDNRN
origin: null
dependencies:
  - TKT-01M29C0WD88M1C8EW4QQTX76NJ
  - TKT-01M29C2WWGF4ERSR2AM2QNEH8C
  - TKT-01M29C5Y2EVNCBADSQYW12F01D
  - TKT-01M29P9RS0YMM3465BSH7AGSNJ
blocks_on: none
references:
  - ref: design:create-post-flow
    path: docs/02-architecture.md
claim: null
archive: null
created_at: 2026-09-11T23:19:48Z
updated_at: 2026-09-24T05:05:28Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude/phase1-groom
  name: ""
extensions: {}
---

## Description

`POST /forums/{slug}/threads` and `POST /threads/{id}/posts`, following the docs/02 create-post flow: rate limit and permission check, thread not locked, slow mode respected, render-validate the markdown, append through the thread actor, publish the event, and return the rendered post so the client can insert it immediately.

Rendering happens before the append and the HTML is discarded. That is deliberate: a post whose markdown cannot be rendered should be refused rather than written to an append-only log forever.

## Acceptance criteria

- [ ] A post is durable before the response returns, and the response carries ordinal, revision, and log length
- [ ] A locked thread, a missing permission, and an active slow mode each refuse the write with their own problem code
- [ ] Source that fails to render is refused before anything is appended
- [ ] The rendered post in the response is identical to what a later read returns
