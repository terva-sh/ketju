// Package projection is the subscriber that turns thread-log records into
// database rows: threads, thread_tags, post_meta and mentions, with
// projection_state recording how far each thread has been projected
// (docs/03 "Write path", docs/04 "Projection tables", TKT-01M29C2WW5).
//
// The log is the source of truth and the database catches up to it. Nothing
// here writes to a log: logs are opened read-only, and a test holds that.
//
// # Idempotence
//
// A thread is projected a range at a time, in one database transaction that
// reads projection_state.log_length, applies every frame from there to the
// target, and records the target. A frame below the recorded length is never
// applied again, which is the (thread, log offset) key docs/03 names. And
// every apply sets absolute values, so a range applied twice anyway, as a
// reindex from the start of the log does, converges on the same rows.
//
// # Failure
//
// A thread whose projection fails is kept, not dropped: it joins a behind set
// that every later delivery and Retry try again, and Lag reports it. Deliver
// does not return the failure to the bus, because the bus would count the
// event as dropped and every later batch would carry a hole this writer does
// not have.
package projection

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/terva-sh/ketju/internal/events"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// Options configures a Writer.
type Options struct {
	// DB is the database the projection tables live in.
	DB *db.DB

	// Root is the threads directory, as threadid.ID.LogPath takes it.
	Root string

	// Sweep catches up every thread from durable state. Deliver calls it when
	// a batch reports dropped events, because those are threads this writer
	// was never told about. Startup catch-up (TKT-01M29C5Y1S) supplies it.
	//
	// Required. Without it a drop could only be logged, and the threads it
	// named would stay stale until the next restart with nothing reporting
	// them as behind (review 389).
	Sweep func(ctx context.Context) error

	// Logger receives the lag transitions. Nil means slog.Default.
	Logger *slog.Logger
}

// Writer is the projection subscriber. It is safe for concurrent use: the
// bus, the Run loop and catch-up may all project at once, and the database's
// single write connection orders them.
type Writer struct {
	db    *db.DB
	root  string
	sweep func(ctx context.Context) error
	log   *slog.Logger

	// Seams for tests: the clock stamped into projection_state.updated_at,
	// how a log is opened, and a hook that sees every frame applied.
	now     func() time.Time
	open    func(path string) (logFile, error)
	applied func(threadid.ID, int64)

	// sweeping serializes CatchUp and Retry. Both read and then rewrite the
	// behind set and the quarantine on the strength of what they read, and
	// two of them interleaving could each undo the other's result (review
	// 464). Deliver is not serialized with them: it only adds to the behind
	// set, and hold's max keeps a newer target.
	sweeping sync.Mutex

	mu     sync.Mutex
	behind map[threadid.ID]*lag

	// quarantined is the set the last CatchUp found corrupt, and last is
	// its report. reopen is the Recover that CatchUp was last given, which
	// Retry opens held threads through.
	quarantined map[threadid.ID]bool
	last        *Report
	reopen      Recover
}

// logFile is what the writer needs of an open log.
type logFile interface {
	io.ReaderAt
	io.Closer
	Stat() (os.FileInfo, error)
}

type lag struct {
	target    int64 // the longest log length an event has reported
	projected int64 // how far the projection is known to have got
}

// Lag is how far the projection is behind the logs it has been told about.
type Lag struct {
	Threads int
	Bytes   int64
}

// New returns a Writer, or an error naming the option that is missing.
func New(opts Options) (*Writer, error) {
	switch {
	case opts.DB == nil:
		return nil, errors.New("projection: Options.DB is nil")
	case opts.Root == "":
		return nil, errors.New("projection: Options.Root is empty")
	case opts.Sweep == nil:
		return nil, errors.New("projection: Options.Sweep is nil; dropped events could not be recovered")
	}
	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}
	return &Writer{
		db: opts.DB, root: opts.Root, sweep: opts.Sweep, log: log,
		now:    time.Now,
		open:   func(path string) (logFile, error) { return os.OpenFile(path, os.O_RDONLY, 0) },
		behind: make(map[threadid.ID]*lag),
	}, nil
}

