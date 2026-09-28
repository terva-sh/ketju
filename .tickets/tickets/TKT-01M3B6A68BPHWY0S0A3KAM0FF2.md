---
schema: 3
id: TKT-01M3B6A68BPHWY0S0A3KAM0FF2
title: Add per-forum post totals to the forum index
type: task
status: ready
status_reason: null
priority: low
due_on: null
labels:
  - area:api
  - effort:S
assignees: []
milestone: phase-1
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-25T02:29:04Z
updated_at: 2026-09-28T05:02:26Z
created_by:
  id: agent:claude-code/phase1-run-415af6aa
  name: ""
updated_by:
  id: agent:claude-code/phase1-run-438e26c1
  name: ""
extensions: {}
---

## Description

The forum index mockup (docs/mockups/Main.dc.html) shows a post total per forum, as in "148 threads · 3,912 posts". GET /forums (TKT-01M29CA9A5 "Implement the forums endpoints") leaves it out.

Computing it on read means summing `threads.post_count` per forum, which scans every thread on the front page's request. MeCounts and the forum index queries scan no table that grows with the site (TestMeCountsScansNoTable, TestForumIndexQueriesScanNoTable).

### Proposed
- Add a `posts` column to `forum_thread_counts` in a new migration.
- Keep it with the same triggers that keep `threads`: insert, delete, and updates of `forum_id` and `post_count`.
- Backfill it in the migration, and add `Forum.post_count` to the DTO.

### Open
- Should hidden or deleted posts count? `post_count` today is the projection's count, whatever that includes.

## Acceptance criteria

- [ ] GET /forums answers each forum's post total without scanning threads
