---
schema: 3
id: TKT-01M3PTT97XM3DCGRTDNHHBNM5M
title: Offer edit and delete only inside the author windows in the client
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:moderation
  - area:client
  - effort:S
assignees: []
milestone: phase-2
parent: TKT-01M29BN36JH4M1W5819D06PGPP
origin: null
dependencies:
  - TKT-01M29CKRHSMCRM9T440314D2Q8
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-29T14:59:02Z
updated_at: 2026-09-29T14:59:06Z
created_by:
  id: agent:claude-code/phase2-run-ckrhs
  name: ""
updated_by:
  id: agent:claude-code/phase2-run-ckrhs
  name: ""
extensions: {}
---

## Description

The client half of TKT-01M29CKRHS (Implement author edit and delete windows with grace edits), split out because that ticket ran in the server lane and the client lane held the committed dist. The server now enforces the windows and answers `edit-window-closed` and `delete-window-closed` (403), and the client already has words for both in `src/api/problem.ts`. What it does not have is a way to know the windows before it offers the actions.

1. **DTO.** `dto.ThreadViewer` gains `author_windows: {edit_seconds, delete_seconds} | null`, null for a viewer who may moderate the forum, since they are bound by neither window. It is per viewer and lives in the viewer block, which is never cached (`internal/service/threads/view.go`). The windows come from `posts.edit_window_seconds` and `posts.delete_window_seconds`. Regenerate the DTO goldens and add the field to `web/client/src/api/dto.ts`, or `dto-contract.ts` fails the build.
2. **Controls.** `Post.tsx` offers Edit and Delete on the viewer's own post only while `created_at` plus the window has not passed, and Restore on their own deleted post under the delete window. A refusal code still shows its message if the client's clock disagrees with the server's.
3. Regenerate `internal/web/spa/dist`.

The windows are open for their first N seconds and closed from N on, so a window of 0 means the author may never act (docs/07 "Edit, delete and restore").

## Acceptance criteria

- [ ] The thread response's viewer block carries author_windows, null for a viewer who may moderate the forum, and it is not part of the cached posts
- [ ] Edit and Delete disappear from the viewer's own post once its window passes (fake timers), and neither is hidden for a viewer whose author_windows is null
- [ ] A refusal with edit-window-closed or delete-window-closed shows its message
