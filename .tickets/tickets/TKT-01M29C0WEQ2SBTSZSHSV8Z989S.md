---
schema: 3
id: TKT-01M29C0WEQ2SBTSZSHSV8Z989S
title: Add mention syntax, resolution, and autocomplete data
type: task
status: in-progress
status_reason: null
priority: high
due_on: null
labels:
  - area:render
  - effort:M
assignees: []
milestone: phase-1
parent: TKT-01M29BKBM7004ZPAA9T81HC939
origin: null
dependencies:
  - TKT-01M29BW9Q8C8NW2MJH9FKVZA5D
blocks_on: none
references:
  - ref: design:composer
    path: docs/06-thread-views.md
claim:
  actor: agent:claude-code/phase1
  branch: mention-syntax
  worktree: /home/sothr/workspace/git.local.sothr.com/terva-sh/ketju
  commit: 642e05584354acd3f259da356e9afd82bf1b9934
  session: null
  claimed_at: 2026-09-16T19:06:42Z
  expires_at: null
archive: null
created_at: 2026-09-11T23:14:40Z
updated_at: 2026-09-16T19:06:42Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase1
  name: ""
extensions: {}
---

## Description

`@handle` resolved at render time to a user id and rendered as a profile link, with the resolved ids returned so the notifier and the mentions projection can use them.

Mentions are stored by user id rather than by handle, which is what keeps old posts correct after a handle change. The renderer resolves the handle at render time, so the stored source keeps the text the author typed while the link and the notification follow the person.

## Acceptance criteria

- [ ] A mention of an existing handle renders as a profile link and returns the resolved user id
- [ ] A mention of a handle that does not exist renders as plain text and produces no notification
- [ ] Mentions inside a code block or a quote are not resolved and produce no notification
- [ ] An endpoint supplies handle candidates for the composer's autocomplete
