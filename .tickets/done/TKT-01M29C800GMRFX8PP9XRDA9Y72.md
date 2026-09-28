---
schema: 3
id: TKT-01M29C800GMRFX8PP9XRDA9Y72
title: Bootstrap the first admin from config and by CLI
type: task
status: done
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
  - ref: pr:97
    path: null
claim: null
archive: null
created_at: 2026-09-11T23:18:33Z
updated_at: 2026-09-28T15:01:09Z
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

- [x] A bootstrap match promotes to admin on first login and is recorded in audit_log
- [x] An email match is honored only when the provider asserts email_verified
- [x] `ketju admin promote` works against a stopped instance with the data directory only
- [x] Bootstrap promotion is idempotent and does not demote or re-promote on later logins

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

## Notes

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T05:45:10Z

Decisions taken while building (internals only, per docs/12 "Gaps the design leaves open"; written into docs/05 in the same PR):

- The idempotency marker is any `audit_log` row with `action='user.promoted'`, `target_type='user'`, `target_id=<id>`, whether the bootstrap or the CLI wrote it. The settled item says "any login where the identity matches and the user is not already an admin"; AC4 says a later login must not re-promote. Reading both, a user is promoted at a matching sign-in unless they are admin now or were ever promoted before. Considered: a marker only for bootstrap rows (lost: a CLI-promoted admin who was demoted would be re-promoted by the config), and no marker, role check only (lost: undoes every demotion at the next sign-in, which AC4 forbids).
- The role write goes through a new `perm.Evaluator.Promote`, because TestMutationsGoThroughPerm refuses `SetUserRole` outside internal/perm, and it has to share a transaction with the audit row and the marker read.
- Bootstrap runs after `IssueUser` succeeds, so only an active user who actually signed in is promoted. Considered: before issuing (lost: would promote a suspended account the config names). A failed promotion fails the sign-in and ends the session it issued. Considered: log and continue (lost: the operator is signed in as a member and has only a log line to explain it).
- `ketju admin promote` takes `--root DIR`, the flag and default `ketju fsck` already use, which is what "the data directory only" in AC3 asks for. It refuses a directory with no `db/ketju.db` rather than creating an empty database there, runs migrations as serve does, takes no lock (settled item), and exits 1 for an unknown handle, 2 for usage, 4 for a missing database or a failed write.

For TKT-01M29C8005 (open-mode registration): registration does not exist yet, so today a brand-new identity cannot finish its first sign-in at all. Registration must call `(*identity.Service).ApplyBootstrap(ctx, newUserID, id)` once it has created the user, or a configured bootstrap admin who registers is left a member until their second sign-in. AC1 is ticked because every sign-in that reaches a user today applies it; the registration path is that ticket's to wire.

Verification: `go test -race ./...` passes locally (two go/build-based layering tests need GOROOT set in this sandbox, and pass with it). Six mutations were tried and each was caught; the commit body lists them.

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T05:49:20Z

Review 1128 (run 511, head 58470fa9e437d226f0f8a017a9eb6bfba61dd81f, base 05a624537bdb1cc5863c39485ef9dbf45eda748f), gate failure. Both findings are answered in commit 03dbd6d:

- medium, "Record the once-only state for an already-admin bootstrap match" (internal/perm/mutate.go): accepted and fixed. With Once set, a user who is already admin and has no `user.promoted` row now gets one, with `"already": "admin"` in its details, and keeps the role. TestPromoteOnceRecordsAnExistingAdmin covers match, demote, and match again. docs/05 says so.
- high, "CLI promotion cannot invalidate the running server's permission cache" (cmd/ketju/admin.go): the premise is true, since another process's cache cannot be cleared. The consequence does not follow. Session resolution builds each request's Viewer from the users row (LookupSession). The role is part of the cache key, and `Check`/`CanEach` return for an active admin before the cache is read. A promoted user's next request therefore never meets an entry cached under the member role. TestPromoteFromAnotherProcess shows it: a server evaluator on its own handle, with its own generation counter and a cached deny, allows the member's next request after a second handle promotes them. A comment in admin.go and the Promote doc comment record the reasoning. Requesting a fresh review of the new head rather than a disposition.

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T15:01:09Z

Maintainer acceptance, 2026-09-28: the maintainer accepted the disposition of review 1128's high finding ("CLI promotion cannot invalidate the running server's permission cache", answered by argument and TestPromoteFromAnotherProcess), saying "PR #97 looks good to me". The re-review had marked it resolved and the PR merged before this acceptance. Since then the run's brief stops any PR whose medium-or-higher finding is answered by argument, even when a re-review clears it.

## Summary

A sign-in matching bootstrap.admin_subjects, or bootstrap.admin_emails with email_verified asserted, makes the user admin once, recorded as user.promoted in audit_log (identity.ApplyBootstrap, perm.Evaluator.Promote). ketju admin promote [--root DIR] <handle> does the same from the data directory alone. A demoted bootstrap admin stays demoted. Registration (TKT-01M29C8005) must call ApplyBootstrap for the user it creates. docs/05 records the email_verified rule, the marker, and the CLI flag.
