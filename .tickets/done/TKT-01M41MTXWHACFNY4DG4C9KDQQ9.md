---
schema: 3
id: TKT-01M41MTXWHACFNY4DG4C9KDQQ9
title: Keep truncation detection on the first start after migration 0025
type: bug
status: done
status_reason: null
priority: urgent
due_on: null
labels:
  - area:storage
assignees: []
milestone: phase-4
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-03T19:46:10Z
updated_at: 2026-10-03T20:21:18Z
created_by:
  id: agent:claude-code/phase4-run-orchestrator
  name: ""
updated_by:
  id: agent:claude-code/phase4-run-m0025
  name: ""
extensions: {}
---

## Description

Migration 0025 (`0025_post_quotes.sql`, PR #380, TKT-01M3YME9AZ) backfills quote backlinks with `UPDATE projection_state SET log_length = 64 WHERE log_length > 64`. The first start after upgrading then replays every log over the existing rows.

That rewind also destroys the evidence `CatchUp` uses to tell a damaged log from one that is merely behind. `Classify(logLen, projected)` (`internal/events/projection/classify.go`) returns `ClassCorrupt` when the file is shorter than the projected length and quarantines the thread. With every projected length at 64:
- **A log truncated before the upgrade**, at a complete-frame boundary above its header, classifies as drift and is replayed as the new truth. `post_meta` rows beyond the cut remain, and the shorter length is recorded as current.
- **A log truncated to exactly its header** classifies as current, and its old rows stay in use.

This is Terva review 2074 (high), on review-only PR #406, which the maintainer asked for on 2026-10-03 in the v1.0 checklist's part 2. The orchestrator confirmed it against `classify.go` and `catchup.go`. The maintainer chose to fix it before v1.0.

**0025 is unreleased.** The dogfood instance is at schema 19 (read-only check, 2026-10-03) and has never run it. So the fix may change 0025 in place, if no guard forbids editing a migration. Otherwise it adds a migration that undoes the old rewind's effect, with a `db.Pending` path for a database that already ran it. The plan says which, and why.

The same review could not judge several paths, because their code was not in its diff. They need tests here:
- blob reference counts, where a replay that adds instead of setting would double them;
- attachment and thumbnail triggers;
- the audit lookup;
- notification upserts;
- the separate search catch-up.

## Acceptance criteria

- [x] A thread log truncated before the upgrade, at a frame boundary above its header or to exactly its header, is classified corrupt and quarantined on the first start after the backlinks migration, as it would be on any other start (an upgrade test from a pre-0025 fixture shows it)
- [x] Every intact log still gets its quote backlinks backfilled on that first start, and the backfill runs once
- [x] Replaying every log over an existing database leaves every projection table, blobs.refcount, audit_log, notifications and the search index equal to a fresh ketju reindex of the same logs (a test compares them table by table)
- [x] The migration's comment and docs/03 or docs/04 say how the backfill keeps the truncation check, and the v1.0 checklist's part 2 records the outcome

## Implementation plan

### Route: a new migration, 0030, and 0025 left as it ran

0025 is not edited. `internal/store/db/migrate.go` records the sha256 of each migration's text in `schema_migrations`, and `applyMigrations` refuses a database whose recorded migration no longer matches the embedded one ("a migration was edited or renumbered after it ran", `ErrMigrations`). `TestMigrateRefusesChecksumDrift` holds that. No test forbids changing the file in the tree, but the guard forbids it for every database that has run it. 0025 has been on main since #380 (bb6faf74), and 0026 to 0029 have landed after it, so any database a build of main has migrated since then would refuse to start with an edited 0025, and the only way out would be editing `schema_migrations` by hand. The dogfood instance is at schema 19 and is not affected either way. A new migration works for both kinds of database, so it wins.

### Design

1. **Migration 0030 (`0030_projection_backfill.sql`).** Adds `projection_state.backfill INTEGER NOT NULL DEFAULT 0 CHECK (backfill IN (0, 1))`. For every row at 64, which only 0025's rewind leaves (the writer records a length only after a log's first frame), it sets `backfill = 1` and gives the length back from `threads.log_length_projected`, which the writer sets in the same transaction as `projection_state` and 0025 did not touch. A row whose threads row is missing or no longer than its header keeps 64 and is still replayed.
2. **The writer (`ProjectThread`).** A row with `backfill = 1` is replayed from the end of the file header, over the rows already there, to `max(upTo, log_length)`, in one transaction. `SetProjectionState` clears `backfill` in the statement that records the length, so the flag and the length always move together. A log shorter than the recorded length fails as a log shorter than an event's length does, so the replay never records a length below the one it found. Every path pays an owed backfill: catch-up, an event, `Retry`, the notifier's and the thread service's `Project`.
3. **Catch-up (`CatchUp`).** Reads `backfill` with the lengths. A thread is classified against its real projected length first, exactly as before, so a log cut at a frame boundary above its header, or to its header, is corrupt and quarantined, and keeps its backfill and its rows. Any other thread owing a backfill is reopened (for its real length after a torn tail) and replayed, gap or not. `Report.Backfilled` counts them, and the catch-up log line and `ketju started` carry it.
4. **The notifier (`process`).** A thread whose backfill is still owed (its replay failed) is not read at all: its post_quotes rows may be missing, and reading them would lose a `quoted` for good. It stays in the generator's behind set until the projection pays the backfill. The old rewind did the same by making the projection look behind.
5. **A database that already ran the old 0025.** Rows no catch-up replayed since are still at 64 and get their length back from `threads.log_length_projected`, so detection is whole for them. A row a catch-up already replayed holds the length of the log it replayed and has its backlinks; nothing records what it held before, so a log cut before that replay is not found, and 0030 leaves it alone (no backfill, nothing to redo). Only a database that ran a main build between #380 and this change can be in that state; `ketju reindex` rebuilds it. The migration comment and docs/04 say so. The path for a database that already ran 0025 is 0030 itself: `db.Pending` counts it like any other pending migration, `ketju backup` copies such a database at its old schema, and serve applies 0030 before its catch-up.

