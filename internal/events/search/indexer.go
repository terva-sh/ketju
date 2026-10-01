// Package search is the subscriber that keeps the full-text index in step
// with the thread logs (docs/04 "search (FTS)", docs/03 "Write path" step 5,
// TKT-01M29CRJ3B).
//
// The index holds only what every reader of a thread may see: one row per
// visible post, at its current revision, run through render.Plain, and the
// thread's title as a row of its own at ordinal 0. A post that stops being
// visible, whatever hid it, loses its row, and one restored gets it back. The
// API checks every hit again when it serves it (TKT-01M29CTY8T); this package
// makes sure the index never holds what that check would have to hide.
//
// # How a thread is indexed
//
// search_state records how far into each log the index has got. Indexing a
// thread scans the frames after that, up to a length an append reported, and
// collects the ordinals of the records that can change a post's text or its
// visibility (decisions.go) and the last title. It then reads the thread's
// index entry for each such ordinal, from a snapshot at or past that length,
// and puts or drops the post's row from it. Every value is absolute, so a
// snapshot newer than the scan is harmless: a later scan writes the same
// values again. search_state moves to the end of the frames scanned and
// never past it, in the same transaction as the rows (Terva review 1695).
//
// A thread with no search_state, one whose recorded length is past its log's
// end, and one whose recorded length is not a frame boundary is indexed from
// its first frame, after every row and the search_state it had are deleted
// in the same transaction, so an ordinal the log no longer holds keeps no row
// (Terva reviews 1728 and 1731).
//
// # Batches
//
// Deliver indexes every thread in a batch in as few write transactions as the
// limits allow: it commits at a thread boundary once a transaction holds 256
// posts or 1 MiB of text, so the single write connection is never held for
// long and a burst of posts is a few transactions rather than one each. Each
// thread runs inside its own savepoint, and one that fails rolls back to it,
// keeping its earlier rows and search_state while the others commit. A failed
// thread joins a behind set, which Lag reports and later deliveries and Run
// retry.
//
// Logs are opened read-only, and the thread cache is only peeked, never
// acquired, so indexing never writes a log or index and never evicts a
// thread a reader has open.
package search

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/terva-sh/ketju/internal/events"
	"github.com/terva-sh/ketju/internal/render"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadcache"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// schemaVersion numbers the index's shape: its columns and its tokenizer
// (migration 0022). Bump it with any change to either that a rebuild must
// follow.
const schemaVersion = 1

// Version is what search_meta records: the schema and the plain-text
// extraction that built the index. An index of another version, or of none,
// is emptied and rebuilt (Prepare).
const Version int64 = schemaVersion<<16 | render.PlainVersion

// Batch limits: a transaction commits at the next thread boundary once it
// holds this many posts or this much text.
const (
	batchPosts = 256
	batchBytes = 1 << 20
)

// Options configures an Indexer.
type Options struct {
	// DB is the database the search tables live in.
	DB *db.DB

	// Root is the data directory, as threadid.ID.LogPath takes it.
	Root string

	// Threads is the open-thread cache. A thread open in it is read from its
	// index in memory; one that is not is opened read-only from disk. It is
	// only peeked, never acquired. Nil opens every thread from disk.
	Threads *threadcache.Cache

	// Quarantined reports a thread the projection found corrupt. It is not
	// indexed until it leaves the quarantine. Nil quarantines nothing.
	Quarantined func(threadid.ID) bool

	// Logger receives the lag transitions. Nil means slog.Default.
	Logger *slog.Logger

	// Now stamps search_state.updated_at. Nil means time.Now.
	Now func() time.Time
}

// Lag is how far the index is behind the logs it knows about.
type Lag struct {
	Threads int
	Bytes   int64
}

