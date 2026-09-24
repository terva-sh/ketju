---
schema: 3
id: TKT-01M39788ATRB57WQ4TJ0SQ337E
title: Decide what an archived forum does to permissions
type: spike
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:db
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-24T08:07:01Z
updated_at: 2026-09-24T08:07:01Z
created_by:
  id: agent:claude-code/phase1-run-415af6aa
  name: ""
updated_by:
  id: agent:claude-code/phase1-run-415af6aa
  name: ""
extensions: {}
---

## Description

`forums.archived` exists (docs/04 "forums"), but nothing says what it does to permissions. The options are:

- The forum becomes read-only, with create_thread, reply and upload denied to everyone but admins.
- The forum becomes hidden from non-moderators.
- Nothing changes, and it is only a listing flag.

`internal/perm` (TKT-01M29C5Y2E) ignores the column for now, and docs/04 "permissions" says the question is open. This is a product decision. Once it is made, the evaluation change is small: the forum row is already read in evaluate.
