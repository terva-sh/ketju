---
schema: 3
id: TKT-01M29CXK19AFDY0WTH5MZAZVP5
title: Build the admin forum management screens
type: task
status: draft
status_reason: null
priority: high
due_on: null
labels:
  - area:moderation
  - area:client
  - effort:M
assignees: []
milestone: phase-2
parent: TKT-01M29BN36JH4M1W5819D06PGPP
origin: null
dependencies:
  - TKT-01M29BYYHGZY39T1F2NGJX8MED
  - TKT-01M29CEE4KNQBFP49FVSCRNZA4
blocks_on: none
references:
  - ref: design:data-model-perms
    path: docs/04-data-model.md
claim: null
archive: null
created_at: 2026-09-11T23:30:21Z
updated_at: 2026-09-11T23:30:21Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:terva/mieli
  name: Mieli
extensions: {}
---

## Description

Create, edit, reorder, and archive forums, including the one level of nesting and the visibility setting.

Reordering is the fiddly part and the one an admin does most, since forum order is how a community signals what it is about. Positions need to survive an insert in the middle without a renumbering that reorders everything else.

Archiving a forum must not orphan its threads, and what happens to them needs deciding here rather than discovering later.

## Acceptance criteria

- [ ] Forums can be created, renamed, reordered, and archived, with nesting limited to one level
- [ ] Reordering is stable and does not disturb unrelated positions
- [ ] Changing visibility takes effect within the permission cache lifetime and the UI says so
- [ ] Archiving a forum has a defined, documented effect on its threads