// Indexer is the search subscriber. It is safe for concurrent use.
type Indexer struct {
	db          *db.DB
	root        string
	threads     *threadcache.Cache
	quarantined func(threadid.ID) bool
	log         *slog.Logger
	now         func() time.Time

	// Seams for tests: how a log is opened, how its sidecar index is read,
	// the batch limits, and a hook run before each indexing transaction of
	// a run, given its number from 0.
	open      func(path string) (logFile, error)
	readIndex func(path string) ([]byte, error)
	maxPosts  int
	maxBytes  int64
	beforeTx  func(ctx context.Context, n int)

	// write serializes indexing transactions, so Deliver, CatchUp and Retry
	// never index one thread at once. sweeping serializes CatchUp.
	write    sync.Mutex
	sweeping sync.Mutex

	mu     sync.Mutex
	behind map[threadid.ID]*lag

	indexing atomic.Bool

	// sweepOwed is set until a CatchUp finishes, and again when one fails
	// before it finishes, so Run sweeps until one does.
	sweepOwed atomic.Bool

	// walk lists the logs under a data directory; a seam for tests.
	walk func(root string) (map[threadid.ID]int64, []string, error)
}

type lag struct {
	target  int64 // the longest log length known for the thread
	indexed int64 // how far the index is known to have got
}

// logFile is what the indexer needs of an open log.
type logFile interface {
	io.ReaderAt
	io.Closer
	Stat() (os.FileInfo, error)
}

// New returns an Indexer, or an error naming the option that is missing.
func New(opts Options) (*Indexer, error) {
	switch {
	case opts.DB == nil:
		return nil, errors.New("search: Options.DB is nil")
	case opts.Root == "":
		return nil, errors.New("search: Options.Root is empty")
	}
	x := &Indexer{
		db: opts.DB, root: opts.Root, threads: opts.Threads, quarantined: opts.Quarantined,
		log: opts.Logger, now: opts.Now,
		open:      func(path string) (logFile, error) { return os.OpenFile(path, os.O_RDONLY, 0) },
		readIndex: readOnly,
		maxPosts:  batchPosts, maxBytes: batchBytes,
		behind: make(map[threadid.ID]*lag),
	}
	if x.quarantined == nil {
		x.quarantined = func(threadid.ID) bool { return false }
	}
	if x.log == nil {
		x.log = slog.Default()
	}
	if x.now == nil {
		x.now = time.Now
	}
	// Until a catch-up has finished, threads may trail their logs and search
	// may be missing results.
	x.indexing.Store(true)
	x.sweepOwed.Store(true)
	x.walk = threadid.WalkLogs
	return x, nil
}

// readOnly reads a file it opens read-only; a missing file reads as nothing.
func readOnly(path string) ([]byte, error) {
	f, err := os.OpenFile(path, os.O_RDONLY, 0)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(f)
}

// Name identifies the indexer on the bus.
func (x *Indexer) Name() string { return "search" }

// Indexing reports whether search may be missing results because threads
// trail their logs: true from New until a CatchUp finishes with no thread
// left behind, and again from a Prepare that emptied the index. A catch-up
// that leaves threads behind, because they failed or more was appended
// while it ran, leaves it true until the behind set first drains (Terva
// review 1753). A thread falling behind afterwards, as appends do, does not
// set it again.
func (x *Indexer) Indexing() bool { return x.indexing.Load() }

// Prepare settles the index's version before anything can search it. A
// search_meta version other than Version, or none, empties the whole index
// and records Version in one transaction, so no row an older extraction
// built survives to be served. Every thread then trails, and CatchUp
// re-indexes it from its first frame; a restart mid-pass resumes thread by
// thread from search_state, and one after the pass re-indexes nothing
// (Terva reviews 1707, 1713 and 1719).
//
// serve calls it before its listener opens. `ketju reindex --search` does
// not: Rebuild settles the version inside its own transaction, so a failed
// rebuild never commits an emptied index (Terva review 1731). With the
// version current it is one read.
func (x *Indexer) Prepare(ctx context.Context) error {
	v, have, err := x.version(ctx)
	if err != nil {
		return fmt.Errorf("search: reading the index version: %w", err)
	}
	if have && v == Version {
		return nil
	}
	x.write.Lock()
	defer x.write.Unlock()
	reset := false
	err = x.db.Write(ctx, func(q *dbq.Queries, tx *sql.Tx) error {
		v, err := q.GetSearchVersion(ctx)
		switch {
		case err == nil && v == Version:
			return nil // another Prepare got here first
		case err != nil && !errors.Is(err, sql.ErrNoRows):
			return err
		}
		if err := db.ClearSearch(ctx, tx); err != nil {
			return err
		}
		reset = true
		return q.SetSearchVersion(ctx, Version)
	})
	if err != nil {
		return fmt.Errorf("search: emptying the index for version %d: %w", Version, err)
	}
	if reset {
		x.mu.Lock()
		clear(x.behind)
		x.mu.Unlock()
		x.indexing.Store(true)
		x.sweepOwed.Store(true)
		if have {
			x.log.Info("the search index was built by another version; it is emptied and every thread will be re-indexed",
				"was", v, "now", Version)
		} else {
			x.log.Info("the search index is empty; every thread will be indexed", "version", Version)
		}
	}
	return nil
}

