---
schema: 3
id: TKT-01M3NS19315QTWBKNZN782VRF1
title: Autosave reply drafts from the composer
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:client
  - effort:S
assignees: []
milestone: phase-2
parent: TKT-01M29BN367F3YQZESM63VN26GP
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-29T05:08:40Z
updated_at: 2026-09-29T05:08:40Z
created_by:
  id: agent:claude-code/phase2-groom
  name: ""
updated_by:
  id: agent:claude-code/phase2-groom
  name: ""
extensions: {}
---

## Description

docs/06 says "Drafts autosave to the server (`drafts` table, one per user+thread)" and that the phone's reply bar shows "the first line of any draft". The server side landed with TKT-01M29CA9CE as `GET/PUT/DELETE /api/v1/drafts/{thread_id}`. The client never calls it. `composer/Composer.tsx:33` still says "Saving the draft is TKT-01M29CA9CE's; the source lasts the session", and `state/composer.ts` holds the source in memory only.

No ticket covers the client half. Found while planning TKT-01M3K9XBJA (Autosave new-thread drafts), whose new-thread counterpart is filed separately.

### What to build
- When the composer opens on a thread, load the draft and fill in the source and reply target.
- Save on a debounce while typing, the way `usePreview` debounces the preview (`Composer.tsx:306-325`), and on leaving the page.
- Delete the draft after a successful post.
- The reply bar shows the draft's first line.
- Rebuild the dist (`go generate ./internal/web/spa`) and stay within the 60000 B gzip budget.

## Acceptance criteria

- [ ] A reply typed and left unsent is restored, with its reply target, when the thread is reopened on another device
- [ ] The draft is deleted after the reply is posted, and the reply bar shows a saved draft's first line
- [ ] Vitest covers the debounce and the delete-after-post, and the bundle stays within the 60000 B gzip budget
