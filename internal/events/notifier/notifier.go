// Package notifier is the subscriber that writes the post kinds of
// notification: reply_to_you, mention, quoted and thread_you_watch (docs/04
// "notifications", docs/02 "Background work", TKT-01M29CRJ2T). The two
// application kinds are written elsewhere, by internal/service/notify, in the
// transaction of the act that causes them.
//
// # What a record generates
//
// A Post written here, not copied by a split or merge, gives the author of
// the post it replies to reply_to_you, each member its mentions key names
// mention, the author of each earlier post it quotes quoted, and each member
// whose effective level on the thread is watch thread_you_watch. An Edit
// whose mentions key is present gives the members it names mention. No other
// record generates anything.
//
// One post gives one member at most one notification, of the strongest kind
// (rank). A member has at most one unread thread_you_watch per thread, which
// names the first post they were alerted to; the database's unique indexes
// keep both rules, so a replay converges on the same rows. The record's
// actor, the post's author, a member who is not active, one whose effective
// level is mute, and one perm does not let read the thread's forum get
// nothing. So do a record written at or before the generator was installed,
// and one more than seven days older than the moment it is processed.
//
// Serving holds a stricter rule than generation: a row is listed and counted
// only while its post is visible, its forum readable and its thread not
// muted, checked when it is served (threads.Service.VisibleNotifications).
// Nothing here carries post text into a row.
//
// # Progress and recovery
//
// notification_progress records how far into each log the generator has
// got. A thread is processed by first asking the projection to reach the
// length an event reported, then scanning the frames from the progress to
// the length the projection holds, in one write transaction with the rows
// and the new progress, so a crash leaves both or neither. The generator
// reads post_meta, threads and watches for the range, and stopping at what
// the projection reached means it never reads them for a range they do not
// yet describe.
//
// The bus is a hint. A thread that fails, or that the projection has not
// reached, joins a behind set that Run retries and Lag reports. A batch
// reporting dropped events, and the first Run after a start, sweep: the
// projection's own sweep first, then every log on disk against
// notification_progress, so a thread both subscribers missed is still found
// (review 1743). Logs are opened read-only.
package notifier