func (x *Indexer) version(ctx context.Context) (v int64, have bool, err error) {
	err = x.db.Read(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		v, err = q.GetSearchVersion(ctx)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		have = err == nil
		return err
	})
	return v, have, err
}

// Deliver indexes each thread in the batch up to the longest log length its
// events report, and retries every thread already behind. A failure is held
// for retry rather than returned, as the projection writer holds one: the
// bus would count the event as dropped and every later batch would carry a
// hole this indexer does not have.
//
// A batch reporting dropped events runs CatchUp first, and a failed CatchUp
// is the one error Deliver returns, so the bus reports the hole again.
func (x *Indexer) Deliver(ctx context.Context, b events.Batch) error {
	var sweepErr error
	if b.Dropped > 0 {
		if err := x.CatchUp(ctx); err != nil {
			sweepErr = fmt.Errorf("search: catching up after %d dropped events: %w", b.Dropped, err)
		}
	}

	var order []threadid.ID
	lengths := map[threadid.ID]int64{}
	for _, ev := range b.Events {
		if _, seen := lengths[ev.Thread]; !seen {
			order = append(order, ev.Thread)
		}
		lengths[ev.Thread] = max(lengths[ev.Thread], ev.LogLength)
	}
	x.mu.Lock()
	for id, l := range x.behind {
		if _, seen := lengths[id]; !seen {
			order = append(order, id)
		}
		lengths[id] = max(lengths[id], l.target)
	}
	x.mu.Unlock()

	targets := make([]target, 0, len(order))
	for _, id := range order {
		if x.quarantined(id) {
			x.forget(id)
			continue
		}
		targets = append(targets, target{id: id, upTo: lengths[id], lag: lengths[id]})
	}
	x.holdAll(ctx, targets)
	x.index(ctx, targets)
	return sweepErr
}

// IndexThread indexes thread id up to logLength, a length an append
// reported; zero or less means the log's length now. It is Deliver for one
// thread, and returns that thread's failure.
func (x *Indexer) IndexThread(ctx context.Context, id threadid.ID, logLength int64) error {
	t := target{id: id, upTo: logLength, lag: logLength}
	x.holdAll(ctx, []target{t})
	rs := x.index(ctx, []target{t})
	if len(rs) == 0 {
		return ctx.Err()
	}
	return rs[0].err
}

