---
schema: 3
id: TKT-01M3NS16VHMVXZW0TG41KVTNSV
title: Build the new-thread screen with draft autosave
type: task
status: draft
status_reason: null
priority: high
due_on: null
labels:
  - area:client
  - effort:M
assignees: []
milestone: phase-2
parent: TKT-01M29BN367F3YQZESM63VN26GP
origin: null
dependencies:
  - TKT-01M3K9XBJA8FSJ8ADCEY284SGA
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-29T05:08:37Z
updated_at: 2026-09-29T05:08:37Z
created_by:
  id: agent:claude-code/phase2-groom
  name: ""
updated_by:
  id: agent:claude-code/phase2-groom
  name: ""
extensions: {}
---

## Description

The route `/f/:forum/new` is a placeholder (`web/client/src/routes.tsx:113`), although the forum index (`screens/ForumIndex.tsx:57,88`) and the thread list (`screens/ThreadListScreen.tsx:72`) both link to it. The server has everything the screen needs:
- `POST /api/v1/forums/{slug}/threads` creates the thread (TKT-01M29CA9B9);
- `POST /api/v1/render` previews it;
- `GET/PUT/DELETE /api/v1/forums/{slug}/draft` autosaves it, title included, once TKT-01M3K9XBJA (Autosave new-thread drafts) lands.

No ticket builds the screen. Found while planning TKT-01M3K9XBJA.

### What to build
- A new-thread screen with a title field and the existing composer (toolbar, preview tab, server preview): inline on desktop, and the composer sheet on the phone (docs/06 "Composing").
- The draft is loaded on open and filled in, saved on a debounce while typing (title and body), and deleted after the thread is created.
- A forum the reader cannot create in shows the refusal the API gives. The same goes for an archived forum.
- After creating, go to the new thread.
- Rebuild the dist (`go generate ./internal/web/spa`) and stay within the 60000 B gzip budget.

## Acceptance criteria

- [ ] A member can create a thread from /f/{slug}/new at 390 px and 1440 px, and lands on it
- [ ] The title and body typed on one device are restored from the server draft on another, and the draft is deleted once the thread is created
- [ ] A Playwright test covers creating a thread and restoring a draft, and the bundle stays within the 60000 B gzip budget
