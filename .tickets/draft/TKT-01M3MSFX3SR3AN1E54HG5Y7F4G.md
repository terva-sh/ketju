---
schema: 3
id: TKT-01M3MSFX3SR3AN1E54HG5Y7F4G
title: Let a restored member reclaim posts under an id the database lacks
type: task
status: draft
status_reason: null
priority: low
due_on: null
labels:
  - area:db
  - area:ops
assignees: []
milestone: null
parent: null
origin: TKT-01M3KB7FXCKRBV62FR6EZXCKP3
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-28T19:57:24Z
updated_at: 2026-09-28T19:57:28Z
created_by:
  id: agent:claude-code/phase1-run-438e26c1
  name: ""
updated_by:
  id: agent:claude-code/phase1-run-438e26c1
  name: ""
extensions: {}
---

## Description

Found while working TKT-01M3KB7FXC (Stop recovered databases reusing member ids that old posts name).

After a database is restored from a backup older than the logs, a member who joined after the backup is missing from the database, together with their IdP identity. Their posts now show as a deleted member (`deleted-user-<id>`), and `ketju reindex` keeps new ids above theirs, so nobody else is given those posts. When that person signs in again, they register a new account with a new id. Nothing links the new account to the posts that name the old id, so their history stays attributed to a deleted member.

### Options to weigh

- An admin command that re-creates a member at a given missing id (`ketju admin restore-member --id N --handle H`), so the person's next sign-in can be linked to that account. It is safe only for an id the logs name and the database lacks, and it needs a way to establish that the person really is who wrote those posts.
- Leave it: the posts stay as a deleted member's, and the README says so.

Who may claim which posts is a product and security decision for a person.
