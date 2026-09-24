---
schema: 3
id: TKT-01M29BYYJ0WJ3C7RRZQMDRP5ZB
title: Add the flags, audit log, blobs, and settings schema
type: task
status: done
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
  - ref: pr:57
    path: null
claim: null
archive: null
created_at: 2026-09-11T23:13:37Z
updated_at: 2026-09-24T09:58:34Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase1-run-415af6aa
  name: ""
extensions: {}
---

## Description

`flags` feeding the moderator queue, `audit_log` as append-only by convention, `blobs` with refcount and quarantine status, and `settings` as the runtime-changeable key-value store.

`settings` is the other half of the config split: everything an admin changes without a restart lives here, and anything that must be readable before the database opens stays in config.toml. `audit_log` is written by every moderation action and never updated, which makes "append-only by convention" a rule worth a test rather than a comment.

## Acceptance criteria

- [x] Flags carry the documented status set and a resolution note, and a flag is never deleted
- [x] No code path updates or deletes an audit_log row, checked by a test over the query layer
- [x] Blob rows carry refcount and status, and a quarantined blob is never served
- [x] Settings values are JSON with a documented key list, and an unknown key is rejected rather than stored

## Implementation plan

Revised at claim, 2026-09-24. Written to the ticket after the build, not before it; the approach did not change during the build.

1. Migration 0006_moderation_blobs_settings.sql:
   - `flags`: reason and status CHECKs; `other` requires a note; open ⇔ no resolver; a BEFORE DELETE trigger raises.
   - `audit_log`: BEFORE UPDATE and BEFORE DELETE triggers raise; `details_json` must be valid JSON.
   - `blobs`: hash is 64 lowercase hex; refcount >= 0; status ok|quarantined; `unreferenced_since`, allowed only at refcount 0.
   - `settings`: `value_json` must be valid JSON; the key is non-empty.
2. Queries:
   - `moderation.sql`: CreateFlag, CloseFlag (open flags only), GetFlag, OpenFlags, OpenFlagCount, AppendAudit (the only audit write), AuditLogBefore, CreateBlob, BlobForServing (status 'ok'), SetBlobStatus, AddBlobRef, DropBlobRef (sets `unreferenced_since` at zero).
   - `settings.sql`: PutSetting, GetSetting, ListSettings.
3. `internal/settings` rather than `internal/store/db/settings.go`: the key list is policy, not storage.
   - Typed `Key[T]`, defined once each, with a validator.
   - `Get` returns the default only when there is no row.
   - `Set(name, json)` refuses unknown keys and ill-typed values, stores the canonical JSON, and appends a `setting.changed` audit row with from/to, all in one transaction.
   - `All()` lists the keys.
   - A layering test keeps PutSetting inside `internal/settings`.
4. docs/04: the key list (checked by TestKeysDocumented), plus the blobs column, flags and audit_log notes. `movedKeys` in `internal/config` is tested against the registry.

## Notes

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T09:58:10Z

Built and verified.

### Decisions (alternatives considered)
- **The package is `internal/settings`.** Rejected: `internal/store/db/settings.go`. The key list, types and defaults are policy that callers import by name (`settings.PublicEditHistory.Get`), and the db package holds storage.
- **Typed keys.** Each setting is a `Key[T]` with a default and a validator, so callers read a bool or an int, never JSON. Rejected: a `map[string]any` of defaults, which pushes a type assertion to every caller.
- **A missing row means the default.** Rejected: seeding rows in the migration. A seed fixes the defaults into the database, so changing a default later would need a migration, and a row the admin never set would look like a choice.
- **A stored value that no longer decodes is an error**, not the default: it can only have been written around `Set`.
- **null is refused explicitly.** It decodes into any T as the zero value, and 0 and false are valid for several keys.
- **The table checks only valid JSON.** The key list stays in one place, Go. Rejected: a SQL CHECK over the key names, which would be a second list for every new key. A layering test instead refuses any call to PutSetting outside `internal/settings`.
- **audit_log and flags** are append-only and never-deleted by trigger. A static test also refuses any UPDATE, DELETE or REPLACE of audit_log in `queries/*.sql` (comments stripped) and in any non-test Go string literal under `internal/`.
- **Two defaults that no document gives:**
  - `registration.mode` defaults to "approval". docs/05 calls approval the v1 mode, and it admits nobody unreviewed.
  - `site_name` defaults to "Ketju".
  - Both are admin-changeable. `invite` is refused until it is implemented, not accepted and ignored.
- **Key names:**
  - `public_edit_history` keeps its name from docs/08 and TKT-01M29CA9C2.
  - Keys with no documented name got dotted names (`posts.*`, `flags.*`, `slow_mode.default_seconds`) that match movedKeys' prefixes.
  - Agent token-bucket keys are left to Phase 2, with agent accounts; docs/08 gives no defaults for them.
- **CreateBlob takes `unreferenced_since`**: a new blob starts at refcount 0, and the uploader passes the upload time.

### Tests and mutation
- Tests:
  - TestFlagStatusAndNoDelete, TestOpenFlagQueries (AC1).
  - TestAuditLogAppendOnly (AC2, both layers).
  - TestQuarantinedBlobNotServable (AC3, with refcount and unreferenced_since).
  - TestSettingsTableWantsJSON.
  - TestSettingsUnknownKeyRejected (AC4: unknown and ill-typed values store nothing).
  - TestSettingsRoundTrip (canonical storage, audit from/to and actor).
  - TestStoredValueThatNoLongerDecodes, TestGetFailureIsNotTheDefault, TestKeysDocumented, TestAllIsSorted, TestWritesGoThroughSet.
  - TestMovedKeysAreSettings in `internal/config`.
- 33 mutants across the migration, the queries and the package, all killed. Two needed new tests: null for a key whose zero value is valid, and Get on a failing database. One needed a stronger one: canonical storage of a whitespace-padded value.

## Summary

Migration 0006 adds flags (never deleted, by trigger), audit_log (append-only, by trigger and a static test), blobs (refcount, status, unreferenced_since; BlobForServing skips quarantined blobs) and settings (valid JSON). internal/settings holds the closed, typed key list with defaults, and Set refuses unknown keys and ill-typed values and writes an audit row in the same transaction. docs/04 lists the keys, checked by a test.
