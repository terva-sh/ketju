package projection

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// Recover opens a thread the way the server does, repairing a torn tail, and
// returns its log length after the repair. Serve supplies it from the thread
// cache, which is the one writer a log may have; catch-up never writes a log
// itself.
type Recover func(ctx context.Context, id threadid.ID) (int64, error)

// Report is what one catch-up found.
type Report struct {
	// Scanned is how many logs the walk found.
	Scanned int

	// Closed is how many gaps were closed, and Bytes how much log they
	// replayed.
	Closed int
	Bytes  int64

	// Corrupt are the threads whose log is shorter than their projection,
	// including a projection with no log at all. They are quarantined: see
	// Writer.Quarantined.
	Corrupt []threadid.ID

	// Failed are the threads with a gap that could not be closed, whether
	// the thread could not be opened or its projection failed. Each is in
	// the writer's behind set, which Run retries and Lag reports.
	Failed []threadid.ID

	// Unfinished are threads whose log holds only its file header and that
	// the projection has never seen: a crash between creating the files and
	// appending ThreadCreated. There is nothing to project: ProjectThread
	// has no frame to apply and records no state, so treating one as a gap
	// would count it closed and find it open again at every catch-up. They
	// are reported instead (review 456).
	Unfinished []threadid.ID

	// Strays are files under threads/ that are not a thread's log or index
	// at the path its id names.
	Strays []string

	Elapsed time.Duration
}

// CatchUp closes the gap between every log and its projection (docs/03
// "Durability and recovery"). A crash between a log write and the database
// write leaves exactly such a gap, and serve runs this before the listener
// accepts a request, so nothing is served from a projection that is behind.
// It is also the Sweep a batch reporting dropped events calls.
//
// The walk opens no log. The projected lengths come from one query, the log
// sizes from the directory entries, and a thread whose two agree costs a
// stat. Only a thread with a gap is opened, through reopen, and projected up
// to the length reopen reports: a torn final frame is truncated by that
// open, so the projection stops at the last whole frame rather than failing
// on the torn one.
//
// A log shorter than its projection cannot come from a crash, because the
// projection is written after the log, so it is corruption. It is logged at
// error level and quarantined, and the rest keep serving; nothing about it
// is rewritten. Each is quarantined as soon as it is found, so a sweep that
// stops partway leaves it quarantined. A catch-up that finishes replaces the
// quarantine with what it found, so a thread an operator repaired leaves it
// at the next complete one; an interrupted one lets nothing out.
//
// Each thread's gap is closed in its own transaction. A process that dies
// partway leaves every finished thread consistent and every other one still
// showing its gap, and the next catch-up picks up from there.
//
// The error is for a catch-up that could not look: the query, or the walk.
// What it found wrong with a thread is in the Report.
func (w *Writer) CatchUp(ctx context.Context, reopen Recover) (Report, error) {
	start := time.Now()
	var rep Report
	if reopen == nil {
		return rep, errors.New("projection: CatchUp needs a Recover to open a thread with a gap")
	}
	// One sweep at a time, and never beside a Retry: see Writer.sweeping.
	w.sweeping.Lock()
	defer w.sweeping.Unlock()

	projected := map[threadid.ID]int64{}
	err := w.db.Read(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		rows, err := q.ListProjectionLengths(ctx)
		for _, r := range rows {
			projected[threadid.ID(r.ThreadID)] = r.LogLength
		}
		return err
	})
	if err != nil {
		return rep, fmt.Errorf("projection: catch-up: reading projection_state: %w", err)
	}

	logs, strays, err := threadid.WalkLogs(w.root)
	if err != nil {
		return rep, fmt.Errorf("projection: catch-up: %w", err)
	}
	rep.Scanned, rep.Strays = len(logs), strays
	for _, path := range strays {
		w.log.Warn("a file under threads/ is not a thread's log or index", "path", path)
	}

	w.mu.Lock()
	w.reopen = reopen
	w.mu.Unlock()

	// Each thread found corrupt is published to the quarantine as it is
	// found, in the same critical section that drops it from the behind set,
	// so a reader never sees it in neither. A sweep interrupted after this
	// point returns with the thread still quarantined, not served from a
	// projection longer than its log (review 468).
	corrupt := map[threadid.ID]bool{}
	quarantine := func(id threadid.ID, logLength, at int64) {
		corrupt[id] = true
		w.mu.Lock()
		if w.quarantined == nil {
			w.quarantined = map[threadid.ID]bool{}
		}
		w.quarantined[id] = true
		w.quarantinedN.Store(int64(len(w.quarantined)))
		w.drop(id)
		w.mu.Unlock()
		w.logCorrupt(id, logLength, at)
	}

	ids := make([]threadid.ID, 0, len(logs))
	for id := range logs {
		ids = append(ids, id)
	}
	slices.Sort(ids) // a log line order an operator can follow, and a test can

	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return rep, err
		}
		size := logs[id]
		at, known := projected[id]
		if !known {
			at = Missing
		}
		class := Classify(size, at)
		if !known {
			at = threadlog.FileHeaderSize // a thread the projection never saw
		}
		switch class {
		case ClassUnfinished:
			rep.Unfinished = append(rep.Unfinished, id)
			w.settle(id, size) // a gap held while it still had a torn frame
			w.log.Info("a thread log holds only its header: its creation never finished, "+
				"and there is nothing to project", "thread", id)
			continue
		case ClassCurrent:
			w.settle(id, size) // a gap held earlier that has since closed
			continue
		case ClassCorrupt:
			quarantine(id, size, at)
			continue
		}

		// A gap. Open the thread for its real length, which a torn tail makes
		// shorter than the file.
		length, err := reopen(ctx, id)
		if err != nil {
			// Held for Run to retry, up to the size the walk saw, so the gap
			// is neither forgotten nor served as if it were closed (review
			// 448); Lag reports it until it is.
			rep.Failed = append(rep.Failed, id)
			w.hold(id, at, size)
			w.log.Error("a thread with a projection gap could not be opened; it is held for retry",
				"thread", id, "log_size", size, "projected", at, "err", err)
			continue
		}
		switch Classify(length, at) {
		case ClassCorrupt:
			quarantine(id, length, at)
			continue
		case ClassCurrent:
			w.settle(id, length) // the gap was a torn tail, and the open removed it
			continue
		}
		if err := w.project(ctx, id, length); err != nil {
			rep.Failed = append(rep.Failed, id)
			continue
		}
		rep.Closed++
		rep.Bytes += length - at
	}

	// A projection whose log is gone is the shortest log there is.
	for id, at := range projected {
		if _, ok := logs[id]; !ok && Classify(Missing, at) == ClassCorrupt {
			quarantine(id, 0, at)
		}
	}

	for id := range corrupt {
		rep.Corrupt = append(rep.Corrupt, id)
	}
	slices.Sort(rep.Corrupt)
	rep.Elapsed = time.Since(start)
	// Only a scan that looked at every thread may replace the quarantine,
	// which is how a repaired thread leaves it. An interrupted one returned
	// above and left the set as the last complete scan and this one's finds
	// made it.
	w.mu.Lock()
	w.quarantined = corrupt
	w.quarantinedN.Store(int64(len(w.quarantined)))
	w.last = &rep
	w.mu.Unlock()

	w.log.Info("projection catch-up finished",
		"scanned", rep.Scanned, "closed", rep.Closed, "bytes", rep.Bytes,
		"corrupt", len(rep.Corrupt), "failed", len(rep.Failed), "unfinished", len(rep.Unfinished), "strays", len(rep.Strays),
		"elapsed", rep.Elapsed)
	return rep, nil
}

