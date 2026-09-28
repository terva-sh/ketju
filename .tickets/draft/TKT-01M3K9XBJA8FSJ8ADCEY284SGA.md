---
schema: 3
id: TKT-01M3K9XBJA8FSJ8ADCEY284SGA
title: Autosave new-thread drafts, and decide whether the title is saved
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels: []
assignees: []
milestone: phase-2
parent: TKT-01M29BKBMCNDSYHYF9WXAPDNRN
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-28T06:05:54Z
updated_at: 2026-09-28T22:29:24Z
created_by:
  id: agent:claude-code/phase1-run-438e26c1
  name: ""
updated_by:
  id: agent:claude-code/phase1-run-438e26c1
  name: ""
extensions: {}
---

## Description

The drafts table (TKT-01M29BYYHQ) keys a new-thread draft by forum: `thread_id` null, `forum_id` set, one per user per forum through the `drafts_new_thread` index. TKT-01M29CA9CE built only the reply draft, `GET/PUT/DELETE /api/v1/drafts/{thread_id}`, because that is what its criteria name, and a new-thread draft has no thread id to put in that URL.

Two things are open, and the first is a product decision:

1. Whether a new-thread draft saves the thread's title. The table has no title column (docs/04 lists none), so as it stands the body autosaves and the title does not. Adding one is a one-line migration, but it changes what a member gets back on another device, which is a person's call.
2. The route. `/api/v1/forums/{slug}/draft` fits the forum-keyed row and the create route `POST /forums/{slug}/threads`; the same rules as the reply draft would apply (member session, forum readable, `create_thread` to save, 204 when there is none).

The form-post table in docs/07 "Form posts for viewer state" needs a row for whatever route is chosen, and the test in internal/web/api that checks it needs the route added to its list.

## Acceptance criteria

- [ ] A decision on saving the thread title is recorded in docs/04 drafts
- [ ] A new-thread draft can be saved, read and deleted per user and forum through the API
- [ ] docs/07's form-post table has a row for the new route

## Notes

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T22:29:23Z

Maintainer decision, 2026-09-28: a new-thread draft saves the title. Add a nullable title column to drafts (null for reply drafts) in this ticket's migration. docs/04 'drafts' now shows the column and says this ticket adds it. Moved into phase-2 during grooming. Carry this into the plan's Settled section.
