---
schema: 3
id: TKT-01M29CP2R48HV8ZW0QVRK2JM6N
title: Build the audit log query and admin view
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
  - TKT-01M29BYYJ0WJ3C7RRZQMDRP5ZB
blocks_on: none
references:
  - ref: design:moderation
    path: docs/08-moderation.md
claim: null
archive: null
created_at: 2026-09-11T23:26:15Z
updated_at: 2026-09-11T23:26:15Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:terva/mieli
  name: Mieli
extensions: {}
---

## Description

A filterable view over `audit_log` by actor, action, target, and date, and the query API behind it.

The log is only worth writing if somebody can read it. In a small community the common question is "who moved this thread and why", asked by a member who is annoyed, and an admin who can answer it in ten seconds defuses something that otherwise festers.

## Acceptance criteria

- [ ] Filtering by actor, action, target, and date range works and is paged
- [ ] Entries link to the thread, post, or user they concern
- [ ] The view is admin-only and a moderator sees only their own scope or nothing, decided and documented here
- [ ] No code path in the application updates or deletes an entry
