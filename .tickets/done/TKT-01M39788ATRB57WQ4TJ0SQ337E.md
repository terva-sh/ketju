---
schema: 3
id: TKT-01M39788ATRB57WQ4TJ0SQ337E
title: Decide what an archived forum does to permissions
type: spike
status: done
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
updated_at: 2026-09-28T22:29:11Z
created_by:
  id: agent:claude-code/phase1-run-415af6aa
  name: ""
updated_by:
  id: agent:claude-code/phase1-run-438e26c1
  name: ""
extensions: {}
---

## Description

`forums.archived` exists (docs/04 "forums"), but nothing says what it does to permissions. The options are:

- The forum becomes read-only, with create_thread, reply and upload denied to everyone but admins.
- The forum becomes hidden from non-moderators.
- Nothing changes, and it is only a listing flag.

`internal/perm` (TKT-01M29C5Y2E) ignores the column for now, and docs/04 "permissions" says the question is open. This is a product decision. Once it is made, the evaluation change is small: the forum row is already read in evaluate.

## Notes

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T22:29:10Z

Maintainer decision, 2026-09-28: read-only. read is unchanged; create_thread, reply and upload are denied to everyone except admins, moderators included. Hidden-from-non-moderators lost because it makes archiving a delete in disguise. Listing-flag-only lost because 'archived' would promise members nothing. Written into docs/04 'permissions'. The evaluation change and the admin exemption at thread create are TKT-01M3N24RHD (Make archived forums read-only in permission evaluation).

## Summary

Decided: an archived forum is read-only, with posting, replying and uploading denied to all but admins, and read access unchanged. The rule is in docs/04; TKT-01M3N24RHD builds it.
