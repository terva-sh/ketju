---
schema: 3
id: TKT-01M3S3Z9GTAV0B38XJDBY26VVG
title: Order the phone post footer reply, quote as docs/06 says
type: bug
status: archived
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
archive:
  archived_at: 2026-10-01T07:55:27Z
  from_status: draft
  reason: Merged into TKT-01M3S3Z9E1 in Phase 3 grooming, 2026-10-01
created_at: 2026-09-30T12:17:32Z
updated_at: 2026-10-01T07:55:27Z
created_by:
  id: agent:claude-code/phase2-run-39wbn4q
  name: ""
updated_by:
  id: agent:claude-code/phase3-groom-polish
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

## Notes

**agent:claude-code/phase3-groom-polish** at 2026-10-01T07:55:26Z

Merged into TKT-01M3S3Z9E1 (Match the phone post header and footer to docs/06 Mobile) in Phase 3 grooming, 2026-10-01. Both change one post at the phone breakpoint in Post.tsx and thread.css, and both refresh thread-phone-*.png, so two client PRs would have queued for the dist to redraw the same post. This ticket's footer order lives on as that ticket's second criterion, and the order is settled by a run decision in docs/06 Mobile.

**agent:claude-code/phase3-groom-polish** at 2026-10-01T07:55:27Z

archived from draft: Merged into TKT-01M3S3Z9E1 in Phase 3 grooming, 2026-10-01
