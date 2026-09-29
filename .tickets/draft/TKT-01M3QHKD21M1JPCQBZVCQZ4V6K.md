---
schema: 3
id: TKT-01M3QHKD21M1JPCQBZVCQZ4V6K
title: Show moderators' post controls and the edited-by-a-moderator marker
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
origin: TKT-01M29CRJ5J9XMJZ30FMZX4D473
dependencies:
  - TKT-01M29CRJ5J9XMJZ30FMZX4D473
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-29T21:37:14Z
updated_at: 2026-09-29T21:37:20Z
created_by:
  id: agent:claude-code/phase2-run-crj5j
  name: ""
updated_by:
  id: agent:claude-code/phase2-run-crj5j
  name: ""
extensions: {}
---

## Description

The server side of TKT-01M29CRJ5J (Allow moderators to edit and delete any post) lets a viewer with `moderate` in the forum edit, delete and restore any post, and serves `edited_by_moderator` on `dto.Post`. The client does not use either yet, because the Phase 2 run's server lane may not change the committed `internal/web/spa/dist`.

This is step 8 of that ticket's plan:
- `Post.tsx` offers Edit, Delete and Restore on every post to a viewer with `viewer.can_moderate`, not only on the viewer's own posts. A moderator's Restore applies to a post that is deleted or hidden by a moderator.
- A post with `edited_by_moderator: true` shows "(edited by a moderator)" where it would show "(edited)". It does not name the moderator; the post's history does (docs/08 "Post history").
- Regenerate `internal/web/spa/dist` with `go generate ./internal/web/spa`.

The tombstone wording and the author's view of their own hidden post are TKT-01M29CKRJ9, not this ticket.

## Acceptance criteria

- [ ] A viewer with viewer.can_moderate sees Edit, Delete and Restore on every post, and each calls the existing endpoint
- [ ] A post served with edited_by_moderator: true shows (edited by a moderator), naming nobody
- [ ] internal/web/spa/dist is regenerated and the bundle stays inside the budget
