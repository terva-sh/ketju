---
schema: 3
id: TKT-01M29CP2Q36D13MT5DDF48RX9E
title: Build the post history view with word-level diffs
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:moderation
  - effort:M
assignees: []
milestone: phase-2
parent: TKT-01M29BN36JH4M1W5819D06PGPP
origin: null
dependencies:
  - TKT-01M29CA9C2CNBHGBD9304N0EJE
blocks_on: none
references:
  - ref: design:post-history
    path: docs/08-moderation.md
claim: null
archive: null
created_at: 2026-09-11T23:26:15Z
updated_at: 2026-09-28T22:29:01Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase1-run-438e26c1
  name: ""
extensions: {}
---

## Description

Every revision with its actor and time, and a word-level diff computed on demand from the two revisions' sources. Visible to moderators always, and to members when `public_edit_history` is on, which it is by default.

Public history by default is a trust decision rather than a technical one. In a group of people who know each other, visible edits discourage quietly rewriting what you said after somebody replied to it.

The diff is computed rather than stored, which costs nothing because both revisions are already in the log.

### Folded in from TKT-01M39WZY8C (Build the post history screen), archived as a duplicate on 2026-09-28

- **The route** is `/t/{id}/{slug}/{ordinal}/history` (docs/07 "URL scheme"). PR #64 left a placeholder there: the thread's title, "History of post #N", "This screen is not built yet", and a link back to the post. The placeholder keeps the slug correction, and so must the screen that replaces it. The Post footer's "history" link already points at this route.
- **The endpoint** is `GET /api/v1/threads/{id}/posts/{ordinal}/history` (TKT-01M29CA9C2). It answers `{ordinal, deleted, revisions: [{revision, actor, at, reason, source}]}`, and the client diffs two revisions' markdown sources (docs/08 "Post history").
- **Refusals** are the endpoint's:
  - an anonymous reader is `unauthenticated`;
  - a member is `forbidden` when `public_edit_history` is off;
  - a moderator can always read the history.

  Render the refusals through ErrorPanel.
- **A deleted post** answers `deleted: true` with no revisions today. The criterion below that lets a moderator see a deleted post's history needs the endpoint changed to serve its revisions to a moderator.

## Acceptance criteria

- [ ] Revisions list actor and time and the diff is word-level rather than line-level
- [ ] The members-visible case is gated on the setting and a moderator always sees it
- [ ] Grace-edited revisions are stored and visible in history even though the post shows no edited marker
- [ ] History for a deleted post is available to moderators and refused to members
- [ ] Any two revisions can be compared, and the refusals render through ErrorPanel
- [ ] The route replaces the PR #64 placeholder and keeps its slug correction
