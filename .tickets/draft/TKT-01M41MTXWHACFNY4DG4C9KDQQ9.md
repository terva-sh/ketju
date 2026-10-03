---
schema: 3
id: TKT-01M41MTXWHACFNY4DG4C9KDQQ9
title: Keep truncation detection on the first start after migration 0025
type: bug
status: draft
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
updated_at: 2026-10-03T19:46:10Z
created_by:
  id: agent:claude-code/phase4-run-orchestrator
  name: ""
updated_by:
  id: agent:claude-code/phase4-run-orchestrator
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

- [ ] A thread log truncated before the upgrade, at a frame boundary above its header or to exactly its header, is classified corrupt and quarantined on the first start after the backlinks migration, as it would be on any other start (an upgrade test from a pre-0025 fixture shows it)
- [ ] Every intact log still gets its quote backlinks backfilled on that first start, and the backfill runs once
- [ ] Replaying every log over an existing database leaves every projection table, blobs.refcount, audit_log, notifications and the search index equal to a fresh ketju reindex of the same logs (a test compares them table by table)
- [ ] The migration's comment and docs/03 or docs/04 say how the backfill keeps the truncation check, and the v1.0 checklist's part 2 records the outcome
