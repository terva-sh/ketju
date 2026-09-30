---
schema: 3
id: TKT-01M3S3Z9E1RX86KA4WX4DQKWBJ
title: Stack author and time in the phone post header
type: bug
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:client
assignees: []
milestone: null
parent: TKT-01M29BN367F3YQZESM63VN26GP
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-30T12:17:32Z
updated_at: 2026-09-30T12:17:32Z
created_by:
  id: agent:claude-code/phase2-run-39wbn4q
  name: ""
updated_by:
  id: agent:claude-code/phase2-run-39wbn4q
  name: ""
extensions: {}
---

## Description

Found comparing `web/client/e2e/screenshots/thread-phone-{light,dark}.png` with `docs/mockups/MobileThread{Light,Dark}.dc.html` (TKT-01M39WBN4Q, divergence P2).

docs/06 "Mobile" says: "One column, 30 px avatars, author and time stacked". The mockup draws the name and role badge on the first line and "#61 Tue 14:02" on the second. The client draws one line on the phone, "#51 Name time", the same as the desktop header (`web/client/src/components/thread/Post.tsx`, the `post-header`).

### What to do
At the phone breakpoint, stack the header: author and badge first, then ordinal, time and "edited" on the second line. The reply-to indicator stays at the right of the header. Refresh `thread-phone-*.png` with `KETJU_SCREENSHOTS=1 npx playwright test e2e/thread.spec.ts`.

## Acceptance criteria

- [ ] At 390 px a post's header draws author and badge on one line and ordinal and time on the next, as docs/06 Mobile says