// Name identifies the writer on the bus.
func (w *Writer) Name() string { return "projection" }

// Deliver projects each thread in the batch up to the longest log length its
// events report, and retries every thread already behind. One thread failing
// does not stop the others, and a failure is held for retry rather than
// returned; see the package comment.
//
// A batch reporting dropped events runs Sweep first. A failed sweep is the
// one error Deliver returns, so the bus reports the hole again and the sweep
// is retried; the batch's own events are projected either way.
func (w *Writer) Deliver(ctx context.Context, b events.Batch) error {
	var sweepErr error
	if b.Dropped > 0 {
		if err := w.sweep(ctx); err != nil {
			sweepErr = fmt.Errorf("projection: sweeping after %d dropped events: %w", b.Dropped, err)
		}
	}

	var order []threadid.ID
	targets := make(map[threadid.ID]int64)
	for _, ev := range b.Events {
		if _, seen := targets[ev.Thread]; !seen {
			order = append(order, ev.Thread)
		}
		targets[ev.Thread] = max(targets[ev.Thread], ev.LogLength)
	}
	w.mu.Lock()
	for id, l := range w.behind {
		if _, seen := targets[id]; !seen {
			order = append(order, id)
		}
		targets[id] = max(targets[id], l.target)
	}
	w.mu.Unlock()

	for _, id := range order {
		w.project(ctx, id, targets[id])
	}
	return sweepErr
}

// Retry projects every thread that is behind. It returns an error joining
// the failures, for Run's backoff; the threads stay behind either way.
//
// Once a catch-up has run, each thread is reopened through its Recover
// first, and projected to the length that reports rather than to the target
// it was held at: a log held because it could not be opened may end in a
// torn frame, which the open truncates and projection refuses (review 453).
// A thread the reopen shows has nothing left to project leaves the behind
// set through project, which finds it already there; one it shows shorter
// than its projection is quarantined.
func (w *Writer) Retry(ctx context.Context) error {
	w.sweeping.Lock()
	defer w.sweeping.Unlock()
	w.mu.Lock()
	type held struct{ target, projected int64 }
	targets := make(map[threadid.ID]held, len(w.behind))
	for id, l := range w.behind {
		targets[id] = held{l.target, l.projected}
	}
	reopen := w.reopen
	w.mu.Unlock()
	var errs []error
	for id, h := range targets {
		target := h.target
		if reopen != nil {
			n, err := reopen(ctx, id)
			if err != nil {
				errs = append(errs, fmt.Errorf("projection: reopening thread %s: %w", id, err))
				continue
			}
			switch {
			case n < h.projected:
				w.quarantine(id, n, h.projected)
				continue
			}
			// The reopened length is the true one. It replaces the held
			// target, which may count torn bytes the open has removed, but
			// only if nothing has held the thread again since the snapshot:
			// an append whose projection failed meanwhile holds a newer
			// target, which is kept (review 464).
			target = n
			w.mu.Lock()
			if b := w.behind[id]; b != nil {
				if b.target == h.target {
					b.target = n
				} else {
					b.target = max(b.target, n)
					target = b.target
				}
			}
			w.mu.Unlock()
		}
		errs = append(errs, w.project(ctx, id, target))
	}
	return errors.Join(errs...)
}

// settle takes a thread out of the behind set without projecting it, for a
// thread a look at its log showed has nothing to project: its log was
// observed bytes long and the projection already there.
//
// That look may be stale by now. An append since, whose projection failed,
// holds the thread again with a newer target, and removing that would lose
// the new bytes from Retry and Lag (review 462). A log only grows, apart
// from a torn tail being cut, so the log is looked at again under the lock
// that orders holds: if it is no longer than observed, whatever is held is
// older than the look, a torn tail included, and is cleared; if it has
// grown, the entry stays.
func (w *Writer) settle(id threadid.ID, observed int64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, ok := w.behind[id]; !ok {
		return
	}
	if fi, err := os.Stat(id.LogPath(w.root)); err == nil && fi.Size() > observed {
		return
	}
	w.drop(id)
}

