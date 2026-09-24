---
schema: 3
id: TKT-01M29BYYH73ZBC9WTS7HN03PKJ
title: Add the users, identities, and sessions schema
type: task
status: done
status_reason: null
priority: urgent
due_on: null
labels:
  - area:db
  - area:identity
  - effort:M
assignees: []
milestone: phase-1
parent: TKT-01M29BKBKV6HH8W0DN6QXQNZ0R
origin: null
dependencies:
  - TKT-01M29BW9Q17KEBMHJ91G6QHW36
blocks_on: none
references:
  - ref: design:data-model-auth
    path: docs/04-data-model.md
claim: null
archive: null
created_at: 2026-09-11T23:13:37Z
updated_at: 2026-09-24T07:27:11Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase1-run-415af6aa
  name: ""
extensions: {}
---

## Description

The authoritative identity tables from docs/04: `users` with a case-insensitive unique handle, `identities` unique on (provider_key, subject), `sessions` with a hashed id and a nullable user_id, and `local_credentials` for the flag that is off by default.

Two shapes here carry weight. `provider_key` refers to a config block rather than a table, so removing a provider from config makes its identities inert rather than orphaned. A session with a null user_id and a non-null identity_id is the pre-user session that approval mode needs, and the schema has to allow it from the start even though Phase 2 uses it.

## Acceptance criteria

- [x] Handles are unique case-insensitively, proven by a test inserting two handles differing only in case
- [x] A session row with user_id NULL and identity_id set is valid, and one with both NULL is refused
- [x] Session ids are stored hashed and the plaintext value exists only in the cookie
- [x] No cascading delete exists on any of these tables

## Implementation plan

Pre-groom plan (2026-09-24). Revise at claim, after reading the code as it stands then.

### Approach
1. Add migration `NNNN_identity.sql` with the tables `users`, `identities`, `local_credentials`, and `sessions`, using the column lists in docs/04. Ids are INTEGER. Timestamps are unix-ms INTEGER. `created_at` is on every table and `updated_at` is on mutable ones. Use CHECK constraints for the enums: `users.status`, `users.role`, `users.kind`, and `sessions.kind`.
2. Handle uniqueness uses `handle TEXT NOT NULL COLLATE NOCASE UNIQUE`. NOCASE folds only ASCII. If handles may be non-ASCII, add a `handle_key` column instead, normalised in Go and UNIQUE. Neither docs/05 nor any ticket defines the handle alphabet, so decide this at claim with the registration ticket (TKT-01M29C8005).
3. Add `UNIQUE (provider_key, subject)` on `identities`. `provider_key` is plain TEXT with no foreign key, per docs/04: removing a provider leaves its identities inert rather than orphaned.
4. `sessions.id_hash BLOB PRIMARY KEY CHECK (length(id_hash) = 32)`. There is no column that could hold a plaintext id. Add `CHECK ((kind = 'user' AND user_id IS NOT NULL) OR (kind = 'pre_user' AND user_id IS NULL AND identity_id IS NOT NULL))`. That makes both-NULL fail, and it also refuses a pre_user row that carries a user_id.
5. Foreign keys (`identities.user_id`, `sessions.user_id` and `identity_id`, `local_credentials.user_id`) are declared with no ON DELETE clause, which gives SQLite's default NO ACTION. No CASCADE anywhere.
6. Query layer: `queries/identity.sql` for sqlc. A sqlc type override maps `sessions.id_hash` to `db.SessionHash [32]byte`, so every generated session query takes that type and none takes a string. Queries: insert, get by hash, touch last_used_at, delete by hash, and delete by user. Hashing plaintext to SessionHash belongs to the session store (TKT-01M29C5Y3E), not here.
7. Add schema comments stating that `users` is authoritative and must be backed up.

### Files and packages
New: `internal/store/db/migrations/NNNN_identity.sql`, `internal/store/db/queries/identity.sql`, the regenerated `dbq/`, and `identity_schema_test.go`. Reuses `db.DB.Write` and `db.DB.Read` from BW9Q1.

### Tests
- AC1: `TestHandleUniqueIgnoresCase` inserts "Drew" and then "drew". The second insert must fail on a UNIQUE constraint.
- AC2: a table-driven `TestSessionUserIdentityShape` covers pre_user with identity only (ok), both NULL (refused), user with user_id (ok), and pre_user with user_id (refused).
- AC3: `TestSessionIDColumnIsHashOnly` asserts that a 31-byte or 44-char base64 value is refused by the CHECK, and that the query API has no string-keyed lookup. This is a compile-level fact noted in the test. The end-to-end "dump contains nothing usable" test is the session store's (C5Y3E AC1).
- AC4: `TestNoCascadingDeletes` iterates `sqlite_schema` and asserts that `pragma_foreign_key_list(t).on_delete` is never CASCADE or SET NULL. Write it generically, over every table, so it also guards later migrations.

