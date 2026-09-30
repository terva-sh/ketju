---
schema: 3
id: TKT-01M3S3ZF48TJFWH1D1D75NB2TC
title: Decide between relative and absolute post times
type: spike
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
created_at: 2026-09-30T12:17:38Z
updated_at: 2026-09-30T12:17:38Z
created_by:
  id: agent:claude-code/phase2-run-39wbn4q
  name: ""
updated_by:
  id: agent:claude-code/phase2-run-39wbn4q
  name: ""
extensions: {}
---

## Description

Found comparing the forum-index and thread screenshots with `docs/mockups/` (TKT-01M39WBN4Q, divergence F5, first listed as item 5 of the note of 2026-09-25 on TKT-01M29CEE5Y).

The mockups and docs/06 "Post anatomy" write times relative to now: "14 min ago", "yesterday", "3 h ago". The client writes them absolute and short: the time for today, the weekday and time within a week, else the date (`web/client/src/components/thread/PostTime.tsx`). Nothing records why, and the two are not the same product.

### What to decide
Relative, absolute, or relative with a cut-over to absolute, for posts, for the forum index's last post, and for the thread list. This is what a member sees, so it is a maintainer's decision (docs/12 "Gaps the design leaves open"). Record it in docs/06, then change PostTime to match.

## Acceptance criteria

- [ ] docs/06 records whether post, last-post and thread-list times are relative, absolute, or relative with a cut-over, and PostTime follows it