// CatchUp indexes every thread whose log has grown past its search_state,
// and every thread with no search_state, from the directory walk the
// projection's catch-up uses. A quarantined thread is skipped. It assumes
// Prepare has run.
//
// The error is for a catch-up that could not look, or was cancelled; a
// thread that failed is in the behind set, which Lag reports and Run
// retries. A catch-up that returns an error leaves the sweep owed, and Run
// runs it again until one finishes (Terva review 1750).
func (x *Indexer) CatchUp(ctx context.Context) (err error) {
	x.sweeping.Lock()
	defer x.sweeping.Unlock()
	start := time.Now()
	defer func() {
		if err != nil {
			x.sweepOwed.Store(true)
		}
	}()

	indexed := map[threadid.ID]int64{}
	err = x.db.Read(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		rows, err := q.ListSearchLengths(ctx)
		for _, r := range rows {
			indexed[threadid.ID(r.ThreadID)] = r.LogLength
		}
		return err
	})
	if err != nil {
		return fmt.Errorf("search: catch-up: reading search_state: %w", err)
	}
	logs, _, err := x.walk(x.root)
	if err != nil {
		return fmt.Errorf("search: catch-up: %w", err)
	}
	ids := make([]threadid.ID, 0, len(logs))
	for id := range logs {
		ids = append(ids, id)
	}
	slices.Sort(ids)

	var targets []target
	for _, id := range ids {
		size := logs[id]
		at, known := indexed[id]
		switch {
		case x.quarantined(id):
			x.forget(id)
			continue
		case !known && size < threadlog.FileHeaderSize:
			// Shorter than a header: nothing a reader can open. A log that
			// holds just its header is indexed, so rows left for it go
			// (Terva review 1755).
			continue
		case known && at == size:
			// Only the lag this snapshot covers is cleared: a Deliver that
			// failed after the walk may hold the thread at a later length
			// (Terva review 1753).
			x.forgetUpTo(id, size)
			continue
		}
		if !known {
			at = threadlog.FileHeaderSize
		}
		x.hold(id, at, size)
		targets = append(targets, target{id: id, lag: size})
	}
	rs := x.index(ctx, targets)
	if err := ctx.Err(); err != nil {
		return err
	}
	failed := 0
	for _, r := range rs {
		if r.err != nil {
			failed++
		}
	}
	x.sweepOwed.Store(false)
	x.mu.Lock()
	if len(x.behind) == 0 {
		x.indexing.Store(false)
	} // otherwise transition clears it when the behind set drains
	x.mu.Unlock()
	x.log.Info("search catch-up finished", "scanned", len(ids), "indexed", len(rs)-failed, "failed", failed,
		"elapsed", time.Since(start))
	return nil
}

// Retry indexes every thread that is behind, to the end of its log. It
// returns an error joining the failures, for Run's backoff; the threads stay
// behind either way.
func (x *Indexer) Retry(ctx context.Context) error {
	x.mu.Lock()
	targets := make([]target, 0, len(x.behind))
	for id, l := range x.behind {
		targets = append(targets, target{id: id, lag: l.target})
	}
	x.mu.Unlock()
	// A thread quarantined since it fell behind is forgotten, as Deliver and
	// CatchUp forget one, so a retry never indexes it (Terva review 1755).
	targets = slices.DeleteFunc(targets, func(t target) bool {
		if x.quarantined(t.id) {
			x.forget(t.id)
			return true
		}
		return false
	})
	slices.SortFunc(targets, func(a, b target) int { return cmp.Compare(a.id, b.id) })
	var errs []error
	for _, r := range x.index(ctx, targets) {
		errs = append(errs, r.err)
	}
	return errors.Join(errs...)
}

// retryFloor and retryCeiling bound Run's backoff.
var retryFloor, retryCeiling = time.Second, time.Minute

// Run keeps the index caught up until ctx ends. While a sweep is owed, from
// New until a CatchUp finishes and again after one fails, it runs CatchUp;
// otherwise it retries the threads that are behind. The first sweep starts
// at once, and while either keeps failing it backs off from one second to a
// minute. serve runs it after its listener opens.
func (x *Indexer) Run(ctx context.Context) {
	var wait time.Duration
	for {
		if wait > 0 {
			t := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				t.Stop()
				return
			case <-t.C:
			}
		}
		var err error
		switch {
		case x.sweepOwed.Load():
			if err = x.CatchUp(ctx); err != nil && ctx.Err() == nil {
				x.log.Error("the search catch-up failed; it will be run again", "err", err)
			}
		case x.Lag().Threads > 0:
			err = x.Retry(ctx)
		default:
			wait = retryFloor
			continue
		}
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			wait = min(max(wait*2, retryFloor), retryCeiling)
		} else {
			wait = retryFloor
		}
	}
}

// Lag reports the threads that are behind and by how many bytes.
func (x *Indexer) Lag() Lag {
	x.mu.Lock()
	defer x.mu.Unlock()
	var l Lag
	for _, b := range x.behind {
		l.Threads++
		l.Bytes += max(b.target-b.indexed, 0)
	}
	return l
}

