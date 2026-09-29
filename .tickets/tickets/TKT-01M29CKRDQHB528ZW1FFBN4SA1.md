---
schema: 3
id: TKT-01M29CKRDQHB528ZW1FFBN4SA1
title: Add the applications and forms schema
type: task
status: ready
status_reason: null
priority: high
due_on: null
labels:
  - area:db
  - area:identity
  - effort:S
assignees: []
milestone: phase-2
parent: TKT-01M29BKBKV6HH8W0DN6QXQNZ0R
origin: null
dependencies:
  - TKT-01M29BW9Q17KEBMHJ91G6QHW36
blocks_on: none
references:
  - ref: design:data-model-applications
    path: docs/04-data-model.md
claim: null
archive: null
created_at: 2026-09-11T23:24:59Z
updated_at: 2026-09-29T07:18:12Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase2-run-orchestrator
  name: ""
extensions: {}
---

## Description

`applications` with a nullable identity_id, the form version it was answered against, the answers as JSON, and its decision fields. `forms` and `form_versions` holding the declarative schema.

Storing `form_version` on the application is what keeps an old application readable after an admin edits the form. Without it, a form edit silently rewrites the meaning of every answer already submitted.

## Acceptance criteria

- [ ] applications.(form_id, form_version) is a foreign key to form_versions, and a test shows an application naming a missing version is refused
- [ ] form_versions refuses UPDATE, DELETE and a replacing INSERT by trigger, and a test shows saving version 2 leaves version 1 byte-identical
- [ ] Application status is constrained by CHECK to pending, approved, denied and withdrawn, with the decision fields consistent with the status; each refused combination is tested
- [ ] An identity can be marked blocked while keeping user_id NULL, and triggers refuse giving a blocked identity a user or blocking one that has a user
- [ ] At most one pending application exists per identity, enforced by a partial unique index
- [ ] The migration seeds the registration form at version 1, and adds application_notes and notifications.application_id, required for exactly the two application kinds
- [ ] docs/04 names every table, column and trigger the migration adds, including why applications.identity_id is ON DELETE SET NULL
- [ ] forms.active_version is NOT NULL under a deferred foreign key to form_versions, with no trigger fallback; TestActiveVersionAlwaysExists shows that a form committed without a version, a form naming a missing version, and an update to a missing version are each refused at commit

## Implementation plan

Groomed 2026-09-29 for the Phase 2 autonomous run (TKT-01M3M9RR26), against main at 7af2c5a. The newest migration there is 0010_users_autoincrement.sql, and no table for applications, forms or blocks exists yet.

### Settled in grooming