import (
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

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/events"
	"github.com/terva-sh/ketju/internal/perm"
	"github.com/terva-sh/ketju/internal/render"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// Horizon is how much older than the moment it is processed a record may
// be and still generate. It covers a database restored without the
// generator's progress, whose catch-up would otherwise alert everyone to
// everything since the backup (docs/04 "notifications").
const Horizon = 7 * 24 * time.Hour

// sessionKey is the SessionKey of every recipient's viewer. perm keys its
// cache by session, user, role and status together, so the generator's
// entries never answer a session's question or the other way round.
const sessionKey = "notifier"

// Options configures a Generator.
type Options struct {
	// DB is the database notifications and their progress live in.
	DB *db.DB

	// Root is the data directory, as threadid.ID.LogPath takes it.
	Root string

	// Perm decides whether a recipient may read the thread's forum.
	Perm *perm.Evaluator

	// Project brings the projection of a thread up to a length an append
	// reported: projection.Writer.ProjectThread. It must be safe to call
	// beside the projection's own subscriber, which ProjectThread is.
	Project func(ctx context.Context, id threadid.ID, upTo int64) error

	// Sweep is the projection's sweep, run before the generator's own, so a
	// thread whose events both subscribers dropped is projected before the
	// generator compares the logs with its progress (review 1743).
	Sweep func(ctx context.Context) error

	// Quarantined reports a thread the projection found corrupt. It is not
	// processed until it leaves the quarantine. Nil quarantines nothing.
	Quarantined func(threadid.ID) bool

	// Logger receives the lag transitions. Nil means slog.Default.
	Logger *slog.Logger

	// Now is the clock the horizon is measured on and progress is stamped
	// with. Nil means time.Now.
	Now func() time.Time

	// Since replaces the install cutoff, in Unix milliseconds, for a test
	// whose records are dated before its database was migrated. Zero reads
	// it from the generator's migration.
	Since int64
}

// Lag is how far the generator is behind the logs it knows about.
type Lag struct {
	Threads int
	Bytes   int64
}

// Generator is the notification subscriber. It is safe for concurrent use.
type Generator struct {
	db          *db.DB
	root        string
	perm        *perm.Evaluator
	project     func(ctx context.Context, id threadid.ID, upTo int64) error
	sweep       func(ctx context.Context) error
	quarantined func(threadid.ID) bool
	log         *slog.Logger
	now         func() time.Time

	// Seams for tests: how a log is opened, how the logs are listed and
	// sized, and a hook run inside each transaction after its rows are
	// written and before it commits.
	open        func(path string) (logFile, error)
	walk        func(root string) (map[threadid.ID]int64, []string, error)
	size        func(path string) (int64, error)
	afterWrites func(id threadid.ID) error

	sinceMu   sync.Mutex
	since     int64
	sinceRead bool

	// write serializes processing, so Deliver, CatchUp and Retry never
	// process one thread at once; sweeping serializes CatchUp.
	write    sync.Mutex
	sweeping sync.Mutex

	mu     sync.Mutex
	behind map[threadid.ID]*lag

	// sweepOwed is set from New until a CatchUp finishes, and again when
	// one fails, so Run sweeps until one does.
	sweepOwed atomic.Bool
}

type lag struct {
	target  int64 // the longest log length known for the thread
	reached int64 // how far the generator is known to have got
}

// logFile is what the generator needs of an open log.
type logFile interface {
	io.ReaderAt
	io.Closer
	Stat() (os.FileInfo, error)
}

// New returns a Generator, or an error naming the option that is missing.
func New(opts Options) (*Generator, error) {
	switch {
	case opts.DB == nil:
		return nil, errors.New("notifier: Options.DB is nil")
	case opts.Root == "":
		return nil, errors.New("notifier: Options.Root is empty")
	case opts.Perm == nil:
		return nil, errors.New("notifier: Options.Perm is nil; no recipient's access could be decided")
	case opts.Project == nil:
		return nil, errors.New("notifier: Options.Project is nil")
	case opts.Sweep == nil:
		return nil, errors.New("notifier: Options.Sweep is nil; a thread both subscribers missed would not be found")
	}
	g := &Generator{
		db: opts.DB, root: opts.Root, perm: opts.Perm, project: opts.Project, sweep: opts.Sweep,
		quarantined: opts.Quarantined, log: opts.Logger, now: opts.Now,
		open: func(path string) (logFile, error) { return os.OpenFile(path, os.O_RDONLY, 0) },
		walk: threadid.WalkLogs,
		size: func(path string) (int64, error) {
			fi, err := os.Stat(path)
			if err != nil {
				return 0, err
			}
			return fi.Size(), nil
		},
		since: opts.Since, sinceRead: opts.Since != 0,
		behind: make(map[threadid.ID]*lag),
	}
	if g.quarantined == nil {
		g.quarantined = func(threadid.ID) bool { return false }
	}
	if g.log == nil {
		g.log = slog.Default()
	}
	if g.now == nil {
		g.now = time.Now
	}
	g.sweepOwed.Store(true)
	return g, nil
}

// Name identifies the generator on the bus.
func (g *Generator) Name() string { return "notifications" }

// Deliver processes each thread in the batch up to the longest log length
// its events report, and retries every thread already behind. A failure is
// held for retry rather than returned, as the projection writer holds one:
// the bus would count the event as dropped and every later batch would
// carry a hole this generator does not have.
//
// A batch reporting dropped events runs CatchUp first, and a failed CatchUp
// is the one error Deliver returns, so the bus reports the hole again.
func (g *Generator) Deliver(ctx context.Context, b events.Batch) error {
	var sweepErr error
	if b.Dropped > 0 {
		if err := g.CatchUp(ctx); err != nil {
			sweepErr = fmt.Errorf("notifier: catching up after %d dropped events: %w", b.Dropped, err)
		}
	}
	var order []threadid.ID
	targets := map[threadid.ID]int64{}
	for _, ev := range b.Events {
		if _, seen := targets[ev.Thread]; !seen {
			order = append(order, ev.Thread)
		}
		targets[ev.Thread] = max(targets[ev.Thread], ev.LogLength)
	}
	g.mu.Lock()
	for id, l := range g.behind {
		if _, seen := targets[id]; !seen {
			order = append(order, id)
		}
		targets[id] = max(targets[id], l.target)
	}
	g.mu.Unlock()
	for _, id := range order {
		if ctx.Err() != nil {
			break
		}
		g.thread(ctx, id, targets[id])
	}
	return sweepErr
}

// CatchUp runs the projection's sweep, then processes every thread whose
// log on disk is longer than its notification_progress, a thread with no
// progress row included. A quarantined thread is skipped.
//
// The error is for a catch-up that could not look: the sweep, the progress
// query or the walk. A thread that failed is in the behind set, which Lag
// reports and Run retries. A catch-up that returns an error leaves the
// sweep owed, and Run runs it again until one finishes.
func (g *Generator) CatchUp(ctx context.Context) (err error) {
	g.sweeping.Lock()
	defer g.sweeping.Unlock()
	defer func() {
		if err != nil {
			g.sweepOwed.Store(true)
		}
	}()
	start := time.Now()
	if err := g.sweep(ctx); err != nil {
		return fmt.Errorf("notifier: catch-up: the projection's sweep: %w", err)
	}
	progress := map[threadid.ID]int64{}
	err = g.db.Read(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		rows, err := q.ListNotificationProgress(ctx)
		for _, r := range rows {
			progress[threadid.ID(r.ThreadID)] = r.LogLength
		}
		return err
	})
	if err != nil {
		return fmt.Errorf("notifier: catch-up: reading notification_progress: %w", err)
	}
	logs, _, err := g.walk(g.root)
	if err != nil {
		return fmt.Errorf("notifier: catch-up: %w", err)
	}
	ids := make([]threadid.ID, 0, len(logs))
	for id := range logs {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	processed, failed := 0, 0
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return err
		}
		size := logs[id]
		at, known := progress[id]
		if !known {
			at = threadlog.FileHeaderSize
		}
		switch {
		case g.quarantined(id):
			g.forget(id)
			continue
		case size <= at:
			g.forgetUpTo(id, size)
			continue
		}
		processed++
		if err := g.thread(ctx, id, size); err != nil {
			failed++
		}
	}
	g.sweepOwed.Store(false)
	g.log.Info("notification catch-up finished", "scanned", len(ids), "processed", processed-failed,
		"behind", failed, "elapsed", time.Since(start))
	return nil
}