### Alternatives considered

- **Edit 0025 in place** (no rewind, set the flag). Lost to the checksum guard above.
- **Classify against `threads.log_length_projected` in catch-up** (review 2074's second suggestion). Two lengths for one rule: `Classify`, `ketju fsck` and `Retry` would each need to know which to trust, and `projection_state` would stay wrong for anything else that reads it. Restoring the length once, in the migration, from that same column keeps `Classify` the one rule (TKT-01M3A054TR).
- **A separate validation watermark column** (review 2074's first suggestion). It holds the same evidence as the restored `log_length`, and every reader of the length would have to take both. The flag only says "replay me"; the length keeps its meaning.
- **A separate backfill table** instead of a column. A column is cleared by the statement that records the length and goes with its row on `ClearProjections`; a table needs its own clear, its own `ProjectionTables` entry and its own transaction discipline.
- **Fill post_quotes in the migration.** Quotes come from rendering the source (`render.QuotedOrdinals`), which SQL cannot do, and the runner runs SQL only.
- **Ask operators to run `ketju reindex` after upgrading.** An upgrade step people skip; the README procedure would grow a step for one release.

### What the unjudged paths do (review 2074's scope note)

- **blobs.refcount**: the projection never writes `blobs`. References are counted by `blobs.Service.AddRefs` before the append (`service/threads/create.go`); there is no trigger on `post_attachments`. A replay leaves the counts alone.
- **Attachment and thumbnail triggers**: none exist. The migrations' triggers are on forums, groups, group_members, flags, audit_log, form_versions, users, identities, applications, notifications and drafts, plus the count triggers on post_meta and threads. The `post_meta` and `threads` count triggers feed `forum_thread_counts` and `live_post_count`, which are projections and are compared.
- **Audit lookup**: `writeAudit` asks `AuditExistsForRecord` (target, action, log offset) before inserting.
- **Notification upserts**: the projection writes none; `notification_progress` is not moved, so the generator does not repeat itself.
- **Search catch-up**: keeps its own `search_state` and reads the logs and indexes, not the projection; a projection replay does not touch it.

The third criterion's test confirms each of these against a world written through the real serve. If it had found a non-idempotent apply, that would be fixed here; it found none.

### Tests

- Criterion 1: `cmd/ketju` `TestUpgradeKeepsTruncationDetection` (the ed82f15e fixture at 0024, two logs cut before the upgrade, one at a frame boundary above its header and one to its header; the first start quarantines both and leaves their `projection_state` as it was). `internal/events/projection` `TestBackfillKeepsTheCorruptionCheck`; `internal/store/db` `TestBackfillMigrationKeepsTheProjectedLength` (including a row a catch-up replayed under 0025, and rows with no length to give back).
- Criterion 2: the same upgrade test (intact threads equal a reindex of the uncut fixture, the seeded quote 2>1 is there, and a second start reports `backfilled` 0 and `closed` 0); `TestCatchUpBackfillsTheBacklinksAfterTheMigration` (rewritten for 0030, with a second catch-up that replays nothing); `TestAnyProjectionPaysAnOwedBackfill`; notifier `TestQuotedReadsTheRecordedBacklinks` "projected before the table existed" and "a backfill still owed".
- Criterion 3: `cmd/ketju` `TestBackfillReplayEqualsReindex`: a world written through serve (quotes, mentions, an attachment, a watch, a moderator's hide, a pin, a cross-forum split and merge, a reply as a new thread, tags and a retag, an archive); one copy has post_quotes emptied and every backfill asked for and is started, one is reindexed with `--projections --search`; every projection table, blobs, blob_uploads, audit_log, notifications, notification_progress and the search index compare equal to each other and to the rows before, table by table.
- Criterion 4: 0030's comment, docs/03 "Durability and recovery", docs/04 "post_quotes" and "projection_state", and the v1.0 checklist's part 2.
- `TestREADMEUpgradeProcedure` still passes.

### Known limit

The response cache keys a thread page on `threads.log_length_projected`, which a backfill does not move. At startup the backfill runs before the listener opens, so nothing is cached first. If a thread's backfill fails at startup and is paid later while serving, a page cached in between can show a `quoted_by` without the backfilled links until the thread's next write. It shows less than the truth, never more, and it needs a failed replay first.

## Summary

Fixed with a new migration, 0030, rather than an edit of 0025, because the migration runner refuses a database whose recorded migration text changed (`TestMigrateRefusesChecksumDrift`), and 0025 has been on main since #380.

- `0030_projection_backfill.sql` adds `projection_state.backfill`, gives every row 0025 moved back to 64 its length again from `threads.log_length_projected`, and sets the flag. A row a catch-up already replayed under 0025 is left alone.
- `ProjectThread` replays a flagged thread from its header to at least the recorded length and clears the flag with the length (`SetProjectionState`). Every projection path pays an owed backfill.
- `CatchUp` classifies against the real length first: a cut log is quarantined and keeps its flag and its rows; any other flagged thread is replayed, gap or not. `Report.Backfilled` and the log lines count them.
- The notifier reads no range of a thread whose backfill is owed.
- docs/03 "Durability and recovery", docs/04 "post_quotes" and "projection_state", the v1.0 checklist's part 2 and the release notes say how.

Tests: `TestUpgradeKeepsTruncationDetection` (ed82f15e fixture, cut at a frame boundary and to the header), `TestBackfillReplayEqualsReindex` (a world written through serve: every projection table, blobs, blob_uploads, audit_log, notifications, notification_progress and the search index equal to `reindex --projections --search` and to the rows before), `TestBackfillMigrationKeepsTheProjectedLength`, `TestBackfillKeepsTheCorruptionCheck`, `TestAnyProjectionPaysAnOwedBackfill`, and the notifier's "a backfill still owed". The replay test found no apply that is not idempotent: the projection never writes `blobs`, no trigger exists on `post_attachments`, audit rows are looked up by record, the projection writes no notifications, and search keeps its own state.

Known limit, in the plan: a backfill that fails at startup and is paid later while serving can leave a cached page whose `quoted_by` lacks the backfilled links until the thread's next write.
