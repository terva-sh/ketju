---
schema: 3
id: TKT-01M3QQWA5ZNT0WMBY1F7ASPG1C
title: Add the flag item and dialog to the post menu
type: task
status: draft
status_reason: null
priority: high
due_on: null
labels:
  - area:moderation
  - area:client
  - effort:S
assignees: []
milestone: phase-2
parent: TKT-01M29BN36JH4M1W5819D06PGPP
origin: TKT-01M29CP2PJ23SK6AN8M0CJY4JT
dependencies:
  - TKT-01M29CP2PJ23SK6AN8M0CJY4JT
blocks_on: none
references:
  - ref: design:flags
    path: docs/08-moderation.md
claim: null
archive: null
created_at: 2026-09-29T23:26:57Z
updated_at: 2026-09-29T23:27:06Z
created_by:
  id: agent:claude-code/phase2-run-cp2pj
  name: ""
updated_by:
  id: agent:claude-code/phase2-run-cp2pj
  name: ""
extensions: {}
---

## Description

The client half of TKT-01M29CP2PJ (Implement flags with the auto-hide threshold), filed from it because the server lane may not change `internal/web/spa/dist` while another lane holds it.

The server route exists: `POST /api/v1/threads/{id}/flags` with `{ordinal, reason, note}` answers 200 `FlagReceipt` `{"received": true}` (docs/07). `dto.ts` already declares `FlagReceipt`.

### What to build

- A "Flag" item in the post menu (`components/thread/Post.tsx`) for a signed-in member, on a post that is shown (not `deleted`).
- A dialog offering the four reasons (spam, abuse, off-topic, other) and a note, required for other and at most 1000 characters.
- On success it says "Thanks, a moderator will look." and nothing else (docs/08 "Flags"). It never shows a count or an outcome.
- A `validation-failed`, `conflict` or `unavailable` answer shows a short message. `unavailable` still means the report was kept.
- Regenerate `dist` with `go generate ./internal/web/spa` and keep the bundle under budget.

The tombstone text for `removal: "review"` ("Post hidden pending review", and "hidden pending review" for the author and moderators) is TKT-01M29CKRJ9's client half, not this ticket.

## Acceptance criteria

- [ ] A signed-in member sees Flag in the menu of a shown post and not of a removed one (Post.test.tsx)
- [ ] The dialog sends one of the four reasons, requires a note for other, and on 200 says 'Thanks, a moderator will look.' and nothing else
- [ ] The built client is regenerated and within the bundle budget