// Retry processes every thread that is behind, up to the length its log has
// now. It returns an error joining the failures, for Run's backoff; the
// threads stay behind either way.
func (g *Generator) Retry(ctx context.Context) error {
	g.mu.Lock()
	ids := make([]threadid.ID, 0, len(g.behind))
	held := map[threadid.ID]int64{}
	for id, l := range g.behind {
		ids = append(ids, id)
		held[id] = l.target
	}
	g.mu.Unlock()
	slices.Sort(ids)
	var errs []error
	for _, id := range ids {
		if g.quarantined(id) {
			g.forget(id)
			continue
		}
		// The log's length now, rather than the target it was held at: a
		// tail torn when it was held is cut by the next open, and a log
		// only grows otherwise.
		target := held[id]
		if n, err := g.size(id.LogPath(g.root)); err == nil {
			target = n
			g.mu.Lock()
			if b := g.behind[id]; b != nil && b.target == held[id] {
				b.target = n
			}
			g.mu.Unlock()
		}
		errs = append(errs, g.thread(ctx, id, target))
	}
	return errors.Join(errs...)
}

// retryFloor and retryCeiling bound Run's backoff.
var retryFloor, retryCeiling = time.Second, time.Minute

// Run keeps the notifications caught up until ctx ends. While a sweep is
// owed, from New until a CatchUp finishes and again after one fails, it
// runs CatchUp; otherwise it retries the threads that are behind. The first
// sweep starts at once, and while either keeps failing it backs off from one
// second to a minute.
func (g *Generator) Run(ctx context.Context) {
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
		case g.sweepOwed.Load():
			if err = g.CatchUp(ctx); err != nil && ctx.Err() == nil {
				g.log.Error("the notification catch-up failed; it will be run again", "err", err)
			}
		case g.Lag().Threads > 0:
			err = g.Retry(ctx)
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
func (g *Generator) Lag() Lag {
	g.mu.Lock()
	defer g.mu.Unlock()
	var l Lag
	for _, b := range g.behind {
		l.Threads++
		l.Bytes += max(b.target-b.reached, 0)
	}
	return l
}

// errProjectionBehind is a thread whose projection did not reach the length
// the generator was asked for. The generator processed what it did reach.
var errProjectionBehind = errors.New("the projection has not reached the log length")

// thread brings thread id's notifications up to target: the projection
// first, then the frames from the progress to where the projection is. It
// keeps the behind set in step with the outcome and returns the failure.
func (g *Generator) thread(ctx context.Context, id threadid.ID, target int64) error {
	if g.quarantined(id) {
		g.forget(id)
		return nil
	}
	projErr := g.project(ctx, id, target)
	reached, err := g.process(ctx, id)
	if err == nil && reached < target {
		err = errProjectionBehind
		if projErr != nil {
			err = fmt.Errorf("%w: %w", err, projErr)
		}
	}
	g.settle(id, target, reached, err)
	return err
}

// settle keeps the behind set and its log lines in step with one thread's
// outcome.
func (g *Generator) settle(id threadid.ID, target, reached int64, err error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	was := len(g.behind) > 0
	b := g.behind[id]
	if err == nil {
		// A delivery that failed while this ran may hold a later target,
		// which stays.
		if b == nil || reached >= b.target {
			delete(g.behind, id)
		} else {
			b.reached = reached
		}
	} else {
		if b == nil {
			b = &lag{}
			g.behind[id] = b
		}
		b.target = max(b.target, target)
		b.reached = max(b.reached, reached)
		g.log.Warn("generating a thread's notifications failed; it will be retried", "thread", id, "err", err)
	}
	g.transition(was)
}

// forget takes a thread out of the behind set without processing it.
func (g *Generator) forget(id threadid.ID) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, ok := g.behind[id]; !ok {
		return
	}
	was := len(g.behind) > 0
	delete(g.behind, id)
	g.transition(was)
}

