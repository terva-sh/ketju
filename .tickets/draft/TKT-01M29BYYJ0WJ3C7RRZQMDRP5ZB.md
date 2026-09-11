---
schema: 3
id: TKT-01M29BYYJ0WJ3C7RRZQMDRP5ZB
title: Add the flags, audit log, blobs, and settings schema
type: task
status: draft
status_reason: null
priority: high
due_on: null
labels:
  - area:db
  - area:moderation
  - effort:M
assignees: []
milestone: phase-1
parent: TKT-01M29BKBKV6HH8W0DN6QXQNZ0R
origin: null
dependencies:
  - TKT-01M29BW9Q17KEBMHJ91G6QHW36
blocks_on: none
references:
  - ref: design:data-model-mod
    path: docs/04-data-model.md
claim: null
archive: null
created_at: 2026-09-11T23:13:37Z
updated_at: 2026-09-11T23:32:24Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:terva/mieli
  name: Mieli
extensions: {}
---

## Description

`flags` feeding the moderator queue, `audit_log` as append-only by convention, `blobs` with refcount and quarantine status, and `settings` as the runtime-changeable key-value store.

`settings` is the other half of the config split: everything an admin changes without a restart lives here, and anything that must be readable before the database opens stays in config.toml. `audit_log` is written by every moderation action and never updated, which makes "append-only by convention" a rule worth a test rather than a comment.

## Acceptance criteria

- [ ] Flags carry the documented status set and a resolution note, and a flag is never deleted
- [ ] No code path updates or deletes an audit_log row, checked by a test over the query layer
- [ ] Blob rows carry refcount and status, and a quarantined blob is never served
- [ ] Settings values are JSON with a documented key list, and an unknown key is rejected rather than stored