### Design
docs/04-data-model.md "users", "identities", "local_credentials", "sessions", and "Conventions". docs/05-identity-and-accounts.md "Sessions and CSRF" and "mode = approval".

### Interactions
Defines the rows consumed by TKT-01M29C5Y3E (session store), TKT-01M29C8005 (registration), TKT-01M29C800G (bootstrap admin), TKT-01M29CKRFS (lifecycle), and C5Y2S (the sweeper, which deletes expired sessions). `oidc_refresh_token_enc` is a nullable BLOB here. Its encryption key is identity-lane business.

### Settled in grooming (2026-09-24)

- Handles: 3-30 characters from a-z, 0-9, _ and -; the first character is a letter or digit; stored lowercased (docs/04, decided 2026-09-24). Enforce it with a CHECK constraint and in identity.ValidateHandle.
- `users.mark_all_read_at` belongs to this ticket. Add it to the docs/04 users column list.

## Notes

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T07:27:10Z

### Built, 2026-09-24, against main at 1c605e5

Migration `0002_identity.sql` adds `users`, `identities`, `local_credentials` and `sessions`. Queries are in `queries/identity.sql`.

Changes from the pre-groom plan:
- **The handle rule is in the schema and in Go.** The CHECK is length 3–30, `NOT GLOB '*[^a-z0-9_-]*'`, and a first character in `[a-z0-9]`. GLOB is case-sensitive, so the character class already refuses uppercase. A separate `handle = lower(handle)` clause was tried and removed: a mutation test showed it was redundant. The Go rule is **`domain.ValidateHandle`** in a new `internal/domain`, which docs/02 gives to pure types and rules. The grooming note said `identity.ValidateHandle`, but no identity package exists outside the future service layer; this is a package-boundary call. The shared table `domain.HandleCases` runs against both the Go rule and the CHECK (`TestHandleRuleMatchesTheSchema`), so they cannot drift.
- **SessionHash lives in `internal/store/db/sqltypes`**, not in package `db`. The generated dbq imports it and db imports dbq, so `db.SessionHash` would be an import cycle. A sqlc override maps `sessions.id_hash` to it. It is a `[32]byte` with Value and Scan, so no generated session query accepts a string.
- `identities.user_id` is nullable. An approval-mode applicant has an identity before an account, and a pre_user session points at that identity.
- `sessions` has no `updated_at`, because `last_used_at` is its only mutable column; the schema comment says so. Indexes are on `sessions(user_id)` for "log out everywhere" and `sessions(expires_at)` for the sweeper.
- docs/04 `users` now lists `mark_all_read_at` and `updated_at`.
- The migration-runner tests now use `openUnmigrated`. They had silently assumed the embedded set was only 0001, and this ticket's 0002 exposed that.

### Mutations tried
- No COLLATE NOCASE: caught by TestHandleUniqueIgnoresCase.
- A pre_user session that needs no identity: caught by TestSessionUserIdentityShape.
- id_hash accepting 32 characters of text: caught by TestSessionIDColumnIsHashOnly.
- ON DELETE CASCADE on sessions.user_id: caught by TestNoCascadingDeletes, both the schema scan and the enforced FOREIGN KEY failure.
- No `handle = lower(handle)`: survived, and was an equivalent mutation (see above), so the clause was removed.

The flaky threadlog test (TKT-01M393S6P7) failed once more in a full local suite run, in a package this change does not touch.

## Summary

Migration 0002_identity adds users, identities, local_credentials and sessions per docs/04, with CHECKs on every enum. The handle rule (3 to 30 of a-z 0-9 _ -, starting alphanumeric) is a CHECK plus a NOCASE UNIQUE index, and domain.ValidateHandle in Go; a shared table proves they agree. A session is 'user' with a user_id, or 'pre_user' with an identity_id and no user_id; both-NULL is refused. id_hash accepts only a 32-byte BLOB and is typed sqltypes.SessionHash in every generated query, so the plaintext id has nowhere to go. Every foreign key is NO ACTION, and TestNoCascadingDeletes scans every table, so it also guards later migrations. docs/04 lists mark_all_read_at and updated_at.