// drop removes a thread from the behind set unconditionally, for one that is
// quarantined: its log is shorter than its projection, which no later
// append undoes. Called with mu held.
func (w *Writer) drop(id threadid.ID) {
	if _, ok := w.behind[id]; !ok {
		return
	}
	delete(w.behind, id)
	if len(w.behind) == 0 {
		w.log.Info("projection caught up")
	}
}

// retryFloor and retryCeiling bound Run's backoff. Variables so a test need
// not wait a second.
var retryFloor, retryCeiling = time.Second, time.Minute

// Run retries threads that are behind until ctx ends, backing off from one
// second to a minute while retries keep failing.
func (w *Writer) Run(ctx context.Context) {
	floor, ceiling := retryFloor, retryCeiling
	wait := floor
	for {
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}
		if w.Lag().Threads == 0 {
			wait = floor
			continue
		}
		if err := w.Retry(ctx); err != nil {
			wait = min(wait*2, ceiling)
		} else {
			wait = floor
		}
	}
}

// hold puts a thread in the behind set without trying it, for a gap that
// could not even be opened: Run retries it and Lag reports it.
func (w *Writer) hold(id threadid.ID, projected, target int64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	was := len(w.behind) > 0
	b := w.behind[id]
	if b == nil {
		b = &lag{projected: projected}
		w.behind[id] = b
	}
	b.target = max(b.target, target)
	if !was {
		w.log.Warn("projection is behind", "threads", len(w.behind))
	}
}

// Lag reports the threads that are behind and by how many bytes.
func (w *Writer) Lag() Lag {
	w.mu.Lock()
	defer w.mu.Unlock()
	var l Lag
	for _, b := range w.behind {
		l.Threads++
		l.Bytes += b.target - b.projected
	}
	return l
}

// project runs ProjectThread and keeps the behind set and its log lines in
// step with the outcome.
func (w *Writer) project(ctx context.Context, id threadid.ID, target int64) error {
	projected, err := w.ProjectThread(ctx, id, target)

	w.mu.Lock()
	defer w.mu.Unlock()
	wasBehind := len(w.behind) > 0
	if err == nil {
		if b := w.behind[id]; b == nil || projected >= b.target {
			delete(w.behind, id)
		} else {
			b.projected = projected // a later event moved the target on
		}
	} else {
		b := w.behind[id]
		if b == nil {
			b = &lag{projected: threadlog.FileHeaderSize}
			w.behind[id] = b
		}
		b.target = max(b.target, target)
		if projected > b.projected {
			b.projected = projected
		}
		w.log.Error("projecting a thread failed; it will be retried", "thread", id, "err", err)
	}
	switch isBehind := len(w.behind) > 0; {
	case isBehind && !wasBehind:
		w.log.Warn("projection is behind", "threads", len(w.behind))
	case !isBehind && wasBehind:
		w.log.Info("projection caught up")
	}
	return err
}

