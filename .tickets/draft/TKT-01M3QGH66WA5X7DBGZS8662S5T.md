---
schema: 3
id: TKT-01M3QGH66WA5X7DBGZS8662S5T
title: Add lock, unlock, pin and unpin to the thread screen
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:moderation
  - area:client
assignees: []
milestone: phase-2
parent: TKT-01M29BN36JH4M1W5819D06PGPP
origin: null
dependencies:
  - TKT-01M29CKRGSF2FPY73MNNFPQ1FV
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-29T21:18:33Z
updated_at: 2026-09-29T21:18:33Z
created_by:
  id: agent:claude-code/phase2-run-ckrgs
  name: ""
updated_by:
  id: agent:claude-code/phase2-run-ckrgs
  name: ""
extensions: {}
---

## Description

Split from TKT-01M29CKRGS (Implement thread lock, unlock, pin, and unpin), whose plan step 6 was the client. The server lane could not change `internal/web/spa/dist` while the client lane held it (docs/13 "Running two lanes"), so the routes landed without a screen.

The routes are in docs/07 "Locking and pinning threads": `POST /api/v1/admin/threads/{id}/lock` and `/unlock` take `{reason?}`, `/pin` takes `{scope, until?}` (Unix ms), and `/unpin` takes `{scope}`. Each answers 200 with `ThreadHeader`, the thread page's header as it now reads, and needs no dto change.

What the groomed plan asked of the client:
- `web/client/src/shell/threadActions.ts` gains Lock/Unlock and Pin/Unpin when `viewer.can_moderate`.
- A small dialog, `components/thread/ModerateDialog.tsx`, takes a reason, or a scope and an optional expiry.
- The answer's header replaces the cached one.
- The thread head shows the existing "This thread is locked." and a pinned note.
- Run `go generate ./internal/web/spa` and commit `dist/`.

## Acceptance criteria

- [ ] The thread actions show Lock or Unlock, and Pin or Unpin, only when viewer.can_moderate
- [ ] The dialog sends the body each route takes, and the answer's header replaces the thread's cached header
- [ ] The thread head shows a locked thread's notice and an active pin, and a member's composer is closed in a locked thread
