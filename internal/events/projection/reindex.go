package projection

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// ReindexProgress is how far a reindex has got. Done and Bytes count the
// threads looked at so far, projected or not, out of Total and TotalBytes,
// which the walk found before anything was projected.
type ReindexProgress struct {
	Done, Total       int
	Bytes, TotalBytes int64
	Elapsed           time.Duration
}

// ReindexFailure is a thread reindex could not project.
type ReindexFailure struct {
	Thread threadid.ID
	Err    error
}

// ReindexReport is what one reindex did.
type ReindexReport struct {
	// Threads is how many logs the walk found, and Projected how many of them
	// were projected, replaying Bytes of log.
	Threads   int
	Projected int
	Bytes     int64

	// Torn are threads whose log ends in a torn final frame, an interrupted
	// write. Each was projected to the end of its last whole frame, and is
	// counted in Projected if it has one; serve cuts the torn bytes off when
	// it next opens the thread, and its catch-up then finds no gap.
	Torn []threadid.ID

	// Unfinished are logs holding only their file header, a creation that
	// never finished, which have nothing to project. Catch-up reports them
	// the same way.
	Unfinished []threadid.ID

	// Failed are threads whose log cannot be projected: damage before the
	// last frame, a header that is damaged or names another thread, or
	// records that contradict each other (IsLogFault). They have no
	// projection rows. A failure that is not the log's stops the run instead.
	Failed []ReindexFailure

	// Strays are files under threads/ that are not a thread's log or index
	// at the path its id names.
	Strays []string

	Elapsed time.Duration
}

// Reindex rebuilds every projection table from the thread logs, which is the
// recovery path for a lost or damaged database (docs/03 "Durability and
// recovery"). It walks threads/ first, so a walk that fails changes nothing;
// then it empties every projection table in one transaction and projects
// each log from its first frame to its end, one transaction per thread, in
// id order. progress, if not nil, is called once before the first thread and
// once after each.
//
// It writes only the database. Logs are opened read-only, as everywhere in
// this package, so a torn tail is projected around rather than cut.
//
// Reindex is for a data directory no server is running on: the caller holds
// the directory lock. A run that stops partway, by error or crash, leaves
// every finished thread projected and the rest with no projection_state row,
// which the next reindex, or serve's catch-up, replays from the start.
//
// The error is for a run that could not go on: the walk, the clear, a
// cancelled context, or a thread that failed for a reason other than its
// log, such as a database write or a log that could not be read. What the
// run found wrong with a log is in the report, and the run goes on past it.
func (w *Writer) Reindex(ctx context.Context, progress func(ReindexProgress)) (ReindexReport, error) {
	start := time.Now()
	var rep ReindexReport
	w.sweeping.Lock()
	defer w.sweeping.Unlock()

	logs, strays, err := w.walk()
	if err != nil {
		return rep, fmt.Errorf("projection: reindex: %w", err)
	}
	rep.Threads, rep.Strays = len(logs), strays
	ids := make([]threadid.ID, 0, len(logs))
	var total int64
	for id, size := range logs {
		ids = append(ids, id)
		total += size
	}
	slices.Sort(ids)

	if err := w.db.Write(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		return db.ClearProjections(ctx, q)
	}); err != nil {
		return rep, fmt.Errorf("projection: reindex: emptying the projection tables: %w", err)
	}
	// Nothing this writer held is true of the emptied tables.
	w.mu.Lock()
	clear(w.behind)
	w.quarantined = nil
	w.mu.Unlock()

	p := ReindexProgress{Total: len(ids), TotalBytes: total}
	report := func() {
		if progress != nil {
			p.Elapsed = time.Since(start)
			progress(p)
		}
	}
	report()
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			rep.Elapsed = time.Since(start)
			return rep, err
		}
		size := logs[id]
		n, torn, err := w.reindexThread(ctx, id, size)
		switch {
		case err != nil && !IsLogFault(err):
			rep.Elapsed = time.Since(start)
			return rep, fmt.Errorf("projection: reindex: %w", err)
		case err != nil:
			rep.Failed = append(rep.Failed, ReindexFailure{Thread: id, Err: err})
		case torn:
			// Before the header-only case: a log whose creation frame was
			// torn is a torn write, and serve has bytes to cut.
			rep.Torn = append(rep.Torn, id)
		case n == threadlog.FileHeaderSize:
			rep.Unfinished = append(rep.Unfinished, id)
		}
		if err == nil && n > threadlog.FileHeaderSize {
			rep.Projected++
			rep.Bytes += n - threadlog.FileHeaderSize
		}
		p.Done++
		p.Bytes += size
		report()
	}
	rep.Elapsed = time.Since(start)
	return rep, nil
}

// reindexThread projects one log of size bytes into emptied tables, and
// returns the length it projected to and whether a torn tail was left out.
// An error the log explains is a logFault (IsLogFault).
func (w *Writer) reindexThread(ctx context.Context, id threadid.ID, size int64) (int64, bool, error) {
	switch {
	case size < threadlog.FileHeaderSize:
		// ProjectThread would see a target below its start and do nothing.
		return 0, false, logFault{fmt.Errorf("%w: %d bytes", threadlog.ErrLogTooShort, size)}
	case size == threadlog.FileHeaderSize:
		// Nothing to project, but the header is still checked: a damaged
		// one, or one naming another thread, is not an unfinished creation.
		return size, false, w.checkLogHeader(id)
	}
	_, err := w.ProjectThread(ctx, id, size)
	if err == nil {
		return size, false, nil
	}
	if ctx.Err() != nil || !IsLogFault(err) {
		return 0, false, err
	}

	// The one failure with a projection still to make is a torn final frame,
	// which an interrupted append leaves and serve cuts off at its next open.
	// Opening the log without an index replays it and tells that apart from
	// damage, and it reads only.
	valid, ok := w.validPrefix(id, size)
	if !ok || valid == size {
		return 0, false, err
	}
	if valid == threadlog.FileHeaderSize {
		return valid, true, nil // the torn frame was the thread's creation
	}
	if _, perr := w.ProjectThread(ctx, id, valid); perr != nil {
		if !IsLogFault(perr) {
			return 0, false, perr // the database's, and the run stops
		}
		return 0, false, errors.Join(err, perr)
	}
	return valid, true, nil
}

// checkLogHeader reads and checks the file header of thread id's log.
func (w *Writer) checkLogHeader(id threadid.ID) error {
	f, err := w.open(id.LogPath(w.root))
	if err != nil {
		return err
	}
	defer f.Close()
	return checkHeader(f, id)
}

// validPrefix is the length of the log up to the end of its last whole
// frame, when everything before a torn tail is intact; false when the log
// has damage a torn write does not explain, or could not be read.
func (w *Writer) validPrefix(id threadid.ID, size int64) (int64, bool) {
	f, err := w.open(id.LogPath(w.root))
	if err != nil {
		return 0, false
	}
	defer f.Close()
	rec, err := threadlog.Open(f, size, nil)
	if err != nil || rec.Header.ThreadID != uint64(id) {
		return 0, false
	}
	return rec.LogLength, true
}