// logCorrupt is the error line for a log shorter than its projection.
func (w *Writer) logCorrupt(id threadid.ID, logLength, at int64) {
	w.log.Error("a thread's log is shorter than its projection; it is quarantined and served as 503 "+
		"until repaired. A crash cannot cause this, because the projection is written after the log, "+
		"unless storage.fsync is \"never\", where a crash can lose log writes the database kept",
		"thread", id, "log_length", logLength, "projected", at)
}

// quarantine adds a thread to the quarantine outside a catch-up, for a retry
// that finds the log shorter than its projection.
func (w *Writer) quarantine(id threadid.ID, logLength, at int64) {
	w.mu.Lock()
	if w.quarantined == nil {
		w.quarantined = map[threadid.ID]bool{}
	}
	w.quarantined[id] = true
	w.quarantinedN.Store(int64(len(w.quarantined)))
	w.drop(id)
	w.mu.Unlock()
	w.logCorrupt(id, logLength, at)
}

// Quarantined reports whether thread id's log was found shorter than its
// projection by the last complete catch-up, or by any sweep or retry since. Such a thread is served as 503 until an
// operator repairs it (docs/03).
func (w *Writer) Quarantined(id threadid.ID) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.quarantined[id]
}

// QuarantinedIDs is the threads Quarantined reports, in no order, for GET
// /me to take out of its unread counts what no list shows
// (TKT-01M29CTY7K). It is for the process's own services: the health check
// still gets only the count.
func (w *Writer) QuarantinedIDs() []threadid.ID {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]threadid.ID, 0, len(w.quarantined))
	for id, q := range w.quarantined {
		if q {
			out = append(out, id)
		}
	}
	return out
}

// QuarantinedCount is how many threads are quarantined, for the health
// check and the metrics endpoint. Only the count leaves the package: which
// threads they are is for the log and `ketju fsck`, not for an
// unauthenticated endpoint. It takes no lock.
func (w *Writer) QuarantinedCount() int {
	return int(w.quarantinedN.Load())
}

// LastCatchUp is the report of the last catch-up that finished, for the
// health and metrics endpoints; false before the first.
func (w *Writer) LastCatchUp() (Report, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.last == nil {
		return Report{}, false
	}
	return *w.last, true
}
