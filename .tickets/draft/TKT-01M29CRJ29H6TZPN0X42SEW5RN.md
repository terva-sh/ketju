---
schema: 3
id: TKT-01M29CRJ29H6TZPN0X42SEW5RN
title: Implement watches and mutes
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:views
  - effort:M
assignees: []
milestone: phase-3
parent: TKT-01M29BN36D34YVKVRPNQD3PHDV
origin: null
dependencies:
  - TKT-01M29BYYHQ7PTYNYA3VBBD4PDB
blocks_on: none
references:
  - ref: design:data-model-viewer
    path: docs/04-data-model.md
claim: null
archive: null
created_at: 2026-09-11T23:27:36Z
updated_at: 2026-09-11T23:27:36Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:terva/mieli
  name: Mieli
extensions: {}
---

## Description

Watch or mute a thread or a whole forum. A watch drives the `thread_you_watch` notification and the `/watching` list; a mute suppresses both notification and unread badge.

Precedence needs deciding here and writing down: a muted thread inside a watched forum is muted, because the more specific setting wins. Getting this backwards makes mute useless exactly when somebody needs it.

## Acceptance criteria

- [ ] Thread-level settings override forum-level settings, tested in both directions
- [ ] A muted thread produces no notification and no unread badge
- [ ] Replying to a thread opts into watching it, if that default is chosen here and documented
- [ ] Watch state appears in the thread viewer block
