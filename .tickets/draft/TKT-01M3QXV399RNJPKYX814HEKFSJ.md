---
schema: 3
id: TKT-01M3QXV399RNJPKYX814HEKFSJ
title: Add rename and move to the thread screen
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
  - TKT-01M29CKRH92JGZ1WY58JXC4T2G
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-30T01:11:09Z
updated_at: 2026-09-30T01:11:12Z
created_by:
  id: agent:claude-code/phase2-run-krh9
  name: ""
updated_by:
  id: agent:claude-code/phase2-run-krh9
  name: ""
extensions: {}
---

## Description

Split from TKT-01M29CKRH9 (Implement thread rename and move), whose plan step 5 was the client. The server lane could not change `internal/web/spa/dist` while the moderator-queue lane held it (docs/13 "Running two lanes"), so the routes landed with only the one client change the new problem code forced: the `rename-window-closed` message in `web/client/src/api/problem.ts`.

The routes are in docs/07 "Renaming and moving threads":
- `PATCH /api/v1/threads/{id}` takes `{title}`. The thread's author may rename it for `threads.rename_window_seconds` (default 86400) after starting it, and a moderator may at any time. Past the window the author gets `rename-window-closed`.
- `POST /api/v1/admin/threads/{id}/move` takes `{forum}`, the destination's slug. It is behind the staff gate and needs `moderate` in both forums. An archived destination is `conflict` except to an admin.
- Both answer 200 with `ThreadHeader`, `no-store`.

What the groomed plan asked of the client:
- Rename in the thread actions, for a moderator and for the author while the window is open. The window has to reach the client per viewer. The plan named a `rename_until` field (Unix ms, or null) in the thread viewer block, which is per viewer and never cached. That is a server change to `internal/dto` `ThreadViewer`, `View` in `internal/service/threads/view.go`, the page goldens, and `web/client/src/api/dto.ts`, and it belongs here.
- Move for a moderator whose role is moderator or admin, with a forum picker (`screens/ForumPicker.tsx`).
- After either action, navigate to the header's new canonical URL. `canonicalThreadUrl` already keeps the ordinal, `/history`, the page, the lens and the hash.
- Run `go generate ./internal/web/spa` and commit `dist/`.

## Acceptance criteria

- [ ] The thread viewer block carries rename_until, the end of the viewer's rename window in Unix ms or null, and the page goldens and dto.ts follow
- [ ] The thread actions offer Rename to a moderator, and to the author while rename_until has not passed, and Move to a viewer with can_moderate whose role is moderator or admin
- [ ] After a rename or a move the screen shows the answer's header and replaces the URL with the canonical one, keeping ordinal, /history, page, lens and hash
- [ ] rename-window-closed, forbidden and conflict answers show their catalogue message and keep the dialog open
- [ ] The thread-view budget holds, and the dist regenerated with go generate ./internal/web/spa is committed
