---
schema: 3
id: TKT-01M3KB7FXCKRBV62FR6EZXCKP3
title: Stop recovered databases reusing member ids that old posts name
type: spike
status: ready
status_reason: null
priority: normal
due_on: null
labels:
  - area:db
  - area:ops
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-28T06:28:54Z
updated_at: 2026-09-28T18:06:56Z
created_by:
  id: agent:claude-code/phase1-run-438e26c1
  name: ""
updated_by:
  id: agent:claude-code/phase1-run-438e26c1
  name: ""
extensions: {}
---

## Description

Found while shipping `ketju reindex` (TKT-01M29C5Y25, Ship ketju reindex for projections).

`users.id` is `INTEGER PRIMARY KEY`, so SQLite gives a new member max(id)+1. Thread logs name authors by member id. After a recovery from a database backup older than the logs, the posts of members who joined after the backup name ids the restored database does not have, and the next members to join are given exactly those ids: old posts then show as written by a stranger. Without any backup, every id is free and every post is misattributed once people sign up again.

`ketju reindex` warns when the database has no members at all, and the README's recovery section states both cases as unsupported. Nothing prevents the reuse.

### Options to weigh

- Reindex raises the next member id above the largest author, mention, or actor id in the logs (for example by inserting and deleting a placeholder row, or by switching the column to AUTOINCREMENT and bumping `sqlite_sequence`). Posts by unknown ids then render as a deleted member rather than a new one.
- Serve refuses to start while the logs name member ids the database lacks, until an operator acknowledges it.
- Keep member ids out of reuse by construction: allocate them like thread ids (time-based), which is a schema change.

Each changes what members see and who a post is attributed to, so it is a product and security decision for a person.

## Notes

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T18:06:55Z

### Maintainer decision, 2026-09-28: "Reindex bumps the id sequence"

`ketju reindex` raises the next member id above the largest member id any thread log names: as author, mention or actor, including edit, delete and moderation records. A member who joins afterwards therefore never takes an id an old post already names.

A post whose author id the database lacks renders as a deleted member, as the anonymized placeholder does. It is never reattributed.

To make the floor persist, a migration may switch `users.id` to AUTOINCREMENT and set `sqlite_sequence`. Reindex then only ever raises it. A reindex over a database that is not behind the logs changes nothing.

Name the new behaviour in README "Recovering a lost or damaged database" and docs/04. Replace the "unsupported" wording there with what now happens.
