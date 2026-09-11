---
schema: 3
id: TKT-01M29CKRFSDCGKH605X8Y4RT3C
title: "Implement the account lifecycle: deactivate, suspend, anonymize"
type: task
status: draft
status_reason: null
priority: high
due_on: null
labels:
  - area:identity
  - area:moderation
  - effort:M
assignees: []
milestone: phase-2
parent: TKT-01M29BKBKZW26HNHFM1YFFGVZC
origin: null
dependencies:
  - TKT-01M29BYYH73ZBC9WTS7HN03PKJ
  - TKT-01M29C5Y3E5SKHFR2SHJ8Y60TP
blocks_on: none
references:
  - ref: design:account-lifecycle
    path: docs/05-identity-and-accounts.md
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

Deactivate by self: sessions revoked, identities kept, posts kept, and the user can return through any linked identity. Suspend by a moderator: the same plus a reason shown at login and a duration. Anonymize by an admin: handle becomes `deleted-user-<id>`, display name and avatar cleared, identities removed, and attribution suppressed at render time by a flag.

Anonymize does not edit post bodies, because they are in append-only logs. Erasing body text is the offline redaction tool, and the two must not be confused when somebody arrives with a legal request.

## Acceptance criteria

- [ ] Each transition revokes sessions immediately and the next request is signed out
- [ ] A suspended user sees the reason and the duration at login
- [ ] Anonymize is irreversible, recorded in audit_log, and leaves post bodies untouched
- [ ] An anonymized user's attribution is suppressed everywhere it was rendered, including old quotes
