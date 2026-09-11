---
schema: 3
id: TKT-01M29C800GMRFX8PP9XRDA9Y72
title: Bootstrap the first admin from config and by CLI
type: task
status: draft
status_reason: null
priority: high
due_on: null
labels:
  - area:identity
  - area:ops
  - effort:S
assignees: []
milestone: phase-1
parent: TKT-01M29BKBKZW26HNHFM1YFFGVZC
origin: null
dependencies:
  - TKT-01M29BYYH73ZBC9WTS7HN03PKJ
  - TKT-01M29BW9QFZ2C7Q24KN1JW5MFW
blocks_on: none
references:
  - ref: design:identity-principles
    path: docs/05-identity-and-accounts.md
claim: null
archive: null
created_at: 2026-09-11T23:18:33Z
updated_at: 2026-09-11T23:18:33Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:terva/mieli
  name: Mieli
extensions: {}
---

## Description

The first user completing login through a provider whose subject or email matches `bootstrap.admin_subjects` or `bootstrap.admin_emails` becomes an admin. `neoforum admin promote <handle>` is the alternative from the command line.

Admin bootstrap never requires a password, which is what lets the whole system ship with local passwords off by default. The matching rule needs care: an email match is only trustworthy when the provider asserts `email_verified`, and the config should say so rather than leaving an operator to discover it.

## Acceptance criteria

- [ ] A bootstrap match promotes to admin on first login and is recorded in audit_log
- [ ] An email match is honored only when the provider asserts email_verified
- [ ] `neoforum admin promote` works against a stopped instance with the data directory only
- [ ] Bootstrap promotion is idempotent and does not demote or re-promote on later logins
