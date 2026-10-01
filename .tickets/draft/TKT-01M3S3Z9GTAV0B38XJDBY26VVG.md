---
schema: 3
id: TKT-01M3S3Z9GTAV0B38XJDBY26VVG
title: Order the phone post footer reply, quote as docs/06 says
type: bug
status: draft
status_reason: null
priority: low
due_on: null
labels:
  - area:client
assignees: []
milestone: phase-3
parent: TKT-01M29BN367F3YQZESM63VN26GP
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-30T12:17:32Z
updated_at: 2026-10-01T06:45:52Z
created_by:
  id: agent:claude-code/phase2-run-39wbn4q
  name: ""
updated_by:
  id: agent:claude-code/phase3-run-orchestrator
  name: ""
extensions: {}
---

## Description

Found comparing `web/client/e2e/screenshots/thread-phone-{light,dark}.png` with `docs/mockups/MobileThread{Light,Dark}.dc.html` (TKT-01M39WBN4Q, divergence P3).

docs/06 "Mobile" says: "Footer actions are reply, quote, and the reply expander", and the mockup draws "reply quote … ⋯". On the phone the client draws "quote reply … ⋯", the desktop order from docs/06 "Post anatomy" (`web/client/src/components/thread/Post.tsx`, the post footer).

### What to do
Put reply before quote in the phone footer and leave the desktop order as it is. Refresh `thread-phone-*.png`.

## Acceptance criteria

- [ ] At 390 px a post's footer reads reply, quote, then the reply expander; the desktop order is unchanged
