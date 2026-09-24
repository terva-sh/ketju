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
  - TKT-01M29P9RS0YMM3465BSH7AGSNJ
blocks_on: none
references:
  - ref: design:identity-principles
    path: docs/05-identity-and-accounts.md
claim: null
archive: null
created_at: 2026-09-11T23:18:33Z
updated_at: 2026-09-24T05:21:03Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude/phase1-groom
  name: ""
extensions: {}
---

## Description

The first user completing login through a provider whose subject or email matches `bootstrap.admin_subjects` or `bootstrap.admin_emails` becomes an admin. `ketju admin promote <handle>` is the alternative from the command line.

Admin bootstrap never requires a password, which is what lets the whole system ship with local passwords off by default. The matching rule needs care: an email match is only trustworthy when the provider asserts `email_verified`, and the config should say so rather than leaving an operator to discover it.

## Acceptance criteria

- [ ] A bootstrap match promotes to admin on first login and is recorded in audit_log
- [ ] An email match is honored only when the provider asserts email_verified
- [ ] `ketju admin promote` works against a stopped instance with the data directory only
- [ ] Bootstrap promotion is idempotent and does not demote or re-promote on later logins

## Implementation plan

Pre-groom plan (2026-09-24). Revise at claim, after reading the code as it stands then.

Depends on TKT-01M29P9RS0 landing first. It makes docs/05 agree on the key spelling, and plans are written against the fixed text: `bootstrap.admin_subjects` ("provider_key:sub") and `bootstrap.admin_emails` (plural). internal/config already parses and validates both spellings (config.go `Bootstrap`, validate.go `validateBootstrap`), so no config change is expected.

### Approach
1. `identity.BootstrapMatch(cfg config.Bootstrap, id *oidc.Identity) (reason string, ok bool)` is pure:
   - A subject matches when `provider_key+":"+sub` is in AdminSubjects.
   - An email matches only when `id.EmailVerified` is true and the address is in AdminEmails, compared case-insensitively on the whole address.
2. `identity.ApplyBootstrap(ctx, tx, userID, match)` runs in the login/registration transaction (TKT-01M29C7ZZ9, TKT-01M29C8005). If no `audit_log` row with `action='bootstrap.promote'` exists for this user, it sets `role='admin'` and writes that audit row with details `{via: subject|email, provider}`. The audit row is the idempotency marker, so a later demotion is not undone at the next login.
3. `ketju admin promote <handle>` in cmd/ketju: load config, open the database under DataDir, look the user up by handle case-insensitively with the sqlc query `UserByHandle`, set the role with `SetUserRole`, and write audit_log with an actor of NULL/CLI. The command needs no running server and no network.
4. Document `email_verified` in the config reference: extend the doc comment on `Bootstrap.AdminEmails` and the TOML example in docs/05.

### Files
New: internal/service/identity/bootstrap.go, cmd/ketju/admin.go (+ a `case "admin"` in main.go). Reuses `config.Bootstrap`, `oidc.Identity.EmailVerified`, and the audit_log schema from TKT-01M29BYYJ0.

### Tests
- AC1: fake IdP login with a subject match; the test asserts role=admin and one audit row.
- AC2: `BootstrapMatch` table covers verified email → match, unverified → no match, and subject → match whatever the email state. The same cases run once end to end with the fake IdP.
- AC3: a CLI test in the style of cmd/ketju/dump_test.go. Build a data directory, stop nothing because nothing runs, invoke promote, and reopen the database to check.
- AC4: log in twice and find one audit row. Demote manually, log in again, and the role stays member.

### Design
Reread docs/05 "Principles" (bootstrap bullet) and "Provider configuration", and docs/08 on the audit_log.

### Interactions
Consumes TKT-01M29BYYH7 (users), TKT-01M29BYYJ0 (audit_log), TKT-01M29BW9QF (config, done), and the login transaction from TKT-01M29C7ZZ9/TKT-01M29C8005. The role write invalidates the TKT-01M29C5Y2E permission cache: use the documented "invalidate on any group or permission write" hook.
Open: "first login" is ambiguous when the config names a user who already exists. The audit marker promotes them at their next login.
Open: whether `admin promote` must refuse while `serve` holds the data-directory lock (TKT-01M29CCBZN).

### Settled in grooming (2026-09-24)

- 'First login' means any login where the identity matches the config and the user is not already an admin. Promotion is idempotent and audited.
- `ketju admin promote` may run while serve does, because WAL and busy_timeout serialise the writes.
