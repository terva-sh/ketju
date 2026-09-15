---
schema: 3
id: TKT-01M29C0WD88M1C8EW4QQTX76NJ
title: Implement the thread actor with ordinal assignment and group commit
type: task
status: done
status_reason: null
priority: urgent
due_on: null
labels:
  - area:storage
  - effort:L
assignees: []
milestone: phase-0
parent: TKT-01M29BKBKP8C66GXXQK4HPEZ35
origin: null
dependencies:
  - TKT-01M29BYYGNN34ZP3AMFZ540R2H
  - TKT-01M29BYYGYZ8FQB2W2XC14S2T3
blocks_on: none
references:
  - ref: design:storage-write-path
    path: docs/03-thread-storage.md
claim: null
archive: null
created_at: 2026-09-11T23:14:40Z
updated_at: 2026-09-15T04:46:53Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/storage
  name: ""
extensions: {}
---

## Description

One goroutine owns each open thread. Appends arrive over a channel and the caller gets its answer only after the record is durable.

This buys three things at once. Ordinals are assigned without a lock dance because the actor knows post_count. There is exactly one writer per file, so offsets are known before the write and O_APPEND semantics stop mattering. And it is the natural place for group commit: several appends arriving within a few milliseconds are written and fsynced once.

Ordering within step 4 of the docs/03 write path is load-bearing. The index header is updated after the log fsync, so the worst case on a crash is an index claiming to be older than it is, which triggers a harmless replay. The reverse ordering loses data.

## Acceptance criteria

- [x] Concurrent appends to one thread produce a gapless ordinal sequence with no duplicates, under the race detector
- [x] Appends arriving within the group window issue one fsync, verified by counting syscalls
- [x] The index header is written only after the log fsync returns, proven by a test that injects a failure between the two
- [x] An append returns ordinal, revision, and the new log length to its caller

## Implementation plan

`actor.go`: one goroutine per open thread, requests over an unbuffered channel,
the caller blocked until its record is durable.

Commit is five steps in a fixed order: encode and number the whole batch
without touching disk, one Write for the batch, one Sync, then the index, then
answer. Encoding first means a malformed request fails alone rather than
poisoning the batch. Everything after the Sync depends on it having returned,
which is the docs/03 step 4 ordering.

Durability is behind two interfaces, LogWriter and IndexWriter, which *os.File
satisfies. That is not abstraction for its own sake: two of the four criteria
are about the ORDER and COUNT of syscalls, and neither can be shown by
inspecting files afterwards. The tests use fakes that record every operation
into one shared log so the global sequence across both files is assertable, and
that can be made to fail at a chosen point.

The index header is written per batch but fsynced only on Close. The header is
what makes an index trusted on the next open, so it is worth writing eagerly;
its durability buys only a faster open, so it is not worth an fsync per batch.
That is also what keeps a batch at exactly one fsync.

## Notes

**agent:claude-code/storage** at 2026-09-15T04:10:38Z

Measured: 17 concurrent appends committed in 2 batches, so drain-batching
collapses 17 fsyncs into 2. SyncAlways is the control in the same test and
issues exactly one per append.

A wording mismatch worth recording rather than quietly resolving. This ticket's
second criterion says "appends arriving within the group WINDOW", which implies
a timer. docs/03 line 325 says the opposite and says it deliberately: the actor
"takes one append, drains whatever else is already queued, and issues a single
fsync for the whole batch. It never waits to form one." Drain is what is
implemented, because a window makes every low-traffic append pay it, and that
is the trade docs/03 explicitly refuses. The criterion is met either way —
queued appends do share one fsync — but the mechanism is not a window and the
code says so.

Two design points a reviewer should check rather than assume.

An index write failure does NOT fail the append. By the time the index is
touched the record is fsynced into the log, and the log is the source of truth,
so failing the caller would report a loss that did not happen. Instead the
header is left stale, which makes the next open rebuild, and the error surfaces
from Close. TestIndexFailureDoesNotFailADurableAppend asserts all three: the
append succeeds, Close reports, and reopening is NOT Trusted.

Ordinal assignment consults the staged batch, not only the committed index.
Several posts can be in one batch before any has been applied, and an actor
reading only a.ix.Len() would hand all of them the same number — the exact bug
single-writer ownership is supposed to make impossible.
TestPostsInOneBatchGetConsecutiveOrdinals is deliberately separate from the
concurrency test because the concurrency test would pass with batch size 1.

Both batching tests form their batch deterministically instead of hoping the
scheduler cooperates: the first Sync blocks on a gate while more appends queue
behind it, then the gate is released and the drain collects them.

The first draft repeated a six-field anonymous struct across three function
signatures. Replaced with a named `staged` type before any of it was tested.

**agent:claude-code/storage** at 2026-09-15T04:46:53Z

Shipped with a latent panic, found and fixed while working TKT-01M29C0WDJ. `Close` closed the request channel to signal shutdown, and `Append` selected on that send against a done channel. A send on a CLOSED channel panics rather than blocking, and selecting alongside it does not help — the send case is "ready" the moment the channel closes, so select can pick it and panic. TestAppendAfterCloseIsRefused passed on the original run purely on timing; adding a mutex to Index in the read-path ticket shifted the schedule and it panicked every time. Fixed by never closing the request channel: an inflight WaitGroup and a closed flag admit or refuse Appends, Close waits for admitted ones to be answered, and only then closes a separate quit channel the loop selects on. The lesson worth keeping is that a passing concurrency test is weak evidence when the failure mode is a race — it took an unrelated timing change to expose this.

## Summary

`actor.go`: one goroutine per open thread, appends over a channel, the caller
blocked until its record is durable. All four criteria met, 9 tests, the whole
package green under -race.

Commit is five steps in a fixed order — encode and number the batch, one Write,
one Sync, then the index, then answer. Encoding before any I/O means a
malformed request fails alone instead of poisoning its batch.

Drain-batching measured at 17 concurrent appends committed in 2 batches, so 17
fsyncs collapse to 2. SyncAlways is the control in the same test and issues one
per append. Both batching tests form their batch deterministically rather than
hoping the scheduler cooperates: the first Sync blocks on a gate while the rest
queue behind it.

The ordering criterion is proven twice — the operation sequence is asserted on a
successful append, and then the log fsync is made to fail and the index must
show no sign of having run. Durability sits behind two interfaces that *os.File
satisfies, which is what makes the order and count of syscalls assertable at
all; the test fakes record every operation into one shared log so the sequence
ACROSS both files can be checked, not just within one.

The criterion's phrase "group window" is not what was built. docs/03 line 325
says the actor drains what is already queued and never waits to form a batch,
because a window makes every low-traffic append pay it. Queued appends do share
one fsync, so the criterion is met, but the mechanism is drain and not a timer.

Two decisions worth not undoing:

An index write failure does not fail the append. By then the record is fsynced
into the log and the log is the source of truth, so failing the caller would
report a loss that did not happen. The header is left stale instead — which
makes the next open rebuild — and the error surfaces from Close. Asserted three
ways: the append succeeds, Close reports, and reopening is not Trusted.

The index header is written per batch but fsynced only on Close. Writing it
eagerly is what lets the next open be Trusted; fsyncing it per batch would buy
nothing but a second fsync and would break the one-fsync-per-batch property.

Ordinal assignment consults the staged batch rather than only the committed
index, because several posts can be in flight before any is applied. Its test is
deliberately separate from the concurrency test, which would pass with batch
size 1.
