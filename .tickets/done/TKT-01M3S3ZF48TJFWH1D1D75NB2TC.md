---
schema: 3
id: TKT-01M3S3ZF48TJFWH1D1D75NB2TC
title: Decide between relative and absolute post times
type: spike
status: done
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
updated_at: 2026-09-30T18:44:45Z
created_by:
  id: agent:claude-code/phase2-run-39wbn4q
  name: ""
updated_by:
  id: agent:claude-code/phase2-run-orchestrator
  name: ""
extensions: {}
---

## Description

Found comparing the forum-index and thread screenshots with `docs/mockups/` (TKT-01M39WBN4Q, divergence F5, first listed as item 5 of the note of 2026-09-25 on TKT-01M29CEE5Y).

The mockups and docs/06 "Post anatomy" write times relative to now: "14 min ago", "yesterday", "3 h ago". The client writes them absolute and short: the time for today, the weekday and time within a week, else the date (`web/client/src/components/thread/PostTime.tsx`). Nothing records why, and the two are not the same product.

### What to decide
Relative, absolute, or relative with a cut-over to absolute, for posts, for the forum index's last post, and for the thread list. This is what a member sees, so it is a maintainer's decision (docs/12 "Gaps the design leaves open"). Record it in docs/06, then change PostTime to match.

## Acceptance criteria

- [x] docs/06 records whether post, last-post and thread-list times are relative, absolute, or relative with a cut-over, and PostTime follows it

## Implementation plan

Record the maintainer's decision of 2026-10-01 (absolute times) in docs/06 "Post anatomy" and in docs/08 "Decided 2026-10-01". PostTime already writes absolute times (the time today, weekday and time within a week, else the date, with the full moment in title and datetime), and ForumIndex, ThreadRow, Post, PostPopover and ReplyExpander all use it, so no client change is needed. The alternatives were relative and relative with a cut-over; the maintainer chose absolute, and it needs no code change. An absolute time also doesn't go stale on a page left open, which is a reason to keep it rather than the reason it won.

## Summary

Decided by the maintainer on 2026-10-01: times are absolute. docs/06 "Post anatomy" now says so for posts, the forum index's last post and the thread list, and notes that the mockups' relative times predate the decision; docs/08 "Decided 2026-10-01" lists it. PostTime.tsx already follows the rule, so there is no client change.
