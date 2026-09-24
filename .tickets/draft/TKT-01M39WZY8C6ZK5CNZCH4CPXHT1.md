---
schema: 3
id: TKT-01M39WZY8C6ZK5CNZCH4CPXHT1
title: Build the post history screen
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:client
  - effort:M
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-24T14:26:57Z
updated_at: 2026-09-24T14:26:57Z
created_by:
  id: agent:claude-code/phase1-run-415af6aa
  name: ""
updated_by:
  id: agent:claude-code/phase1-run-415af6aa
  name: ""
extensions: {}
---

## Description

The route `/t/{id}/{slug}/{ordinal}/history` (docs/07 "URL scheme") has no screen yet. Review 421 on PR #64, for TKT-01M29CEE6V (Build the thread page with pager and unread divider), found that the thread page had taken it over and shown the ordinary post list there. PR #64 put back a placeholder: the thread's title, "History of post #N", "This screen is not built yet", and a link back to the post. The placeholder also keeps the slug correction.

The endpoint exists: `GET /api/v1/threads/{id}/posts/{ordinal}/history` (TKT-01M29CA9C2) answers `{ordinal, deleted, revisions: [{revision, actor, at, reason, source}]}`, and the client diffs two revisions' markdown sources (docs/08 "Post history"; settled for CA9C2). The access rules are the endpoint's:
- an anonymous reader is unauthenticated;
- a member is forbidden when `public_edit_history` is off;
- a moderator can always read the history.

A deleted post answers `deleted: true` with no revisions.

Build the screen: list the revisions with actor, time and reason, and show a word-level diff between any two. Render ErrorPanel for the refusals. The Post footer's "history" link already points at this route.

## Acceptance criteria

- [ ] The history route lists a post's revisions with actor, time and reason
- [ ] Any two revisions can be compared as a word-level diff of their sources
- [ ] Refusals (unauthenticated, forbidden) and a deleted post's withheld history render through ErrorPanel or a stated notice
