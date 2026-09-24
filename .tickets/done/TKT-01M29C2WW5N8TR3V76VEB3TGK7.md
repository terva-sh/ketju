---
schema: 3
id: TKT-01M29C2WW5N8TR3V76VEB3TGK7
title: Build the projection writer
type: task
status: done
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
  - ref: pr:54
    path: null
claim: null
archive: null
created_at: 2026-09-11T23:15:46Z
updated_at: 2026-09-24T08:52:27Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase1-run-415af6aa
  name: ""
extensions: {}
---

## Description

The subscriber that turns log records into rows: `threads`, `post_meta`, `thread_tags`, and `mentions`, advancing `projection_state.log_length` as it goes.

Ordered per thread and idempotent, keyed on thread id and log offset. Idempotence is not an optimization here. It is what makes replay after a crash safe, and every projection of every record type has to have it, including the thread events that mutate a single row.

The log is the source of truth and the database catches up to it. Nothing in this writer may ever write back into a log.

## Acceptance criteria

- [x] Replaying the same log range twice produces identical rows, tested per record type
- [x] Projection of one thread's records happens in log order even under concurrent events from other threads
- [x] Every record type in docs/03 has a projection or an explicit decision that it has none
- [x] A projection failure is retried and surfaced as lag rather than dropping the event

## Implementation plan

Revised at claim (2026-09-24), after reading the code as it stands.

