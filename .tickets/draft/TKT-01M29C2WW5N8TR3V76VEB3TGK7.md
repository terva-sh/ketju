---
schema: 3
id: TKT-01M29C2WW5N8TR3V76VEB3TGK7
title: Build the projection writer
type: task
status: draft
status_reason: null
priority: urgent
due_on: null
labels:
  - area:db
  - effort:L
assignees: []
milestone: phase-1
parent: TKT-01M29BKBKV6HH8W0DN6QXQNZ0R
origin: null
dependencies:
  - TKT-01M29C0WF9GFGVG7QSAYRAKAA4
  - TKT-01M29BYYJ87H5ERS3RXC66XF44
  - TKT-01M29C0WDJ9FETR8EFZ7ED5JYF
blocks_on: none
references:
  - ref: design:storage-write-path
    path: docs/03-thread-storage.md
  - ref: design:data-model-projections
    path: docs/04-data-model.md
claim: null
archive: null
created_at: 2026-09-11T23:15:46Z
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

The subscriber that turns log records into rows: `threads`, `post_meta`, `thread_tags`, and `mentions`, advancing `projection_state.log_length` as it goes.

Ordered per thread and idempotent, keyed on thread id and log offset. Idempotence is not an optimization here. It is what makes replay after a crash safe, and every projection of every record type has to have it, including the thread events that mutate a single row.

The log is the source of truth and the database catches up to it. Nothing in this writer may ever write back into a log.

## Acceptance criteria

- [ ] Replaying the same log range twice produces identical rows, tested per record type
- [ ] Projection of one thread's records happens in log order even under concurrent events from other threads
- [ ] Every record type in docs/03 has a projection or an explicit decision that it has none
- [ ] A projection failure is retried and surfaced as lag rather than dropping the event

## Implementation plan

Pre-groom plan (2026-09-24). Revise at claim, after reading the code as it stands then.

### Approach
1. New package `internal/events/projection`. docs/02 places "subscribers, projection catch-up" under `events/`. `projection.Writer` implements `events.Subscriber` with `Name() == "projection"`.
2. The core entry point is `Writer.ProjectThread(ctx, id threadid.ID, upTo int64) (int64, error)`. It reads `projection_state.log_length` (L0, or 64 when there is no row, which is `threadlog.FileHeaderSize`). It opens `id.LogPath(root)` read-only. `os.File.ReadAt` is safe beside the actor's appends, and bytes below `upTo` are durable. It walks the frames in [L0, upTo). In ONE `DB.Write` (BEGIN IMMEDIATE), using the generated `dbq.Queries` upserts from BYYJ8's `queries/projection.sql`, it applies each frame and sets `projection_state.log_length` and `threads.log_length_projected` to the end offset. Frames below L0 are never applied twice. That offset key is the idempotence the ticket asks for.
3. Add `threadlog.ScanFramesFrom(r, from, size, fn)`, and make `ScanFrames` call it with `FileHeaderSize`. This adds an API without changing the format. The rest of the frozen v1 package stays untouched.
4. Every apply is written as an absolute set or upsert, never a relative increment. For example, Edit sets `current_revision = frame.Revision`; it does not do ++. So a replay with the state rewound still converges. The per-type mapping follows the docs/03 "Record types" table:
   - ThreadCreated upserts the `threads` row (author from `frame.Actor`, created_at from `frame.TimestampMS`) and its tags.
   - Post upserts `post_meta` and inserts `mentions` from `PostBody.Mentions`. It also sets `threads.post_count = max(post_count, ordinal)` and the last_post fields.
   - Edit sets `current_revision`, `edited_at`, `reply_to`, and `log_offset`.
   - Delete and Restore set `deleted`.
   - Renamed, Moved, Locked, Unlocked, Pinned, Unpinned (scope-matched), Tagged (full replace), and Archived set their `threads` columns.
   - The reserved types 20 and 21 have no projection until Phase 4. The reader refuses them, so the thread stalls and shows as lag.
   - An unknown advisory type is skipped via `threadlog.SkipUnknown`. An unknown critical type is an error.
