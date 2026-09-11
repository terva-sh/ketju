---
schema: 3
id: TKT-01M29CCBYGNF7T7R9MFNHW5X60
title: Implement the user profile endpoints
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:api
  - effort:S
assignees: []
milestone: phase-1
parent: TKT-01M29BKBMCNDSYHYF9WXAPDNRN
origin: null
dependencies:
  - TKT-01M29C801M9WBEAVCCC6AXNZ4Z
blocks_on: none
references:
  - ref: design:account-lifecycle
    path: docs/05-identity-and-accounts.md
claim: null
archive: null
created_at: 2026-09-11T23:20:57Z
updated_at: 2026-09-11T23:20:57Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:terva/mieli
  name: Mieli
extensions: {}
---

## Description

`GET /users/{handle}` and `GET /users/{handle}/posts`, the latter served from the `post_meta` index on (author_id, created_at), which is one of the two reasons that projection table exists.

An anonymized user is the case to get right. Their posts remain, their attribution is suppressed at render time by the `users.anonymized` flag, and their profile must not resurrect the handle or display name that was cleared.

## Acceptance criteria

- [ ] A post history spans forums and excludes posts in forums the viewer cannot read
- [ ] An anonymized user's profile shows the placeholder handle and no prior identity
- [ ] Deleted posts are omitted from a member's view of a history and shown to moderators
- [ ] Paging is cursor-based on created_at
