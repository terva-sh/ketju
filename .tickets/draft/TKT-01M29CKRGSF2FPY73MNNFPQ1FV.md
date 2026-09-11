---
schema: 3
id: TKT-01M29CKRGSF2FPY73MNNFPQ1FV
title: Implement thread lock, unlock, pin, and unpin
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
  - TKT-01M29BYYGNN34ZP3AMFZ540R2H
  - TKT-01M29BYYHGZY39T1F2NGJX8MED
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

Lock and unlock with a reason, pin and unpin with a forum or global scope and an optional expiry. Each is a record in the thread log and a row in audit_log.

Locking is not archiving. A locked thread accepts no posts or edits from members, but moderators may still post and their posts are styled as such. Archiving is the stronger, admin-level state.

## Acceptance criteria

- [ ] Lock and pin state survives a database rebuild because it comes from the log
- [ ] A locked thread refuses member posts and edits while allowing moderator posts
- [ ] A pin expiry is honored without a background job re-checking every thread
- [ ] Each action writes an audit_log row with the actor and reason