// ProjectThread projects thread id up to upTo, a log length an append
// reported, and returns how far the thread is now projected. On an error
// nothing from this call is kept, and the returned length is where the
// projection stood before it, when that could be read.
//
// Startup catch-up and reindex (TKT-01M29C5Y1S, TKT-01M29C5Y25) call this
// directly with the log's length.
func (w *Writer) ProjectThread(ctx context.Context, id threadid.ID, upTo int64) (int64, error) {
	var from int64 = -1
	err := w.db.Write(ctx, func(q *dbq.Queries, tx *sql.Tx) error {
		st, err := q.GetProjectionState(ctx, int64(id))
		switch {
		case errors.Is(err, sql.ErrNoRows):
			from = threadlog.FileHeaderSize
		case err != nil:
			return err
		default:
			from = st.LogLength
		}
		if upTo <= from {
			return nil // already there; an event can arrive after a sweep
		}

		f, err := w.open(id.LogPath(w.root))
		if err != nil {
			return err
		}
		defer f.Close()
		fi, err := f.Stat()
		if err != nil {
			return err
		}
		if fi.Size() < upTo {
			return fmt.Errorf("the log is %d bytes, shorter than the %d an event reported", fi.Size(), upTo)
		}
		if err := checkHeader(f, id); err != nil {
			return err
		}

		var applyErr error
		scanErr := threadlog.ScanFramesFrom(f, from, upTo, func(rec threadlog.ScanRecord) bool {
			applyErr = w.apply(ctx, q, id, rec)
			return applyErr == nil
		})
		if err := errors.Join(scanErr, applyErr); err != nil {
			return err
		}
		// A log whose records never created the thread row has not been
		// projected, whatever its frames said (review 390).
		if n, err := q.SetThreadProjected(ctx, dbq.SetThreadProjectedParams{LogLengthProjected: upTo, ID: int64(id)}); err != nil {
			return err
		} else if n != 1 {
			return fmt.Errorf("the log up to %d never created the thread", upTo)
		}
		return q.SetProjectionState(ctx, dbq.SetProjectionStateParams{
			ThreadID: int64(id), LogLength: upTo, UpdatedAt: w.now().UnixMilli(),
		})
	})
	if err != nil {
		return from, fmt.Errorf("projection: thread %s: %w", id, err)
	}
	return max(from, upTo), nil
}

// checkHeader reads the log's file header and refuses a damaged one, a
// version this build cannot read, or a log that names another thread: a file
// at the wrong path would otherwise project its rows under this thread's id
// (review 390).
func checkHeader(r io.ReaderAt, id threadid.ID) error {
	b := make([]byte, threadlog.FileHeaderSize)
	if _, err := r.ReadAt(b, 0); err != nil {
		return fmt.Errorf("reading the file header: %w", err)
	}
	h, err := threadlog.ParseFileHeader(b)
	if err != nil {
		return err
	}
	if err := h.CheckVersion(); err != nil {
		return err
	}
	if h.ThreadID != uint64(id) {
		return fmt.Errorf("the log at this thread's path names thread %s", threadid.ID(h.ThreadID))
	}
	return nil
}

// apply projects one scanned frame.
func (w *Writer) apply(ctx context.Context, q *dbq.Queries, id threadid.ID, rec threadlog.ScanRecord) error {
	if rec.Err != nil {
		// Damage is never skipped: projecting past a frame that fails its
		// checksum would project whatever the log says after it as if the
		// damaged record had not happened.
		return fmt.Errorf("frame at offset %d: %w", rec.Offset, rec.Err)
	}
	body, err := threadlog.DecodeBody(rec.Header.Type, rec.Header.Encoding, rec.Body)
	if err != nil {
		if threadlog.SkipUnknown(err) {
			return nil // advisory, or reserved: stepped over as the index does
		}
		return fmt.Errorf("frame at offset %d: %w", rec.Offset, err)
	}
	d, ok := decisions[rec.Header.Type]
	if !ok || d.apply == nil {
		// A body type with no decision is a catalogue entry nobody wrote a
		// projection for. TestEveryRecordTypeHasADecision should have caught
		// it; refusing here keeps it from being skipped quietly in production.
		return fmt.Errorf("frame at offset %d: %s has no projection", rec.Offset, rec.Header.Type)
	}
	f := frame{thread: id, offset: rec.Offset, end: rec.Offset + int64(rec.Size), h: rec.Header, body: body}
	if err := d.apply(ctx, q, f); err != nil {
		return fmt.Errorf("projecting %s at offset %d: %w", rec.Header.Type, rec.Offset, err)
	}
	if w.applied != nil {
		w.applied(id, rec.Offset)
	}
	return nil
}