- **Columns come from docs/04** "applications (approval mode)" and "forms, form_versions". Status is exactly `pending | approved | denied | withdrawn` (docs/04).
- **No pending user is created.** An applicant is an identity with `user_id = NULL`, and the `users` row exists only after approval (docs/05 "mode = approval" step 1). This migration writes no `users` row, and nothing in this epic writes `users.status = 'pending'`.
- **A block keeps `user_id = NULL`** and refuses re-application (docs/05 step 5).
- **Old applications stay readable after a form edit** because each application names its form version (docs/04, and this ticket's description).
- **These are authoritative tables** (the parent epic's description), so they carry foreign keys and must be backed up.
- **Registration defaults to approval** (docs/05 "Provider configuration", TKT-01M3MAS0A3). A fresh install therefore needs a registration form on its first start, so the migration seeds one.

Engineering decisions taken here, with the alternative that lost:
- **`applications.identity_id` is `ON DELETE SET NULL`.** Account linking removes identities (docs/07 `DELETE /settings/identities/{id}`), and so does anonymization (docs/05). Under 0002's NO ACTION convention, an identity that had applied could never be removed. Keeping the application with a null identity is what docs/04's "nullable" makes possible. The convention exists because users are never deleted, and identities are.
- **`applications.user_id` is added**, set on approval. It records which member an application created even after the identity is removed. Following identity → user lost, because that chain breaks on removal.
- **The block is two columns on `identities`** (`blocked_at`, `blocked_by`), guarded by triggers. A separate `identity_blocks` table lost: a block has exactly one state per identity, and the form check would pay a join for nothing.
- **Moderator notes get their own table, `application_notes`.** docs/05 step 5 shows "free-text notes from other moderators", and docs/04 has only the one `decision_note`. A JSON array on the application lost: it cannot be appended to without rewriting the row, and it cannot name an author with a foreign key.
- **`notifications.application_id` is added.** The `new_application` and `application_decided` kinds exist in the CHECK, but no column can say which application they mean (`thread_id` and `ordinal` are for posts).
- **`form_versions` is immutable by trigger**, with the same three triggers `audit_log` has in 0006 (`audit_log_no_update`, `_no_delete`, `_no_replace`). A convention would not stop an `INSERT OR REPLACE` from rewriting an old version.

### Approach

1. `internal/store/db/migrations/0011_applications_forms.sql`. Use the next free number if another Phase 2 migration lands first.
   - `forms`: `id INTEGER PRIMARY KEY`, `key TEXT NOT NULL UNIQUE` (CHECK `key GLOB '[a-z]*' AND key NOT GLOB '*[^a-z0-9_]*'`), `title TEXT NOT NULL`, `active_version INTEGER NOT NULL`, `created_at`, `updated_at`, and `FOREIGN KEY (id, active_version) REFERENCES form_versions (form_id, version) DEFERRABLE INITIALLY DEFERRED`. The foreign key is deferred because a form and its first version are inserted in one transaction. `active_version` is NOT NULL, so a form cannot be committed without a version: a form inserted alone names a version that does not exist, and the deferred check refuses it at commit. There is no trigger fallback (review 1358). sqlc v1.31.1, the pinned version, was run during grooming on 2026-09-29 against this exact `forms` and `form_versions` pair with the deferred clause, plus a join query over it. It generated code with exit 0, so the fallback path had no reason to exist. At run time `foreign_keys` is on, so the check runs at commit. During migrations, the runner turns foreign keys off and runs `foreign_key_check` before commit (`migrate.go`), which checks the seed.
   - `form_versions`: `form_id INTEGER NOT NULL REFERENCES forms (id)`, `version INTEGER NOT NULL CHECK (version >= 1)`, `schema_json TEXT NOT NULL CHECK (json_valid(schema_json))`, `created_by INTEGER REFERENCES users (id)` (null for the seed), `created_at`, and `PRIMARY KEY (form_id, version)`. Add the triggers `form_versions_no_update`, `form_versions_no_delete` and `form_versions_no_replace`, the last refusing an INSERT whose (form_id, version) already exists.
   - `applications`:
     - Columns: `id INTEGER PRIMARY KEY`, `identity_id INTEGER REFERENCES identities (id) ON DELETE SET NULL`, `form_id INTEGER NOT NULL`, `form_version INTEGER NOT NULL`, `answers_json TEXT NOT NULL CHECK (json_valid(answers_json))`, `status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','approved','denied','withdrawn'))`, `user_id INTEGER REFERENCES users (id)`, `decided_by INTEGER REFERENCES users (id)`, `decided_at INTEGER`, `decision_note TEXT`, `created_at INTEGER NOT NULL`.
     - Foreign key: `FOREIGN KEY (form_id, form_version) REFERENCES form_versions (form_id, version)`.
     - Table CHECKs: `(status = 'pending') = (decided_at IS NULL)`; `(status = 'approved') = (user_id IS NOT NULL)`; `decided_by IS NULL OR status IN ('approved','denied')`; `decision_note IS NULL OR status IN ('approved','denied')`. A withdrawal has no deciding moderator. SQLite checks a CHECK when each statement runs, not at commit. An approval must therefore create the user first, then set `status` and `user_id` in one UPDATE (TKT-01M29CP2MG, review 1344), and `TestApplicationStatusIsConstrained` shows that an UPDATE to approved without a user_id is refused.
     - Indexes: `applications_one_pending` UNIQUE on (identity_id) WHERE status = 'pending'; `applications_identity` on (identity_id, created_at); `applications_status` on (status, created_at, id) for the review list and the queue.
   - `application_notes`: `id INTEGER PRIMARY KEY`, `application_id INTEGER NOT NULL REFERENCES applications (id)`, `author_id INTEGER NOT NULL REFERENCES users (id)`, `body TEXT NOT NULL CHECK (length(body) BETWEEN 1 AND 2000)`, `created_at INTEGER NOT NULL`, and an index on (application_id, created_at).
   - `identities`: `ALTER TABLE identities ADD COLUMN blocked_at INTEGER` and `ADD COLUMN blocked_by INTEGER REFERENCES users (id)`. Two triggers enforce the rule that a blocked identity never has a user: `identities_blocked_has_no_user`, BEFORE UPDATE OF user_id, raises when NEW.blocked_at IS NOT NULL AND NEW.user_id IS NOT NULL; `identities_block_needs_no_user`, BEFORE UPDATE OF blocked_at, raises when NEW.blocked_at IS NOT NULL AND NEW.user_id IS NOT NULL; and `identities_insert_blocked`, BEFORE INSERT, raises on a row with both set.
   - `notifications`: `ALTER TABLE notifications ADD COLUMN application_id INTEGER REFERENCES applications (id)`. Add a BEFORE INSERT trigger that requires `application_id` for exactly the kinds `new_application` and `application_decided`, and add an index `notifications_application` on (application_id).
   - Seed: `forms (key 'registration', title 'Registration', active_version 1)` and `form_versions (form 1, version 1, created_by NULL)`. The version holds docs/05's example schema without the illustrative `interests` select: `name` (text, from_claim name, required), `email` (email, from_claim email, locked, not required), `who` (textarea, required, max 1000), and `rules` (checkbox, required). An admin edits it through TKT-01M29CP2P1.
2. This ticket adds no service queries. Its consumers write theirs in `internal/store/db/queries/forms.sql` (TKT-01M29CP2KE) and `applications.sql` (TKT-01M29CP2KZ, TKT-01M29CP2MG). Run `go generate ./internal/store/db` so dbq's models pick up the new tables, and commit the result.
3. docs/04: add the new columns (`applications.user_id`, `identities.blocked_at` and `blocked_by`, `notifications.application_id`), the `application_notes` table, the ON DELETE SET NULL rule and why, and the immutability triggers.

### Tests

`internal/store/db/applications_schema_test.go`, in the style of `moderation_schema_test.go`:
- `TestApplicationRecordsItsFormVersion`: an application naming a (form_id, version) that does not exist is refused; one naming an existing version is stored.
- `TestFormVersionsAreImmutable`: UPDATE, DELETE and INSERT OR REPLACE of version 1 are refused; version 2 inserts, and version 1's `schema_json` reads back byte-identical.
- `TestActiveVersionAlwaysExists`: each of these is refused at commit, and nothing from the refused transaction remains afterwards. A form inserted alone, with no version. A form whose `active_version` names a version that does not exist. An update of `registration`'s `active_version` to a missing version. A form inserted together with its version 1 in one transaction commits.
- `TestApplicationStatusIsConstrained`: 'rejected' is refused; each of the four statuses is accepted with consistent decision fields; each inconsistent pair (pending with decided_at, approved without user_id, withdrawn with decided_by, pending with a note) is refused.
- `TestOnePendingApplicationPerIdentity`: a second pending application for one identity is refused; a pending one beside a withdrawn one is accepted.
- `TestBlockedIdentityHasNoUser`: blocking an identity with user_id NULL succeeds; setting user_id on a blocked identity is refused; blocking an identity that has a user is refused.
- `TestApplicationOutlivesItsIdentity`: deleting the identity leaves the application with identity_id NULL.
- `TestApplicationNotificationsNameTheApplication`: new_application without application_id is refused; reply_to_you with an application_id is refused.
- `TestRegistrationFormIsSeeded`: after Migrate, `registration` exists at active_version 1 and its schema_json is valid JSON. TKT-01M29CP2KE's `TestSeededRegistrationFormParses` holds it to the schema rules once the parser exists.
- The existing migration tests (checksums, order, `TestFailedMigrationLeavesNothing`) and CI's sqlc regen-diff cover the rest.
- Run the repository's mutation habit over the migration: drop each CHECK, trigger and index in turn, and confirm that a test fails.

### Out of scope

- The schema's field rules and the validator: TKT-01M29CP2KE.
- Every read and write path: TKT-01M29CP2KZ (submit, status, withdraw), TKT-01M29CP2MG (review), TKT-01M29CP2N0 (notification rows).
- The pre-user session and the identity row it creates: TKT-01M29CKRG9.
- Unblocking an identity (maintainer decision 2026-09-29): TKT-01M29CP2MG. Clearing `blocked_at` and `blocked_by` passes the block triggers, so no schema change is needed.
