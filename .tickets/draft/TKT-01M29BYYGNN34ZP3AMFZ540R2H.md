---
schema: 3
id: TKT-01M29BYYGNN34ZP3AMFZ540R2H
title: Implement the record type catalogue and body codecs
type: task
status: draft
status_reason: null
priority: urgent
due_on: null
labels:
  - area:storage
  - effort:M
assignees: []
milestone: phase-0
parent: TKT-01M29BKBKP8C66GXXQK4HPEZ35
origin: null
dependencies:
  - TKT-01M29BW9PTAQ34EC78E61AHZM0
blocks_on: none
references:
  - ref: design:storage-records
    path: docs/03-thread-storage.md
claim: null
archive: null
created_at: 2026-09-11T23:13:37Z
updated_at: 2026-09-11T23:13:37Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:terva/mieli
  name: Mieli
extensions: {}
---

## Description

Every record type in the docs/03 table and its body: ThreadCreated, Post, Edit, Delete, Restore, and the thread events for rename, move, lock, unlock, pin, unpin, tag, and archive. Split and merge records are reserved here and implemented in Phase 4.

Bodies are self-describing maps so a field can be added without a format bump. An unknown field in a body is ignored rather than fatal, and an unknown record type is skipped with its frame length, which is how an older binary survives reading a newer log.

## Acceptance criteria

- [ ] Every record type in docs/03 round-trips through encode and decode with its documented body fields
- [ ] An unknown record type is skipped using the frame length and does not stop a replay
- [ ] An unknown field in a known body is ignored and the rest of the body still decodes
- [ ] Type numbers match the docs/03 table exactly, and reserved numbers for split and merge stay unused
