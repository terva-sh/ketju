---
schema: 3
id: TKT-01M29C0WF9GFGVG7QSAYRAKAA4
title: Build the in-process event bus
type: task
status: done
status_reason: null
priority: urgent
due_on: null
labels:
  - area:db
  - effort:M
assignees: []
milestone: phase-1
parent: TKT-01M29BKBKV6HH8W0DN6QXQNZ0R
origin: null
dependencies: []
blocks_on: none
references:
  - ref: design:background-work
    path: docs/02-architecture.md
claim: null
archive: null
created_at: 2026-09-11T23:14:40Z
updated_at: 2026-09-15T16:59:53Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/events
  name: ""
extensions: {}
---

## Description

`internal/events`: publish `{thread_id, ordinal, new_log_length}` after a durable append, and fan it out to the projection writer, the search indexer, the notification generator, and the SSE fan-out.

No external queue, by design. The whole system is goroutines in one process, which is what keeps the deployment to a binary and a directory.

The delivery contract needs stating here because four subscribers depend on it: ordered per thread, at-least-once, and never blocking the append path. A slow subscriber must fall behind rather than stall a writer, and falling behind must be visible as projection lag rather than silent.

## Acceptance criteria

- [x] Events for one thread are delivered to each subscriber in log order
- [x] A slow or failing subscriber never blocks an append, proven by a test with a deliberately stalled subscriber
- [x] Subscriber lag is observable, and a subscriber that falls behind is logged rather than dropped silently
- [x] Subscribers are registered at startup and the set is visible in one place

## Implementation plan

internal/events with one type per concern: Event {Thread, Ordinal, LogLength}, Batch {Events, Dropped}, Subscriber {Name, Deliver}, and a Bus with a fixed lifecycle of subscribe, start, publish, close.

One goroutine and one bounded channel per subscriber. Publish does a non-blocking send to each and nothing else, so it cannot wait on anyone. The runner takes one event and then drains whatever else is already queued into a single batch, which is the thread actor's group-commit shape and makes the batched search indexer free.

The three promises and how each is met:

1. Never blocks a writer. Publish holds only an RWMutex read lock and performs only non-blocking sends. The lock is not there for the queues, it is there to order Publish against Close so that closing the channels cannot race a send -- the same send-on-a-closed-channel bug the actor had.

2. Ordered per thread. One FIFO queue per subscriber, and one actor per thread committing serially upstream, so publishing from the acknowledgement path is enough.

3. At-least-once, in cooperation with the subscriber. Bounded queues mean the bus cannot promise both 1 and delivery of every event from memory, so a full queue drops and counts, and the count reaches the subscriber as Batch.Dropped on its next delivery. A failed Deliver is folded into the same counter: a batch that was not processed is the same hole an overflow is, and giving the subscriber one mechanism to handle rather than two is what keeps the recovery path exercised.

## Notes

**agent:claude-code/events** at 2026-09-15T16:59:31Z

The delivery contract has a limit that belongs on the ticket rather than only in a doc comment, because the next four tickets are its subscribers.

At-least-once is a joint property of the bus and the subscriber, not a property of the bus. The queues are bounded, so 'never block a writer' and 'hand over every event from memory' cannot both hold; something has to give, and what gives is memory. A full queue drops the event and raises a counter, and the counter reaches the subscriber as Batch.Dropped on its next delivery.

A non-zero Dropped is an instruction, not a diagnostic: recover the missed events from durable state before trusting what follows. That is possible because the bus is a hint and the thread log is the record. Every event names the log length that produced it, and each subscriber already knows how far it has projected, so the recovery path is the same one it takes after a restart -- TKT-01M29C5Y1S startup catch-up, and TKT-01M29C5Y25 reindex. A drop therefore costs a catch-up and never costs correctness.

A subscriber that ignores Dropped is silently and permanently wrong. That is the failure mode to watch for in the projection writer review.

Drops discard the newest event rather than the oldest. Order is unaffected either way; the newest is simply what a channel send does when the buffer is full, and since recovery reads forward from the last projected position, which end was discarded does not change the work.

**agent:claude-code/events** at 2026-09-15T16:59:31Z

Two smaller decisions worth their reasons.

Failure is counted as a drop rather than retried. A retry loop inside the bus would trade a visible hole for an invisible stall, and it would need its own backoff, its own bound, and its own way of giving up -- three new behaviours to reason about, when the subscriber already has a correct recovery path for exactly this situation. So a rejected batch raises the same counter an overflow does, and the subscriber learns about it the same way.

Falling behind is logged at the transitions, not per event. One line when a subscriber first drops something, one when it catches up. Per-event logging would be a flood at precisely the moment the machine is already struggling, and it would put a log write on the append path for every event rather than once per episode. The counters in Stats carry the volume; the log carries the news.

Found while writing this: TestQueuedAppendsShareOneFsync in threadlog was not deterministic and this package's tests made it fail about two runs in five. Filed and fixed as TKT-01M2JZB0, merged ahead of this one so the baseline was green.

**agent:claude-code/events** at 2026-09-15T16:59:46Z

Criteria, and the test that holds each one.

1. Ordered per thread -- TestDeliversOneThreadInLogOrder. Four threads interleaved on the way in, 250 events each; each thread's ordinals must come out gapless and ascending. Interleaving is what makes it catch a bus that preserved global arrival order by accident.

2. A slow subscriber never blocks an append -- TestStalledSubscriberNeverBlocksPublish. One subscriber wedged inside Deliver with a queue of 4, a healthy one with a queue larger than the run, 5000 publishes. Publish must return, the healthy subscriber must lose nothing, the stalled one must be reported behind, and the log must say so.

3. Lag observable, drops logged not silent -- Stats() reports Queued, Capacity, Delivered, Dropped, Failed, Behind and LastError per subscriber, which is the seam TKT-01M29CEE7S will read for /metrics. Bus-side lag is Queued plus Dropped; projection lag proper (log length minus projected length) is the projection writer's number, since the bus only knows what it handed over. Asserted in tests 2, TestDropsAreReportedToTheSubscriber and TestFailedBatchIsCountedAsDropped, each of which checks the log line as well as the counter.

4. Registered at startup, visible in one place -- TestSubscriberSetIsFixedAtStartup. Subscribe after Start returns ErrStarted, duplicate and empty names are refused, and Subscribers() is the single list, printed by the startup log line.

Also held: Close drains what is queued; Close names subscribers still running when its context expires rather than hanging; Publish outside the running window is a no-op; and TestPublishRacesCloseWithoutPanicking runs 8 writers against a mid-flight Close 20 times, which is the test that holds the RWMutex discipline in place.

## Summary

internal/events ships the bus: Event, Batch, Subscriber, and a Bus with a fixed subscribe-start-publish-close lifecycle, one goroutine and one bounded queue per subscriber, and drain-batching so a batched subscriber costs nothing extra.

Publish never blocks -- non-blocking sends under a read lock whose only job is ordering against Close. Per-thread order comes from one FIFO per subscriber. At-least-once holds in cooperation with the subscriber: a full queue or a rejected batch drops, counts, and reports the count as Batch.Dropped, which the subscriber must treat as an instruction to catch up from durable state.

Stats() is the observability seam for /metrics. No subscriber is wired up here; the projection writer (TKT-01M29C2WW5) is the first, and it is where a subscriber ignoring Dropped would first hurt.
