---
schema: 3
id: TKT-01M29CKRHSMCRM9T440314D2Q8
title: Implement author edit and delete windows with grace edits
type: task
status: draft
status_reason: null
priority: high
due_on: null
labels:
  - area:moderation
  - effort:M
assignees: []
milestone: phase-2
parent: TKT-01M29BN36JH4M1W5819D06PGPP
origin: null
dependencies:
  - TKT-01M29CA9BP9QTMN2JEDPM0YS67
blocks_on: none
references:
  - ref: design:moderation-primitives
    path: docs/08-moderation.md
claim: null
archive: null
created_at: 2026-09-11T23:24:59Z
updated_at: 2026-09-11T23:24:59Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:terva/mieli
  name: Mieli
extensions: {}
---

## Description

An author may edit their own post for 24 hours and delete it for 15 minutes, both site settings. Edits within the first 5 minutes collapse into the original revision for display, while still being stored.

Grace edits exist because fixing a typo ten seconds after posting should not brand a post as edited forever. The revision is kept either way, so the history stays complete even when the display hides it.

## Acceptance criteria

- [ ] Edit and delete windows are enforced from settings and each refusal names the expired window
- [ ] A grace edit stores its revision but does not set the visible edited marker
- [ ] After the window an edit sets the edited marker and appears in history
- [ ] A moderator is not bound by either window
