// Package posts holds the one seam every thread append goes through.
package posts

import (
	"github.com/terva-sh/ketju/internal/events"
	"github.com/terva-sh/ketju/internal/store/threadcache"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// Writer appends records to threads and announces them (docs/03 "Write
// path", steps 3 to 6). Every write to a thread log that the server makes on
// a request goes through Append, so nothing appends without publishing and
// nothing publishes what was not appended. Edits, deletes and moderation
// (TKT-01M29CA9BP and later) reuse it.
type Writer struct {
	// Publish announces a durable append: events.Bus.Publish in the server.
	// Nil publishes nothing, for tests that do not watch.
	Publish func(events.Event)
}

// Append writes a to th through its actor, which returns once the record is
// durable under the thread's sync policy, then publishes the event every
// subscriber (the projection, and later search, SSE and notifications)
// catches up from.
//
// A failed append publishes nothing: there is nothing new to catch up to.
func (w *Writer) Append(th *threadcache.Thread, a threadlog.Append) (threadlog.Result, error) {
	res, err := th.Actor.Append(a)
	if err != nil {
		return res, err
	}
	if w.Publish != nil {
		w.Publish(events.Event{Thread: th.ID, Ordinal: res.Ordinal, LogLength: res.LogLength})
	}
	return res, nil
}
