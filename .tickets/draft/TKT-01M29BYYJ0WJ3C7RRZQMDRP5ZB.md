---
schema: 3
id: TKT-01M29BYYJ0WJ3C7RRZQMDRP5ZB
title: Add the flags, audit log, blobs, and settings schema
type: task
status: draft
status_reason: null
priority: high
due_on: null
labels:
  - area:db
  - area:moderation
  - effort:M
assignees: []
milestone: phase-1
parent: TKT-01M29BKBKV6HH8W0DN6QXQNZ0R
origin: null
dependencies:
  - TKT-01M29BW9Q17KEBMHJ91G6QHW36
blocks_on: none
references:
  - ref: design:data-model-mod
    path: docs/04-data-model.md
claim: null
archive: null
created_at: 2026-09-11T23:13:37Z
updated_at: 2026-09-24T05:21:02Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude/phase1-groom
  name: ""
extensions: {}
---

## Description

`flags` feeding the moderator queue, `audit_log` as append-only by convention, `blobs` with refcount and quarantine status, and `settings` as the runtime-changeable key-value store.

`settings` is the other half of the config split: everything an admin changes without a restart lives here, and anything that must be readable before the database opens stays in config.toml. `audit_log` is written by every moderation action and never updated, which makes "append-only by convention" a rule worth a test rather than a comment.

## Acceptance criteria

- [ ] Flags carry the documented status set and a resolution note, and a flag is never deleted
- [ ] No code path updates or deletes an audit_log row, checked by a test over the query layer
- [ ] Blob rows carry refcount and status, and a quarantined blob is never served
- [ ] Settings values are JSON with a documented key list, and an unknown key is rejected rather than stored

## Implementation plan

Pre-groom plan (2026-09-24). Revise at claim, after reading the code as it stands then.

### Approach
1. Add migration `NNNN_moderation_blobs_settings.sql` with the tables `flags`, `audit_log`, `blobs`, and `settings`, using the columns in docs/04. docs/08 "Flags" defines the same flags columns.
2. `flags.status` gets `CHECK (status IN ('open','resolved','dismissed'))` and `flags.reason` gets `CHECK (reason IN ('spam','abuse','off-topic','other'))` (docs/08). A `BEFORE DELETE ON flags` trigger raises, so no flag is ever deleted. Resolution fields update in place.
3. `audit_log`: `BEFORE UPDATE` and `BEFORE DELETE` triggers raise. That makes append-only a schema property, not only a convention. The query layer exposes only `AppendAudit` and read queries.
4. `blobs(hash PK, size, mime, original_name, uploaded_by, uploaded_at, refcount CHECK (refcount >= 0), status CHECK (status IN ('ok','quarantined')))`. Add `unreferenced_since INTEGER NULL`, set when refcount reaches 0 and cleared when it rises. docs/04 lacks this column, but blob GC (C5Y2S) needs it to implement "seven days at zero" rather than "seven days since upload". Update docs/04 "blobs" in the same commit. Add a serving query, `BlobForServing(hash)`, with `WHERE status = 'ok'`. It is the only lookup the serve path may use.
5. Settings: a Go registry `settings.Keys` maps each key to a spec: a JSON schema-like validator, a default, and a doc line. `Set(ctx, key, value, actor)` refuses unknown keys and invalid values, and writes an audit row. The initial key list comes from the docs. docs/05 gives `registration.mode` and `registration.allow_local_passwords`, docs/04 gives `site_name` and slow-mode defaults, and docs/08 gives the edit window, delete window, grace edits, flag auto-hide threshold, and agent bucket defaults. Write the list into docs/04 "settings" as the documented key list. Reuse the same names as `movedKeys` in `internal/config/problem.go`, and make a test assert that every movedKeys entry is a registered settings key.

### Files and packages
New: the migration, `internal/settings/` (registry, Get, Set; or `internal/store/db/settings.go`, choose at claim), `queries/moderation.sql` and `queries/settings.sql`, the regenerated `dbq/`, `moderation_schema_test.go`, `settings_test.go`. Touched: docs/04 (settings key list, blobs column). Reuses `DB.Write` from BW9Q1.

### Tests
- AC1: `TestFlagStatusAndNoDelete` checks that the three statuses insert, that `'closed'` is refused, that DELETE is refused, and that a resolution_note round-trips.
- AC2: `TestAuditLogAppendOnly` has two layers. The schema layer checks that UPDATE and DELETE raise. The query-layer check is a static test. It reads every file in `internal/store/db/queries/` and every Go string literal in `internal/store/db/views/`, and asserts that no statement matches `UPDATE audit_log` or `DELETE FROM audit_log`, case-insensitively.
- AC3: `TestQuarantinedBlobNotServable` checks that `BlobForServing` returns not-found for a quarantined row, and that `refcount = -1` is refused.
- AC4: `TestSettingsUnknownKeyRejected` checks that an unknown key errors and stores nothing. It also checks that a known key with an ill-typed value errors, and that a value round-trips as JSON.

### Design
docs/04-data-model.md "flags", "audit_log", "blobs", "settings". docs/08-moderation.md "Primitives (v1)" (slow mode lives in `settings` per thread), "Flags (reports)", "Rate limits". docs/02-architecture.md "Configuration".

### Interactions
Consumed by C5Y2S (blob GC, which needs `unreferenced_since`), TKT-01M29CXK0 (blob store, Phase 3), TKT-01M29CP2PJ (flags), TKT-01M29CP2R (audit view), and TKT-01M29CXK2 (settings screen).

### Open items
docs/08 puts slow mode "per thread" in `settings`. The settings key shape for a per-thread value, such as `slow_mode.thread.<id>`, is not specified. The docs/04 settings table has no thread column.

### Settled in grooming (2026-09-24)

- Write the settings key list into docs/04 in this PR.
- Per-thread slow mode is Phase 2 (TKT-01M29CRJ50). Its default is off (TKT-01M29P9RS0).
- Add `blobs.unreferenced_since` for C5Y2S.