// target is one thread to index: up to upTo, or to its log's end when upTo
// is zero, with lag the length Lag counts it behind.
type target struct {
	id   threadid.ID
	upTo int64
	lag  int64

	// rebuild indexes the thread for Rebuild: from its log alone, never its
	// sidecar index, and writing search_docs without the FTS5 index, which
	// Rebuild builds once before it commits.
	rebuild bool
}

// result is what indexing one target came to.
type result struct {
	target
	out outcome
	err error
}

// holdAll puts every target in the behind set before it is indexed, at the
// length its search_state records, so Lag counts it while it waits.
func (x *Indexer) holdAll(ctx context.Context, targets []target) {
	at := make([]int64, len(targets))
	_ = x.db.Read(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		for i, t := range targets {
			n, err := q.GetSearchState(ctx, int64(t.id))
			switch {
			case err == nil:
				at[i] = n
			case errors.Is(err, sql.ErrNoRows):
				at[i] = threadlog.FileHeaderSize
			default:
				return err
			}
		}
		return nil
	})
	for i, t := range targets {
		if at[i] < t.lag {
			x.hold(t.id, at[i], t.lag)
		}
	}
}

// hold puts a thread in the behind set, or moves its target on.
func (x *Indexer) hold(id threadid.ID, indexed, target int64) {
	x.mu.Lock()
	defer x.mu.Unlock()
	was := len(x.behind) > 0
	b := x.behind[id]
	if b == nil {
		b = &lag{indexed: indexed}
		x.behind[id] = b
	}
	b.target = max(b.target, target)
	x.transition(was)
}

// forget takes a thread out of the behind set without indexing it.
func (x *Indexer) forget(id threadid.ID) {
	x.mu.Lock()
	defer x.mu.Unlock()
	if _, ok := x.behind[id]; !ok {
		return
	}
	was := len(x.behind) > 0
	delete(x.behind, id)
	x.transition(was)
}

// forgetUpTo takes a thread out of the behind set if the index is known to
// reach size and nothing holds it at a later length.
func (x *Indexer) forgetUpTo(id threadid.ID, size int64) {
	x.mu.Lock()
	defer x.mu.Unlock()
	b, ok := x.behind[id]
	if !ok || b.target > size {
		return
	}
	was := len(x.behind) > 0
	delete(x.behind, id)
	x.transition(was)
}

// settle keeps the behind set and its log lines in step with one result.
func (x *Indexer) settle(r result) {
	x.mu.Lock()
	defer x.mu.Unlock()
	was := len(x.behind) > 0
	b := x.behind[r.id]
	if r.err == nil {
		// Reaching the log's end clears a thread only when nothing set a
		// later target after this run was asked for: a delivery that failed
		// while this result waited to settle keeps it (Terva review 1755).
		// A target past the log's end, left by a log swapped for a shorter
		// one, clears once a run is asked for that target.
		if b == nil || r.out.indexed >= b.target || (r.out.indexed >= r.out.end && b.target <= r.lag) {
			delete(x.behind, r.id)
		} else {
			b.indexed = r.out.indexed // a later event moved the target on
		}
	} else {
		if b == nil {
			b = &lag{indexed: threadlog.FileHeaderSize}
			x.behind[r.id] = b
		}
		b.target = max(b.target, r.lag, r.upTo)
		x.log.Error("indexing a thread for search failed; it keeps its rows and will be retried",
			"thread", r.id, "err", r.err)
	}
	x.transition(was)
}

// transition logs the behind set going from empty to not, or back. Called
// with mu held.
func (x *Indexer) transition(was bool) {
	switch is := len(x.behind) > 0; {
	case is && !was:
		x.log.Warn("the search index is behind", "threads", len(x.behind))
	case !is && was:
		x.log.Info("the search index caught up")
		if !x.sweepOwed.Load() {
			x.indexing.Store(false) // the last pass's leftovers are indexed
		}
	}
}

