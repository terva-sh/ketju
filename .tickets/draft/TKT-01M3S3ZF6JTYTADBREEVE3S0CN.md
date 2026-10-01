---
schema: 3
id: TKT-01M3S3ZF6JTYTADBREEVE3S0CN
title: Show who started a thread, when, and its post count under the title
type: task
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
created_at: 2026-09-30T12:17:38Z
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

Found comparing the thread screenshots with `docs/mockups/Thread{Light,Dark}.dc.html` and `MobileThread{Light,Dark}.dc.html` (TKT-01M39WBN4Q, divergences T2 and P5).

Under the title the mockups draw a meta line: "started by mara · 3 days ago · 212 posts", then the tag chip, the pin and the watch control. The tag chip is owned by TKT-01M29CYSCP (Implement thread tags), the pin note by TKT-01M3QGH6 (Add lock, unlock, pin and unpin to the thread screen), and watching by TKT-01M29CRJ29 (Implement watches and mutes). Nothing owns the first part: who started the thread, how long ago, and how many posts it has. The client's thread head draws the forum and the title only (`web/client/src/screens/ThreadScreen.tsx`, `thread-head`).

### What to do
Draw "started by HANDLE · AGE · N posts" under the title at both widths, from the thread page's DTO, adding the starter to it if the page does not carry one. How the age is written follows TKT-01M3S3ZF48 (Decide between relative and absolute post times).

## Acceptance criteria

- [ ] The thread head draws started by HANDLE, the age and N posts at 390 px and 1440 px
