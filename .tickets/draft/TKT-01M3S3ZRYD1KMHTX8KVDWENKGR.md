---
schema: 3
id: TKT-01M3S3ZRYD1KMHTX8KVDWENKGR
title: Add the front page footer with member and post counts
type: task
status: draft
status_reason: null
priority: low
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
created_at: 2026-09-30T12:17:48Z
updated_at: 2026-09-30T12:17:48Z
created_by:
  id: agent:claude-code/phase2-run-39wbn4q
  name: ""
updated_by:
  id: agent:claude-code/phase2-run-39wbn4q
  name: ""
extensions: {}
---

## Description

Found comparing `web/client/e2e/screenshots/forum-index-desktop-*.png` with `docs/mockups/Main.dc.html` and `FrontDark.dc.html` (TKT-01M39WBN4Q, divergence F6, first listed as item 8 of the note of 2026-09-25 on TKT-01M29CEE5Y).

The mockup ends the front page with a footer: "34 members · 1,208 posts · Press ? for keyboard shortcuts · Guidelines · Markdown help". The client draws no footer. The counts are not in the API, and the guidelines and Markdown help pages do not exist.

### What to do
Add the member and post counts to the forum index's response, and draw the footer with the "?" hint, which opens the help overlay that exists. Leave Guidelines and Markdown help out until their pages exist, or file them.

## Acceptance criteria

- [ ] The forum index ends with the member and post counts and the ? shortcuts hint at both widths
