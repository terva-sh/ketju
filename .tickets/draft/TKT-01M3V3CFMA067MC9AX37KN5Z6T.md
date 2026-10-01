---
schema: 3
id: TKT-01M3V3CFMA067MC9AX37KN5Z6T
title: Build the member profile screen and their post list
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:client
  - effort:M
assignees: []
milestone: phase-3
parent: TKT-01M29BN367F3YQZESM63VN26GP
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-01T06:45:45Z
updated_at: 2026-10-01T06:45:45Z
created_by:
  id: agent:claude-code/phase3-run-orchestrator
  name: ""
updated_by:
  id: agent:claude-code/phase3-run-orchestrator
  name: ""
extensions: {}
---

## Description

`/u/{handle}` and `/u/{handle}/posts` route to placeholders. TKT-01M3QS0TMV (#245) put an Account block on the viewer's own profile, with "Link a sign-in", and TKT-01M3QVWKAG (#253) added Sign out there. The profile itself is still empty, although its endpoints are done (TKT-01M29CCBYG).

docs/07 defines both answers. `GET /users/{handle}` returns `{user, bio_html, joined_at, post_count, status}`, where `status` reaches only moderators and admins. `GET /users/{handle}/posts?cursor=` returns 25 posts to a page, newest first, each rendered as its thread page renders it, with a link to its thread and forum. Who may see a profile follows docs/05 "Whose profile is served" (maintainer decision 2026-09-28, TKT-01M3KJJNDC). A renamed handle redirects (docs/07, TKT-01M3NRP3W9).

Found when the maintainer used the dogfood instance on 2026-10-01 and asked how far the UI was from working. The Phase 3 run filed it under docs/14.

## Acceptance criteria

- [ ] The profile at /u/{handle} shows the member's handle, role badge, bio, join date and post count, and shows status only to moderators and admins
- [ ] The posts list at /u/{handle}/posts pages newest first by the API's cursor and links each post to its permalink and forum
- [ ] The viewer's own Account block keeps Link a sign-in and Sign out
- [ ] Tested at 390 and 1440 px with axe, and the dist is regenerated within the budget
