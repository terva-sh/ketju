// Package events fans a durable append out to the parts of the system that
// react to one: the projection writer, the search indexer, the notification
// generator, and the SSE fan-out (docs/02).
//
// There is no broker, no queue daemon, and no network hop. The deployment
// promise is a binary and a directory, and an external queue would break it
// for a system whose subscribers all live in the same address space. What that
// simplicity buys in operations it takes back in care: a misbehaving
// subscriber shares a process with the writer, so the bus has to be explicit
// about what it will and will not do to a writer on a subscriber's behalf.
//
// # The contract
//
// Three promises, listed in the order they win when they conflict.
//
//  1. Publishing never blocks. An append that has been fsynced is finished,
//     and the caller that made it durable is not made to wait on anything
//     downstream. Publish takes no lock a subscriber can hold and performs no
//     blocking send.
//
//  2. Events for one thread reach a subscriber in log order. The bus keeps one
//     queue per subscriber and delivers from it in order, so ordering holds as
//     long as the caller publishes a thread's events in the order its appends
//     were acknowledged. One actor owns a thread and commits serially, so a
//     caller that publishes from the acknowledgement path gets this for free.
//
//  3. Delivery is at-least-once, in cooperation with the subscriber. The
//     queues are bounded, so the bus cannot both refuse to block a writer and
//     promise to hand over every event from memory alone. When a queue is
//     full the event is dropped and counted, and the count is handed to the
//     subscriber on its next batch in [Batch.Dropped].
//
// Promise three is the one that needs reading twice. A non-zero
// [Batch.Dropped] is not a diagnostic, it is an instruction: the subscriber
// must recover the events it missed from durable state before trusting what
// follows. It can, because the bus is a hint and not the record. The thread
// log is the record, every event names the log length that produced it, and
// each subscriber already knows how far it has projected. That is the same
// path a subscriber takes after a restart, so a drop costs a catch-up and
// never costs correctness.
//
// What the bus will not do is hide the drop. Falling behind flips a subscriber
// into a logged, counted state that [Bus.Stats] reports, because a projection
// that is quietly twenty minutes stale is the failure this design is built to
// make impossible to miss.
package events

import (
	"context"

	"github.com/terva-sh/ketju/internal/store/threadid"
)

// Event is one durable append, named by where it landed.
//
// It carries no content. A subscriber that needs the post reads it from the
// thread log, which keeps the event small, keeps per-viewer logic out of the
// bus, and means the SSE fan-out can forward an event to a browser without
// having decided whether that browser is allowed to see the post — the
// follow-up fetch is where authorization happens (docs/02).
type Event struct {
	// Thread is the thread that was appended to.
	Thread threadid.ID

	// Ordinal is the post the record concerns, one-based. It is zero for
	// records that concern the thread rather than a post, such as a rename.
	Ordinal uint32

	// LogLength is the length of the log in bytes after the append.
	//
	// This is the thread's version number: monotonic, changed by every
	// mutation, and available without a database query. It is what a
	// subscriber compares against its own progress to know what it has
	// missed, and what an SSE client echoes back as after_log_length.
	LogLength int64
}

// Batch is what a subscriber receives: events to process, and an account of
// what it did not receive.
type Batch struct {
	// Events are in per-thread log order and never empty.
	Events []Event

	// Dropped is how many events for this subscriber were discarded since its
	// last successful delivery, because its queue was full or because a
	// previous batch failed.
	//
	// Non-zero means this subscriber's view has a hole in it. Processing
	// Events alone will not fill the hole; the subscriber must catch up from
	// durable state, as it would after a restart. Ignoring a non-zero Dropped
	// is how a projection goes permanently and silently wrong.
	Dropped uint64
}

// Subscriber is one consumer of the bus.
//
// Deliver runs on a goroutine the bus owns, one per subscriber, so a
// subscriber is never called concurrently with itself and does not need
// internal locking for ordering. It may block for as long as it likes without
// stalling a writer; the cost of blocking is its own queue filling and its own
// events being dropped.
//
// Returning an error counts the batch as dropped rather than retried. A retry
// loop inside the bus would trade a visible hole for an invisible stall, and
// the subscriber's own catch-up path already handles a hole correctly.
type Subscriber interface {
	// Name identifies the subscriber in logs and in Stats. It must be unique
	// across the bus and stable across restarts, because it is how an
	// operator matches a lag metric to a component.
	Name() string

	// Deliver processes a batch. The context is the one passed to Start and is
	// cancelled when the bus shuts down.
	Deliver(ctx context.Context, b Batch) error
}

// SubscriberFunc adapts a function to [Subscriber], for tests and for
// subscribers with no state of their own.
type SubscriberFunc struct {
	N string
	F func(ctx context.Context, b Batch) error
}

// Name reports the subscriber's name.
func (s SubscriberFunc) Name() string { return s.N }

// Deliver calls the wrapped function.
func (s SubscriberFunc) Deliver(ctx context.Context, b Batch) error { return s.F(ctx, b) }
