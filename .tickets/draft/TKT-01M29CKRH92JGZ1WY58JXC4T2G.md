---
schema: 3
id: TKT-01M29CKRH92JGZ1WY58JXC4T2G
title: Implement thread rename, retag, and move
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
  - TKT-01M29BYYGNN34ZP3AMFZ540R2H
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

Rename with a new slug, replace the tag set, and move to another forum, all as thread log records. An author may rename their own thread within a window.

A rename changes the slug, and every URL ever shared carries the old one. The slug is advisory and a stale one redirects, which is what makes renaming safe enough to be a routine moderator action.

## Acceptance criteria

- [ ] A stale slug redirects to the current one while preserving page and lens
- [ ] Tag replacement is a full set rather than an add or a remove, matching the log record
- [ ] A move respects the destination forum's permissions and is refused when the mover lacks them
- [ ] The author rename window is a site setting and expires correctly