### Approach
1. `threadlog.ScanFramesFrom(r, from, size, fn)`. ScanFrames becomes the call from FileHeaderSize. The format is unchanged.
2. `threadlog.EditBody` gains `Mentions *[]uint64` (`cbor:"mentions,omitempty"`).
   - docs/03 already declares the key: it was added 2026-09-24 as an optional body field, which the freeze permits. The Go struct had not caught up.
   - It is a pointer, so "absent" (keep the post's mentions) is distinguishable from "present and empty" (the edit removed every mention). With a plain slice and omitempty, an edit could never clear the last mention.
3. New package `internal/events/projection`, with `Writer` implementing `events.Subscriber` under the name "projection".
   - `ProjectThread(ctx, id, upTo)` runs in ONE `db.Write`. It reads `projection_state.log_length` (L0, or 64) inside the transaction, scans frames in [L0, upTo) read-only, applies each, and sets `projection_state.log_length` and `threads.log_length_projected` to upTo.
   - The high-water mark is the (thread, offset) idempotence key: a frame below it is never applied again.
   - Every apply is an absolute set, so replaying from 64 converges on the same rows.
4. Per-type decisions live in a table, with an apply function or an explicit no-projection reason:
   - **ThreadCreated:** upsert the thread row, resetting every column, and replace its tags.
   - **Post:** upsert post_meta with every column reset; set post_count and last_post_* from this post; replace its mentions.
   - **Edit:** current_revision, edited_at, reply_to and log_offset; replace mentions if the key is present.
   - **Delete / Restore:** set deleted.
   - **Renamed, Moved, Locked/Unlocked, Pinned, Tagged, Archived:** set their threads columns. Unpinned clears only a matching scope.
   - **Reserved 20/21 and unknown advisory types:** skipped, as the index does (apply.go uses SkipUnknown). A projection that refused where the index skips would leave the two folds of one log disagreeing.
   - **Unknown critical types:** an error, so the thread stalls and shows as lag.
   - **Damaged frames:** an error, never skipped.
   - A post-level record whose row is missing is an error, as the index errors.
5. `Deliver`:
   - Groups by thread in order and projects each to its highest LogLength, plus any thread already behind.
   - One thread's failure does not stop the others.
   - A failed thread joins the behind set, and Deliver returns nil: the event is held for retry, not dropped. Returning an error would make the bus count it as dropped, and every later batch would carry a spurious hole.
   - `Dropped > 0` calls `Options.Sweep`, which C5Y1S supplies. A sweep error is returned so the hole is reported again.
6. `Retry(ctx)` re-runs the behind set. `Run(ctx)` calls it with backoff from 1 s up to 60 s. `Lag()` reports the threads and bytes behind. A transition to behind logs a warning, and catching up logs at info.
7. Logs are opened O_RDONLY through an opener hook, and a test asserts the flag.
8. The reads and upserts are sqlc queries in `queries/projection.sql`.

### Tests
- **AC1:** for each record type, build a real log through threadcache and the actor, project it, dump, reset projection_state to 64, project again, and compare the dumps. Also project in random splits and compare with one full projection.
- **AC2:** interleaved events for many threads through a real `events.Bus`, under -race. Each thread's rows must equal its full projection, and an apply hook shows offsets rising per thread.
- **AC3:** every type from 1 to 255 that is Known or Reserved has an entry in the decision table.
- **AC4:** an injected write failure leaves Lag above zero and the event kept. Retry catches up with identical rows. Dropped triggers Sweep.

## Notes

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T08:41:59Z

### Implementation notes

- **Edit mentions.** `EditBody.Mentions` is `*[]uint64`. docs/03 had already declared the key, but the struct had not caught up. A pointer is the only way to tell "absent" (keep the mentions) from "present and empty" (remove them all). The old open item on this ticket is closed by that.
- **Reserved types 20 and 21 are skipped, as the index skips them.** docs/03 line 102 says a reader refuses them. The code and its tests say skip. The projection follows the code, so the two folds agree. The conflict is filed as draft TKT-01M39973KG.
- **Deliver never returns a projection failure.** The bus would count it as a drop, and every later batch would then carry a false hole. The thread is held in the behind set and reported by `Lag()`. Only a failed Sweep is returned.
- **The whole range runs in one write transaction**, including the file read. That holds the single writer for the duration of a pread walk, which is bounded by one thread's unprojected tail.

### Mutation check

28 mutants, all killed; the first round let 9 survive, and each got a test.

The first round's survivors:
- edited_at dropped;
- the thread upsert not resetting flags (now caught by replaying over deliberately stale rows);
- the already-projected check removed;
- damaged frames skipped;
- the caught-up check skipped;
- the behind target lowered;
- Deliver not retrying behind threads;
- the short-log check removed;
- Edit reply_to dropped.

The rest were killed from the start:
- Edit mentions (absent versus empty);
- a pin with until 0;
- a missing row;
- ThreadCreated tags;
- Restore and Unlock;
- the Post upsert keeping stale state;
- the Unpin scope and until;
- all decode errors skipped;
- apply errors ignored;
- Deliver returning an error;
- the sweep error swallowed;
- Dropped ignored;
- the first event winning over the highest;
- a log opened writable;
- a scan starting inside the header.

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T08:47:11Z

Terva review 389 of head 70adf18 (base a15a1aa): request ready-review-70adf18, Actions run 164, run id 85c1519f-f85d-4178-8185-cd27d367867e. It raised two findings, and both are accepted and fixed on the next head.

### 1. Delete and Restore overwrite post_meta.log_offset (high, apply.go:121)

- **Accepted.** The index's Offset is the offset of the current revision's record, and a reader loading a body from log_offset would have got the Delete frame.
- **Fix.** SetPostDeleted no longer touches log_offset.
- **Documentation.** Migration 0004's column comment says otherwise. It cannot be edited, because applied migrations are checksummed, so docs/04 now states the meaning and says the comment is wrong.
- **Test.** `agreesWithIndex` compares every post_meta row with the in-memory index entry: author, created, revision, offset, reply_to and deleted. It runs after every per-type case and every random stepped projection. A mutant that moves log_offset on Delete is killed.

### 2. A nil Sweep acknowledges dropped events (medium, writer.go:123)

- **Accepted, and it was worse than reported.** A failed sweep also returned before the batch's own events were projected.
- **Fix, part 1.** `New` now returns an error unless DB, Root and Sweep are all set.
- **Fix, part 2.** A failed sweep is still returned to the bus, so the hole is reported again, but the batch's events are projected first.
- **Tests.** TestNewRequiresOptions, and TestDroppedTriggersSweep now asserts that the batch is projected when the sweep fails. Mutants for an optional sweep, a swallowed sweep error, and a sweep error that is not returned are all killed.

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T08:52:27Z

Terva review 390 of head 5435485 (base a15a1aa): request ready-review-5435485, Actions run 166, run id 82db4ee7-3cb4-4a5a-8e0b-84ff4532c03a. It marked review 389's two findings resolved and raised two new mediums. Both are accepted and fixed on the next head.

### 1. An empty ThreadTagged advances the projection without a thread row (medium, apply.go:175)

- **Accepted.** An empty tag list touches no row, and SetThreadProjected did not check rows either.
- **Fix, part 1.** applyTagged now requires the thread row to exist.
- **Fix, part 2.** SetThreadProjected is `:execrows` and must hit exactly one row. A range whose records never created the thread is therefore an error, and projection_state does not advance.
- **Test.** TestNoThreadRowIsAnError covers three logs: empty tags alone, only skippable records, and tags before ThreadCreated. The last is the case the tag check alone catches.
- **Mutants.** Killed: removing the tag check, and letting SetThreadProjected accept zero rows. The mutant that disables only the ErrNoRows branch is equivalent, because the error still returns through the next branch with a different message.

### 2. The log header is not validated (medium, writer.go:288)

- **Accepted.** ProjectThread now reads and parses the file header before applying anything. It checks the checksum and magic, checks the version, and requires the header's thread id to match the thread being projected.
- **Test.** TestHeaderIsChecked covers a copy of thread A's log at thread B's path and a flipped header byte (ErrChecksum). Neither leaves rows.
- **Mutants.** Skipping the header check, and skipping the thread-id comparison, are both killed.

## Summary

Landed as `internal/events/projection`, which provides the `Writer` subscriber.

- **How a thread is projected.** `ProjectThread` projects a thread in one transaction, from `projection_state.log_length` to the event's length. Logs are opened read-only.
- **Record types.** Every record type has an apply that sets absolute values, or a stated reason it has none: reserved types are skipped, as the index skips them.
- **Failures.** A failure is held in a behind set, retried on every delivery and by `Run` (1 s to 60 s backoff), and reported by `Lag()`. It is never returned to the bus as a drop.
- **Dropped events.** A batch with Dropped > 0 calls `Options.Sweep`.
- **Supporting changes.**
  - `threadlog.ScanFramesFrom`.
  - `EditBody.Mentions` as an optional pointer, per docs/03.
  - The writer's sqlc upserts.
  - docs/03 names the writer.
- **Tests.** Per-type replay over stale rows and into empty tables, random stepped-versus-full projection, 16 threads interleaved through a real bus under -race, the decision table, failure and lag, sweep, damage, and unknown types. 28 mutants, all killed.
- **Follow-up.** The reserved-type conflict between docs/03 and the code is filed as draft TKT-01M39973KG.
