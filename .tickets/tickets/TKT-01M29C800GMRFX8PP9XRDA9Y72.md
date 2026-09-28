---
schema: 3
id: TKT-01M29C800GMRFX8PP9XRDA9Y72
title: Bootstrap the first admin from config and by CLI
type: task
status: in-progress
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
claim:
  actor: agent:claude-code/phase1-run-438e26c1
  branch: run/c800g-bootstrap-admin
  worktree: /home/sothr/workspace/git.local.sothr.com/terva-sh/ketju/.claude/worktrees/agent-accbfc12563040fba
  commit: 0233673650a9a633cfab3419554002d601c4bb7f
  session: null
  claimed_at: 2026-09-28T05:38:42Z
  expires_at: null
archive: null
created_at: 2026-09-11T23:18:33Z
updated_at: 2026-09-28T05:39:04Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase1-run-438e26c1
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

Revised at claim (2026-09-28) against main at 0233673.

The design fixes both operator-facing shapes: docs/05 "Principles" names `bootstrap.admin_subjects` ("provider_key:subject") and `bootstrap.admin_emails`, and `ketju admin promote <handle>`. internal/config already parses and validates both keys. No new config key is added. The CLI takes `--root DIR` for the data directory, the flag `ketju fsck` already uses, so "the data directory only" (AC3) needs no config file.

### Approach
1. `identity.BootstrapMatch(cfg config.Bootstrap, id *oidc.Identity) (Match, bool)` is pure. A subject matches when `provider+":"+sub` is in AdminSubjects. An email matches only when `id.EmailVerified` is true and the address equals an AdminEmails entry, compared case-insensitively on the whole address. A subject match wins over an email match. An address that is listed but unverified is reported separately so the login can log a warning naming why nothing happened.
2. `perm.Evaluator.Promote(ctx, userID, Promotion)` makes a user admin and appends one `audit_log` row (`action='user.promoted'`, `target_type='user'`, `target_id=<id>`, details JSON with `role`, `via`, and for bootstrap `match` and `provider`) in one write transaction, then invalidates the permission cache. The role write must live in internal/perm: TestMutationsGoThroughPerm refuses a `SetUserRole` call anywhere else. It does nothing for a user who is already admin. With `Once` set (bootstrap), it also does nothing when any `user.promoted` row for the user exists. That row is the idempotency marker, so a later demotion is not undone at the next login (AC4). A new sqlc query `AuditExists(action, target_type, target_id)` reads it through the `audit_log_target` index.
3. `identity.Service` gains `Bootstrap config.Bootstrap` and `Perm *perm.Evaluator`. `Login` applies the bootstrap after the session is issued and the replaced session ended, so only an active user who actually signed in is promoted. A failed promotion fails the sign-in and ends the session it issued, as a failed revoke does, so the operator retries instead of being signed in as a member with the failure buried in a log. serve.go wires `cfg.Bootstrap` and the evaluator.
4. `ketju admin promote [--root DIR] <handle>` in cmd/ketju/admin.go. It refuses a directory with no `db/ketju.db` rather than creating one, opens the database, migrates it as serve would, finds the user by handle (the column is NOCASE), and calls `perm.Promote` with no actor (actor_id NULL) and `via=cli`. An already-admin user is reported and left alone, exit 0. An unknown handle exits 1 with a message. It takes no lock, per the settled item below.
5. Registration (TKT-01M29C8005) is not built yet, so a brand-new identity cannot finish a first login today. The bootstrap hook is an exported function that registration must call once it creates the user. The note on this ticket says so.
6. Document `email_verified` on `Bootstrap.AdminEmails` and in the docs/05 TOML example.

### Tests
- AC1/AC4 end to end in internal/web/auth with the fake IdP: a subject match signs in as admin with one audit row; a second sign-in writes no second row; a demoted user stays member at the next sign-in; a non-matching user stays member.
- AC2: a `BootstrapMatch` table (verified email, unverified email, case-folded email, subject regardless of email state, wrong provider prefix, empty config), plus one end-to-end sign-in with an unverified listed email that stays member.
- AC3: cmd/ketju/admin_test.go builds a data directory, promotes by handle in a different case, reopens the database and checks role and audit row; also unknown handle, a directory with no database (nothing created), already admin, and usage errors.
- perm: Promote table for once/not-once, already admin, prior marker, and cache invalidation.

### Settled in grooming (2026-09-24)

- 'First login' means any login where the identity matches the config and the user is not already an admin. Promotion is idempotent and audited.
- `ketju admin promote` may run while serve does, because WAL and busy_timeout serialise the writes.
