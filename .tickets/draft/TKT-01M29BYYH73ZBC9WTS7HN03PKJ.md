---
schema: 3
id: TKT-01M29BYYH73ZBC9WTS7HN03PKJ
title: Add the users, identities, and sessions schema
type: task
status: draft
status_reason: null
priority: urgent
due_on: null
labels:
  - area:db
  - area:identity
  - effort:M
assignees: []
milestone: phase-1
parent: TKT-01M29BKBKV6HH8W0DN6QXQNZ0R
origin: null
dependencies:
  - TKT-01M29BW9Q17KEBMHJ91G6QHW36
blocks_on: none
references:
  - ref: design:data-model-auth
    path: docs/04-data-model.md
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

The authoritative identity tables from docs/04: `users` with a case-insensitive unique handle, `identities` unique on (provider_key, subject), `sessions` with a hashed id and a nullable user_id, and `local_credentials` for the flag that is off by default.

Two shapes here carry weight. `provider_key` refers to a config block rather than a table, so removing a provider from config makes its identities inert rather than orphaned. A session with a null user_id and a non-null identity_id is the pre-user session that approval mode needs, and the schema has to allow it from the start even though Phase 2 uses it.

## Acceptance criteria

- [ ] Handles are unique case-insensitively, proven by a test inserting two handles differing only in case
- [ ] A session row with user_id NULL and identity_id set is valid, and one with both NULL is refused
- [ ] Session ids are stored hashed and the plaintext value exists only in the cookie
- [ ] No cascading delete exists on any of these tables
