---
schema: 3
id: TKT-01M39C6XFMQFYYNSPG0AN5FF0M
title: Rendered-post cache ignores later handle resolution for mentions
type: bug
status: draft
status_reason: null
priority: low
due_on: null
labels: []
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-24T09:33:40Z
updated_at: 2026-09-24T09:33:40Z
created_by:
  id: agent:claude-code/phase1-run-415af6aa
  name: ""
updated_by:
  id: agent:claude-code/phase1-run-415af6aa
  name: ""
extensions: {}
---

## Description

The rendered-post cache (`render.Cache`) is keyed by thread, ordinal, revision and renderer version. A mention resolves a handle against the users table at render time: a handle nobody has renders as plain text, and a known one renders as a link. A rendering cached before the handle existed, or before a handle rename (if renames ever land), keeps serving the old form until the post is next edited or the cache evicts it. The thread page's ETag does not move either, because the mentioned user is not necessarily one of the page's authors.

This is the mention counterpart of the quote problem that TKT-01M29CA9AX fixed in review 391. There, `render.Cache.PutQuoting` stores `Result.Quotes` with the HTML, and the service re-renders when a quoted post's deleted state changes. Unresolved mentions are not recorded in `Result`, so the same trick needs the renderer to report the handles it failed to resolve.

### Options
- Record unresolved handles in `Result`, store them with the cache entry, and re-render on a hit when any of them now resolves. Include a users "handle generation" in the ETag.
- Invalidate the render cache for posts mentioning a handle when a user is created or renamed. That needs a reverse index from handle to posts, which the mentions projection has only for resolved mentions.
- Accept it: handles are immutable today, and a mention made before signup going unlinked until the next edit is arguably correct.