// index indexes targets in order, in as few write transactions as the
// limits allow, each thread inside its own savepoint, and settles each
// result. It returns the results of the targets it reached; a cancelled
// context stops it at a transaction boundary.
func (x *Indexer) index(ctx context.Context, targets []target) []result {
	var all []result
	for n, i := 0, 0; i < len(targets); n++ {
		if x.beforeTx != nil {
			x.beforeTx(ctx, n)
		}
		if ctx.Err() != nil {
			return all
		}
		var done []result
		x.write.Lock()
		err := x.db.Write(ctx, func(_ *dbq.Queries, tx *sql.Tx) error {
			var posts int
			var bytes int64
			for i < len(targets) && posts < x.maxPosts && bytes < x.maxBytes {
				t := targets[i]
				out, threadErr, txErr := x.inSavepoint(ctx, tx, t)
				if txErr != nil {
					return txErr
				}
				i++
				done = append(done, result{target: t, out: out, err: threadErr})
				posts += out.posts
				bytes += out.bytes
			}
			return nil
		})
		x.write.Unlock()
		if err != nil {
			// Nothing in the transaction was kept. Every thread it held is
			// behind, and one it could not even begin is too.
			if len(done) == 0 && i < len(targets) {
				done = append(done, result{target: targets[i]})
				i++
			}
			for k := range done {
				done[k].err = errors.Join(done[k].err, err)
				done[k].out.indexed = done[k].out.from
			}
		}
		for _, r := range done {
			x.settle(r)
		}
		all = append(all, done...)
	}
	return all
}

// inSavepoint indexes one thread inside a savepoint of tx, rolling back to
// it on failure. threadErr is the thread's failure, which leaves tx usable;
// txErr is a failure to manage the savepoint, which does not.
func (x *Indexer) inSavepoint(ctx context.Context, tx *sql.Tx, t target) (out outcome, threadErr, txErr error) {
	if _, err := tx.ExecContext(ctx, "SAVEPOINT search_thread"); err != nil {
		return out, nil, err
	}
	out, threadErr = x.indexThread(ctx, tx, t)
	if threadErr != nil {
		if _, err := tx.ExecContext(ctx, "ROLLBACK TO search_thread"); err != nil {
			return out, threadErr, err
		}
	}
	if _, err := tx.ExecContext(ctx, "RELEASE search_thread"); err != nil {
		return out, threadErr, err
	}
	if threadErr != nil {
		out.indexed = out.from
		out.posts, out.bytes, out.docs = 0, 0, 0
	}
	return out, threadErr, nil
}

// outcome is how one thread's indexing went.
type outcome struct {
	from    int64 // where search_state stood before
	indexed int64 // where it stands now
	end     int64 // the log length the snapshot saw
	posts   int   // rows put or dropped, for the batch limit
	bytes   int64 // text put
	docs    int   // posts' rows put, the title's left out
}