// forgetUpTo takes a thread out of the behind set if nothing holds it past
// size.
func (g *Generator) forgetUpTo(id threadid.ID, size int64) {
	g.mu.Lock()
	defer g.mu.Unlock()
	b, ok := g.behind[id]
	if !ok || b.target > size {
		return
	}
	was := len(g.behind) > 0
	delete(g.behind, id)
	g.transition(was)
}

// transition logs the behind set going from empty to not, or back. Called
// with mu held.
func (g *Generator) transition(was bool) {
	switch is := len(g.behind) > 0; {
	case is && !was:
		g.log.Warn("notifications are behind", "threads", len(g.behind))
	case !is && was:
		g.log.Info("notifications caught up")
	}
}

// cutoff is the install cutoff: a record at or before it generates nothing.
func (g *Generator) cutoff(ctx context.Context) (int64, error) {
	g.sinceMu.Lock()
	defer g.sinceMu.Unlock()
	if g.sinceRead {
		return g.since, nil
	}
	err := g.db.Read(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		var err error
		g.since, err = q.NotificationsSince(ctx)
		return err
	})
	if err != nil {
		return 0, fmt.Errorf("notifier: reading when the generator was installed: %w", err)
	}
	g.sinceRead = true
	return g.since, nil
}

// process generates thread id's notifications from its progress to the
// length its projection has reached, in one write transaction, and returns
// how far the thread is now processed. On an error nothing is kept, and the
// length returned is the progress before.
func (g *Generator) process(ctx context.Context, id threadid.ID) (int64, error) {
	since, err := g.cutoff(ctx)
	if err != nil {
		return 0, err
	}
	g.write.Lock()
	defer g.write.Unlock()
	now := g.now()
	horizon := now.Add(-Horizon).UnixMilli()
	var from, reached int64
	err = g.db.Write(ctx, func(q *dbq.Queries, tx *sql.Tx) error {
		var err error
		from, err = q.GetNotificationProgress(ctx, int64(id))
		switch {
		case errors.Is(err, sql.ErrNoRows):
			from = threadlog.FileHeaderSize
		case err != nil:
			return err
		}
		reached = from
		st, err := q.GetProjectionState(ctx, int64(id))
		switch {
		case errors.Is(err, sql.ErrNoRows):
			return nil // nothing projected, so nothing to read the range against
		case err != nil:
			return err
		}
		upTo := st.LogLength
		if upTo <= from {
			return nil
		}
		items, err := g.scan(id, from, upTo, since, horizon)
		if err != nil {
			return err
		}
		if err := g.generate(ctx, q, id, items); err != nil {
			return err
		}
		if g.afterWrites != nil {
			if err := g.afterWrites(id); err != nil {
				return err
			}
		}
		if err := q.SetNotificationProgress(ctx, dbq.SetNotificationProgressParams{
			ThreadID: int64(id), LogLength: upTo, UpdatedAt: now.UnixMilli(),
		}); err != nil {
			return err
		}
		reached = upTo
		return nil
	})
	if err != nil {
		return from, fmt.Errorf("notifier: thread %s: %w", id, err)
	}
	return reached, nil
}