5. `Deliver(ctx, batch)` groups events by thread, keeping order, and calls `ProjectThread` with each thread's highest `LogLength`. A single bus goroutine plus per-thread log reads gives log order per thread for free. If `batch.Dropped > 0`, it calls the injected `Sweep func(ctx) error`. C5Y1S supplies the real one, and tests use a naive walk.
6. Failure handling: a failed thread goes into a retry set, and `Deliver` returns the error, so the bus counts it. A retry goroutine re-runs the retry set with capped backoff (1 s to 60 s; the numbers are open). `Writer.Lag() Lag{Threads int; Bytes int64}` sums (last seen LogLength minus projected) for threads that are behind. Lag is logged on each transition, following the bus's behind/caught-up pattern.
7. Never writes to a log. Log files are opened with `os.O_RDONLY`, which is asserted in a test through an opener hook.

### Files and packages
New: `internal/events/projection/{writer.go,apply.go,retry.go,writer_test.go,apply_test.go,property_test.go}`. Touched: `internal/store/threadlog/scan.go` (ScanFramesFrom). Reused: `events.Bus`, `Batch.Dropped`, `threadlog.DecodeBody`, `SkipUnknown`, `RecordType.Known` and `Reserved`, `threadid.ID.LogPath`, `threadcache` (only in tests, to produce real logs), `db.DB.Write`, and `dbtest.DumpProjections` from BYYJ8.

### Tests
- AC1: a table test per record type (every Known type) builds a log with the actor, projects it, dumps, resets `projection_state` to 64, projects again, and asserts that the dumps are equal. A property test, modelled on `threadlog/property_test.go`, uses random record sequences and random split points to check incremental projection against a single full projection. Give it `-prop.cases` flags like the existing CI step.
- AC2: `TestPerThreadOrderUnderInterleaving` publishes interleaved events for 16 threads through a real `events.Bus` under `-race`. It then checks that each thread's `post_meta` equals the full projection. A hook records the apply order per thread, and the test asserts it is monotonic in offset.
- AC3: `TestEveryRecordTypeHasAProjectionDecision` loops t over 1..255. For every t with `Known() || Reserved()`, the writer's decision table (apply func or an explicit `noProjection` with a reason) must have an entry.
- AC4: `TestFailureRetriedAndReportedAsLag` injects a DB write error on the first N attempts. While failing, `Lag().Bytes > 0`. After recovery, the rows are identical and lag is 0. Also a test that `Dropped > 0` triggers Sweep.

### Design
docs/03-thread-storage.md "Record types" (the projection column), "Write path" steps 4–5 and its last paragraph, "Unrecognised record types", "Reserved record type numbers". docs/04-data-model.md "Projection tables". docs/02-architecture.md "Request flows / Create a post" step 4, and "Background work". The `internal/events/events.go` package doc: "The contract", promise 3.

### Interactions
Consumes BYYJ8's tables and the events bus. Defines `projection.Writer`, `ProjectThread`, `Lag`, and the `Sweep` hook. C5Y1S supplies Sweep and runs catch-up with `ProjectThread`. C5Y25 calls `ProjectThread` from offset 64. CCBZ8 reads `Lag`. The publisher is the create/reply path (TKT-01M29CA9B9). It must publish `events.Event{Thread, Ordinal, LogLength: Result.LogLength}` from the append acknowledgement path, in order.

### Open items
`EditBody` carries no mentions (records.go), so `mentions` reflects revision 1 only unless an edit re-extracts them. docs/03 does not say which. Decide at claim, and record the decision in docs/03 "Record types".

### Settled in grooming (2026-09-24)

- An Edit may carry optional `mentions`. When present, they replace the post's mention rows (docs/03, decided 2026-09-24).