// indexThread indexes thread t.id up to t.upTo, or to its log's end when
// that is zero or less, inside tx. See the package comment. A failure the
// log explains is a logFault (IsLogFault).
func (x *Indexer) indexThread(ctx context.Context, tx *sql.Tx, t target) (outcome, error) {
	id, upTo := t.id, t.upTo
	put, drop := db.PutSearchDoc, db.DropSearchDoc
	if t.rebuild {
		put, drop = db.PutSearchRow, db.DropSearchRow
	}
	q := dbq.New(tx)
	var out outcome
	from, err := q.GetSearchState(ctx, int64(id))
	known := err == nil
	switch {
	case errors.Is(err, sql.ErrNoRows):
		from = threadlog.FileHeaderSize
	case err != nil:
		return out, err
	}
	out.from, out.indexed, out.end = from, from, from
	if known && upTo == from {
		return out, nil // already there; an event can arrive after a sweep
	}

	src, err := x.source(id, t.rebuild)
	if err != nil {
		return out, err
	}
	defer src.close()
	out.end = src.length
	if upTo <= 0 {
		upTo = src.length
	}
	if src.length < upTo {
		return out, src.fault(fmt.Errorf("thread %s: the log is %d bytes, shorter than the %d an event reported", id, src.length, upTo))
	}
	fresh := !known
	if known && from > src.length {
		// A database restored beside an older log, or a log swapped for a
		// shorter one. The index is disposable, so the thread is indexed
		// again rather than quarantined.
		x.log.Warn("a thread's search_state is past its log's end; it is indexed again from its first frame",
			"thread", id, "search_state", from, "log_length", src.length)
		fresh = true
	}
	if !fresh && upTo <= from {
		return out, nil // an event older than a sweep that already passed it
	}

	var s scan
	if !fresh {
		s, err = scanFrames(src.f, from, upTo)
		if errors.Is(err, errNotABoundary) {
			x.log.Warn("a thread's search_state is not a frame boundary; it is indexed again from its first frame",
				"thread", id, "search_state", from, "err", err)
			fresh = true
		} else if err != nil {
			return out, src.fault(fmt.Errorf("thread %s: %w", id, err))
		}
	}
	if fresh {
		// Nothing of what the thread had may survive: an ordinal the log no
		// longer holds would keep its row (Terva review 1731). That holds
		// for a log cut back to its header too, which has nothing left to
		// index (Terva review 1750).
		if err := db.DropSearchThread(ctx, tx, int64(id)); err != nil {
			return out, err
		}
		from = threadlog.FileHeaderSize
		if upTo <= threadlog.FileHeaderSize {
			// A log that holds only its header has no rows. Its
			// search_state is recorded at the header, so the next catch-up
			// passes it by without a transaction (Terva review 1755).
			if err := q.SetSearchState(ctx, dbq.SetSearchStateParams{
				ThreadID: int64(id), LogLength: threadlog.FileHeaderSize, UpdatedAt: x.now().UnixMilli(),
			}); err != nil {
				return out, err
			}
			out.from, out.indexed, out.end = from, threadlog.FileHeaderSize, src.length
			return out, nil
		}
		if s, err = scanFrames(src.f, from, upTo); err != nil {
			return out, src.fault(fmt.Errorf("thread %s: %w", id, err))
		}
	}

	ordinals := make([]uint32, 0, len(s.touched))
	for o := range s.touched {
		ordinals = append(ordinals, o)
	}
	slices.Sort(ordinals)
	entries, snapLen, err := src.entries(ordinals)
	if err != nil {
		return out, src.fault(fmt.Errorf("thread %s: %w", id, err))
	}
	if snapLen < upTo {
		return out, fmt.Errorf("thread %s: the index snapshot is at %d, before the %d scanned", id, snapLen, upTo)
	}
	for i, o := range ordinals {
		e := entries[i]
		out.posts++
		if !e.Flags.Visible() {
			if err := drop(ctx, tx, int64(id), int64(o)); err != nil {
				return out, err
			}
			continue
		}
		source, err := src.text(o, e)
		if err != nil {
			return out, src.fault(fmt.Errorf("thread %s: reading post %d: %w", id, o, err))
		}
		plain := render.Plain(source)
		out.bytes += int64(len(plain))
		if err := put(ctx, tx, db.SearchDoc{
			Thread: int64(id), Ordinal: int64(o), Revision: int64(e.Revision),
			AuthorID: int64(e.Author), CreatedAt: e.CreatedMS, SourcePlain: plain,
		}); err != nil {
			return out, err
		}
		out.docs++
	}

	if s.titleSeen {
		out.posts++
		title := strings.TrimSpace(render.DropSentinels(s.title))
		if title == "" {
			err = drop(ctx, tx, int64(id), 0)
		} else {
			if !s.creatorSeen {
				if s.creator, err = firstFrame(src.f, upTo); err != nil {
					return out, src.fault(fmt.Errorf("thread %s: %w", id, err))
				}
			}
			out.bytes += int64(len(title))
			err = put(ctx, tx, db.SearchDoc{
				Thread: int64(id), Ordinal: 0, AuthorID: int64(s.creator.actor), CreatedAt: s.creator.ms, Title: title,
			})
		}
		if err != nil {
			return out, err
		}
	}

	if err := q.SetSearchState(ctx, dbq.SetSearchStateParams{
		ThreadID: int64(id), LogLength: upTo, UpdatedAt: x.now().UnixMilli(),
	}); err != nil {
		return out, err
	}
	out.from, out.indexed = from, upTo
	return out, nil
}
