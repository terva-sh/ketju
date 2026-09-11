---
schema: 3
id: TKT-01M29CRJ4E4AZ079KEHVGJ01DS
title: Build the moderator queue
type: task
status: draft
status_reason: null
priority: high
due_on: null
labels:
  - area:moderation
  - effort:L
assignees: []
milestone: phase-2
parent: TKT-01M29BN36JH4M1W5819D06PGPP
origin: null
dependencies:
  - TKT-01M29CP2PJ23SK6AN8M0CJY4JT
  - TKT-01M29CP2MGC6FD35PJHR2Y2WNA
blocks_on: none
references:
  - ref: design:flags
    path: docs/08-moderation.md
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

`/admin/queue`: open flags and pending applications in one list, newest first, with counts in the moderator navigation.

One list rather than two is the point. A moderator checking in wants a single place that is either empty or not, and splitting the queue means one half gets ignored.

Actions resolve in place, and a resolved item leaves the queue while staying in the record.

## Acceptance criteria

- [ ] Flags and applications appear in one list ordered newest first
- [ ] Resolving an item removes it from the queue and keeps it queryable afterwards
- [ ] Queue counts appear as a badge in the moderator navigation and match the list
- [ ] Two moderators acting on the same item produce one outcome and a clear message for the second