// item is one record that may generate: a Post, or an Edit naming mentions.
type item struct {
	ordinal  uint32
	actor    int64
	at       int64
	edit     bool
	replyTo  uint32
	mentions []uint64
	quotes   []uint32
}

// scan reads the frames in [from, upTo) of thread id's log and returns the
// records that may generate, past the install cutoff and inside the
// horizon. Damage is an error: generating past a damaged record would treat
// what follows as if it had not happened.
func (g *Generator) scan(id threadid.ID, from, upTo, since, horizon int64) ([]item, error) {
	f, err := g.open(id.LogPath(g.root))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if err := checkHeader(f, id); err != nil {
		return nil, err
	}
	var items []item
	var scanErr error
	err = threadlog.ScanFramesFrom(f, from, upTo, func(rec threadlog.ScanRecord) bool {
		if rec.Err != nil {
			scanErr = fmt.Errorf("frame at offset %d: %w", rec.Offset, rec.Err)
			return false
		}
		h := rec.Header
		if h.Type != threadlog.TypePost && h.Type != threadlog.TypeEdit {
			return true
		}
		if h.TimestampMS <= since || h.TimestampMS < horizon {
			return true
		}
		body, err := threadlog.DecodeBody(h.Type, h.Encoding, rec.Body)
		if err != nil {
			scanErr = fmt.Errorf("frame at offset %d: %w", rec.Offset, err)
			return false
		}
		it := item{ordinal: h.Ordinal, actor: int64(h.Actor), at: h.TimestampMS}
		switch b := body.(type) {
		case *threadlog.PostBody:
			if b.Origin != nil {
				return true // a split or merge copy: Phase 4's (TKT-01M29CXJZZ)
			}
			it.replyTo, it.mentions, it.quotes = b.ReplyTo, b.Mentions, quoted(b.Source, h.Ordinal)
		case *threadlog.EditBody:
			if b.Mentions == nil {
				return true
			}
			it.edit, it.mentions = true, *b.Mentions
		default:
			return true
		}
		items = append(items, it)
		return true
	})
	if err := errors.Join(err, scanErr); err != nil {
		return nil, err
	}
	return items, nil
}

// quoted is the ordinals before ordinal that source quotes. Quotes are not
// in the log, so the source is rendered to find them, as the composer and
// the create path find them; with no resolver every quote is reported, and
// the caller keeps those post_meta says are there and not removed.
func quoted(source string, ordinal uint32) []uint32 {
	if !strings.Contains(source, "[quote=#") {
		return nil
	}
	r, err := render.RenderWith(source, render.Context{})
	if err != nil {
		return nil // a source the renderer refuses was never posted
	}
	var out []uint32
	for _, q := range r.Quotes {
		if q.Ordinal < ordinal && !slices.Contains(out, q.Ordinal) {
			out = append(out, q.Ordinal)
		}
	}
	return out
}

// rank orders the post kinds: one post gives one member the strongest it
// would give them (docs/04, run decision 2026-10-01). UpsertPostNotification
// states the same order in SQL, and TestStrongestKindWins holds the two
// together.
var rank = map[string]int{
	domain.NotifyMention:        4,
	domain.NotifyReplyToYou:     3,
	domain.NotifyQuoted:         2,
	domain.NotifyThreadYouWatch: 1,
}

// generate writes the rows items give, in log order, through q.
func (g *Generator) generate(ctx context.Context, q *dbq.Queries, id threadid.ID, items []item) error {
	if len(items) == 0 {
		return nil
	}
	th, err := q.GetThread(ctx, int64(id))
	if err != nil {
		return fmt.Errorf("reading the thread's projection row: %w", err)
	}

	// The posts the items answer, quote or edit.
	var need []int64
	for _, it := range items {
		if it.replyTo > 0 {
			need = append(need, int64(it.replyTo))
		}
		for _, o := range it.quotes {
			need = append(need, int64(o))
		}
		if it.edit {
			need = append(need, int64(it.ordinal))
		}
	}
	slices.Sort(need)
	need = slices.Compact(need)
	posts := map[uint32]dbq.PostAuthorsRow{}
	if err := inChunks(need, func(part []int64) error {
		rows, err := q.PostAuthors(ctx, dbq.PostAuthorsParams{ThreadID: int64(id), Ordinals: part})
		for _, r := range rows {
			posts[uint32(r.Ordinal)] = r
		}
		return err
	}); err != nil {
		return err
	}

	// Every member's effective level on the thread.
	rows, err := q.ThreadWatchers(ctx, dbq.ThreadWatchersParams{
		ThreadID: sql.NullInt64{Int64: int64(id), Valid: true}, ForumID: sql.NullInt64{Int64: th.ForumID, Valid: true}})
	if err != nil {
		return err
	}
	onThread, onForum := map[int64]string{}, map[int64]string{}
	for _, r := range rows {
		if r.OnThread != 0 {
			onThread[r.UserID] = r.Level
		} else {
			onForum[r.UserID] = r.Level
		}
	}
	level := func(u int64) string { return domain.EffectiveWatch(onThread[u], onForum[u]) }
	var watchers []int64
	for u := range onThread {
		if level(u) == domain.WatchWatch {
			watchers = append(watchers, u)
		}
	}
	for u := range onForum {
		if _, own := onThread[u]; !own && level(u) == domain.WatchWatch {
			watchers = append(watchers, u)
		}
	}

	// Each item's candidates, the strongest kind each.
	type candidates struct {
		item
		author int64
		kinds  map[int64]string
	}
	all := make([]candidates, 0, len(items))
	ids := map[int64]bool{}
	for _, it := range items {
		c := candidates{item: it, kinds: map[int64]string{}}
		offer := func(u int64, kind string) {
			if u <= 0 {
				return
			}
			if rank[kind] > rank[c.kinds[u]] {
				c.kinds[u] = kind
			}
		}
		if it.edit {
			p, ok := posts[it.ordinal]
			if !ok {
				continue // an edit of a post the projection does not have
			}
			c.author = p.AuthorID
		} else {
			c.author = it.actor
			for _, u := range watchers {
				offer(u, domain.NotifyThreadYouWatch)
			}
			for _, o := range it.quotes {
				if p, ok := posts[o]; ok && p.Deleted == 0 && p.Moved == 0 {
					offer(p.AuthorID, domain.NotifyQuoted)
				}
			}
			if p, ok := posts[it.replyTo]; ok && it.replyTo > 0 {
				offer(p.AuthorID, domain.NotifyReplyToYou)
			}
		}
		for _, m := range it.mentions {
			offer(int64(m), domain.NotifyMention)
		}
		delete(c.kinds, it.actor)
		delete(c.kinds, c.author)
		for u := range c.kinds {
			ids[u] = true
		}
		ids[it.actor] = true
		all = append(all, c)
	}

	// Who each candidate is, read once, and whether they may have it.
	lookup := make([]int64, 0, len(ids))
	for u := range ids {
		lookup = append(lookup, u)
	}
	slices.Sort(lookup)
	users := map[int64]dbq.User{}
	if err := inChunks(lookup, func(part []int64) error {
		us, err := q.UsersByIDs(ctx, part)
		for _, u := range us {
			users[u.ID] = u
		}
		return err
	}); err != nil {
		return err
	}
	readable := map[int64]bool{}
	may := func(u int64) (bool, error) {
		if ok, seen := readable[u]; seen {
			return ok, nil
		}
		row, ok := users[u]
		if !ok || row.Status != domain.StatusActive || level(u) == domain.WatchMute {
			readable[u] = false
			return false, nil
		}
		can, err := g.perm.Can(ctx, RecipientViewer(row), th.ForumID, domain.PermRead)
		if err != nil {
			return false, err
		}
		readable[u] = can
		return can, nil
	}

	for _, c := range all {
		recipients := make([]int64, 0, len(c.kinds))
		for u := range c.kinds {
			recipients = append(recipients, u)
		}
		slices.Sort(recipients)
		var actor sql.NullInt64
		if _, ok := users[c.actor]; ok {
			actor = sql.NullInt64{Int64: c.actor, Valid: true}
		}
		thread := sql.NullInt64{Int64: int64(id), Valid: true}
		ordinal := sql.NullInt64{Int64: int64(c.ordinal), Valid: true}
		for _, u := range recipients {
			ok, err := may(u)
			if err != nil {
				return err
			}
			if !ok {
				continue
			}
			if kind := c.kinds[u]; kind == domain.NotifyThreadYouWatch {
				err = q.InsertWatchNotification(ctx, dbq.InsertWatchNotificationParams{
					UserID: u, ThreadID: thread, Ordinal: ordinal, ActorID: actor, At: c.at})
			} else {
				err = q.UpsertPostNotification(ctx, dbq.UpsertPostNotificationParams{
					UserID: u, Kind: kind, ThreadID: thread, Ordinal: ordinal, ActorID: actor, At: c.at})
			}
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// RecipientViewer is the viewer the generator decides a recipient's access
// as: the viewer their own session would carry (session.Session.Viewer), so
// perm decides for them what it decides for that session, a moderator by
// group grant included. An account that is not active carries no role,
// as a session's viewer does not.
func RecipientViewer(u dbq.User) domain.Viewer {
	v := domain.Viewer{UserID: u.ID, Active: u.Status == domain.StatusActive, SessionKey: sessionKey}
	if v.Active {
		v.Role = u.Role
	}
	return v
}

// checkHeader reads the log's file header and refuses a damaged one, a
// version this build cannot read, or a log that names another thread, whose
// posts would otherwise generate under this thread's id.
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

// lookupChunk bounds how many ids one IN list binds. sqlc expands a slice
// into one parameter per element, and SQLite refuses a statement with more
// variables than its limit, so a catch-up backlog that names more posts or
// members than that would fail the same way on every retry and never
// advance (Terva review 1784). The bound sits far below any limit SQLite
// has shipped with.
var lookupChunk = 500

// inChunks calls fn with ids in consecutive parts of at most lookupChunk,
// stopping at the first error. With no ids it calls nothing.
func inChunks(ids []int64, fn func([]int64) error) error {
	for part := range slices.Chunk(ids, lookupChunk) {
		if err := fn(part); err != nil {
			return err
		}
	}
	return nil
}
