package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/terva-sh/ketju/internal/auth/agentcred"
	"github.com/terva-sh/ketju/internal/auth/oidc"
	"github.com/terva-sh/ketju/internal/auth/seal"
	"github.com/terva-sh/ketju/internal/auth/session"
	"github.com/terva-sh/ketju/internal/backup"
	"github.com/terva-sh/ketju/internal/buildinfo"
	"github.com/terva-sh/ketju/internal/config"
	"github.com/terva-sh/ketju/internal/events"
	"github.com/terva-sh/ketju/internal/events/notifier"
	"github.com/terva-sh/ketju/internal/events/projection"
	"github.com/terva-sh/ketju/internal/events/search"
	"github.com/terva-sh/ketju/internal/jobs"
	"github.com/terva-sh/ketju/internal/lock"
	"github.com/terva-sh/ketju/internal/logging"
	"github.com/terva-sh/ketju/internal/metrics/collect"
	"github.com/terva-sh/ketju/internal/perm"
	"github.com/terva-sh/ketju/internal/render"
	"github.com/terva-sh/ketju/internal/service/account"
	"github.com/terva-sh/ketju/internal/service/applications"
	"github.com/terva-sh/ketju/internal/service/blobs"
	formsvc "github.com/terva-sh/ketju/internal/service/forms"
	"github.com/terva-sh/ketju/internal/service/forums"
	"github.com/terva-sh/ketju/internal/service/identity"
	"github.com/terva-sh/ketju/internal/service/posts"
	"github.com/terva-sh/ketju/internal/service/respcache"
	"github.com/terva-sh/ketju/internal/service/threads"
	"github.com/terva-sh/ketju/internal/service/users"
	"github.com/terva-sh/ketju/internal/settings"
	"github.com/terva-sh/ketju/internal/store/blob"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadcache"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/web"
	"github.com/terva-sh/ketju/internal/web/api"
	"github.com/terva-sh/ketju/internal/web/auth"
	blobsweb "github.com/terva-sh/ketju/internal/web/blobs"
	"github.com/terva-sh/ketju/internal/web/health"
	"github.com/terva-sh/ketju/internal/web/spa"
	"github.com/terva-sh/ketju/internal/web/sse"
)

// defaultDrain is how long shutdown waits for in-flight requests (docs/02
// "Background work"). Past it the remaining connections are cut, and
// shutdown goes on to flush the thread actors regardless.
const defaultDrain = 10 * time.Second

// busDrain bounds how long the event bus has to deliver what is queued at
// shutdown. The projection it feeds is caught up again at the next start,
// so waiting longer buys nothing a restart does not.
const busDrain = 5 * time.Second

const serveUsage = `usage: ketju serve [--config PATH]

Runs the server until SIGTERM or SIGINT, then stops accepting, drains
in-flight requests for up to 10 seconds, flushes every open thread, and
closes the database.

The config file is --config, else $KETJU_CONFIG, else ./config.toml. A
relative data_dir is relative to the config file.

exit status:
  0  shut down cleanly
  4  startup failed, naming the step, or a shutdown step failed
`

// serveOpts are the seams a test needs. The zero value is what the binary
// runs with.
type serveOpts struct {
	// getenv reads the environment; nil means os.Getenv.
	getenv func(string) string

	// logOut receives the log; nil means stderr.
	logOut io.Writer

	// drain overrides defaultDrain.
	drain time.Duration

	// step is told each startup and shutdown step as it begins, which is how
	// the order is tested.
	step func(name string)

	// ready is called with the bound address once the listener is open,
	// which is after catch-up has finished.
	ready func(addr string)

	// metricsReady is called with the metrics listener's bound address,
	// before ready, when metrics_listen is set.
	metricsReady func(addr string)

	// metrics is handed the metrics set once its sources are registered,
	// so a test can scrape it directly.
	metrics func(*collect.Set)

	// wrap wraps the root handler, so a test can mount a handler of its own
	// under the real middleware.
	wrap func(http.Handler) http.Handler

	// threads is handed the thread cache once it is open, so a test can
	// append through the same actors serve will flush.
	threads func(*threadcache.Cache)

	// runReturned is called as the projection runner returns, which is how
	// a test holds it back to see that shutdown waits for it.
	runReturned func()

	// searchHold, when set, runs in the search indexer's goroutine before
	// its catch-up, under the context that stops it, so a test can hold
	// the pass back and see that the listener does not wait for it.
	searchHold func(ctx context.Context)

	// listen wraps the listener, so a test can make accepting fail.
	listen func(net.Listener) net.Listener

	// idpClient reaches the identity providers; nil is http.DefaultClient. A
	// test passes one that trusts its in-process IdP.
	idpClient *http.Client

	// snapshotHold, when set, runs while a backup snapshot holds appends,
	// and pauseCap overrides backup.PauseCap. A test holds the pause open
	// with the one and shortens it with the other.
	snapshotHold func(ctx context.Context)
	pauseCap     time.Duration

	// job, when set, runs beside the background jobs, and jobsWait
	// overrides how long shutdown waits for them (busDrain). A test uses
	// them to hold a job past the wait.
	job      *jobs.Job
	jobsWait time.Duration
}

func runServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() { fmt.Fprint(os.Stderr, serveUsage) }
	path := fs.String("config", "", "config file")
	if err := fs.Parse(args); err != nil {
		return errUsage{err}
	}
	if fs.NArg() != 0 {
		return errUsage{fmt.Errorf("serve takes no arguments, got %q", fs.Args())}
	}
	ctx, stop := signalContext()
	defer stop()
	return serve(ctx, configPath(*path, os.Getenv), serveOpts{})
}

// signalContext is cancelled by SIGTERM or SIGINT, which is how serve is
// asked to shut down.
func signalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
}

// configEnv names the variable that chooses the config file.
const configEnv = "KETJU_CONFIG"

// configPath is --config, else KETJU_CONFIG, else ./config.toml (settled in
// grooming for TKT-01M29BYYJG).
func configPath(flagValue string, getenv func(string) string) string {
	switch {
	case flagValue != "":
		return flagValue
	case getenv(configEnv) != "":
		return getenv(configEnv)
	default:
		return "config.toml"
	}
}

// serve runs the server until ctx is cancelled.
//
// Startup brings the pieces up in dependency order: config, the lock, the
// database, the thread store, the event subscribers and projection
// catch-up, and only then the listener, so no request is served from a
// projection that is behind. A failing step returns an error naming it.
//
// Shutdown runs the other way, and every step runs even if one before it
// failed: stop accepting and drain, stop the bus, close every thread actor
// (which flushes its index header), close the database, release the lock.
// The actors close before the database because docs/02 orders it so; the
// point of closing them at all is that a restart can trust every index
// instead of replaying every log.
func serve(ctx context.Context, path string, o serveOpts) (err error) {
	step := func(name string) {
		if o.step != nil {
			o.step(name)
		}
	}
	fail := func(what string, err error) error { return fmt.Errorf("serve: %s: %w", what, err) }
	drain := o.drain
	if drain <= 0 {
		drain = defaultDrain
	}

	step("config")
	cfg, warnings, err := config.Load(path, o.getenv)
	if err != nil {
		return fail("loading the config", err)
	}
	out := o.logOut
	if out == nil {
		out = os.Stderr
	}
	log, err := logging.New(cfg.Log, logging.Options{Writer: out})
	if err != nil {
		return fail("starting the logger", err)
	}
	for _, w := range warnings {
		log.Warn(w)
	}

	// Shutdown steps are pushed as startup succeeds and run in reverse, so a
	// failure partway up closes exactly what was opened.
	var closers []func() error
	defer func() {
		for i := len(closers) - 1; i >= 0; i-- {
			if cerr := closers[i](); cerr != nil {
				log.Error("shutdown step failed", "err", cerr)
				err = errors.Join(err, cerr)
			}
		}
		if err == nil {
			log.Info("ketju stopped")
		}
	}()

	step("lock")
	if err := os.MkdirAll(cfg.DataDir, 0o750); err != nil {
		return fail("creating the data directory "+cfg.DataDir, err)
	}
	lk, err := lock.Acquire(cfg.DataDir, "serve")
	if err != nil {
		return fail("locking the data directory", err)
	}
	closers = append(closers, func() error {
		step("release lock")
		return lk.Release()
	})

	// A hard redaction that did not finish may have rewritten some logs and
	// not others, and serving that would show a copy of text another log has
	// lost (docs/03 "Hard redaction"). Only redact finishes it.
	step("redaction")
	if _, err := os.Stat(journalPath(cfg.DataDir)); err == nil {
		return fail("a hard redaction is pending", fmt.Errorf("%s exists; finish it with ketju redact --root %s --resume, "+
			"then start ketju serve", journalPath(cfg.DataDir), cfg.DataDir))
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fail("looking for a pending hard redaction", err)
	}

	step("database")
	for _, sub := range []string{threadid.ThreadsDir, "blobs"} {
		if err := os.MkdirAll(filepath.Join(cfg.DataDir, sub), 0o750); err != nil {
			return fail("creating "+sub, err)
		}
	}
	// Thread indexes an older build may have left trusted over a stale entry
	// go, once per data directory, before anything opens a thread; each is
	// rebuilt from its log on first open (TKT-01M3VHSYFR, docs/03
	// "Durability and recovery"). Starting without it could serve a deleted
	// or hidden post as visible, so a failure stops the start.
	if n, err := threadcache.DiscardUnverifiedIndexes(cfg.DataDir); err != nil {
		return fail("discarding thread indexes written before index write-back", err)
	} else if n > 0 {
		log.Info("discarded thread indexes an older build wrote; each is rebuilt from its log when the thread is first opened",
			"removed", n)
	}
	// Uploads staged by a process that stopped mid-upload. Under the lock
	// and before the listener, nothing is being staged now, so all of it
	// goes (docs/03, TKT-01M29CXK0K).
	if n, err := blob.SweepTmp(cfg.DataDir, 0, time.Now()); err != nil {
		log.Warn("could not empty the upload staging directory; the collector retries it", "removed", n, "err", err)
	} else if n > 0 {
		log.Info("removed uploads a stopped process left staged", "removed", n)
	}
	// A control socket and backup staging a stopped process left. Under the
	// lock, nothing is using either (TKT-01M29CXK35).
	if err := backup.RemoveStale(cfg.DataDir); err != nil {
		log.Warn("could not remove a stale control socket or backup staging directory", "err", err)
	}
	dbPath := filepath.Join(cfg.DataDir, "db", "ketju.db")
	d, err := db.Open(ctx, db.Options{Path: dbPath})
	if err != nil {
		return fail("opening the database "+dbPath, err)
	}
	// jobsDone is the background jobs' Done, once they start. A job that
	// outlasts its bounded stop still holds the database, so it is left
	// open rather than closed under the job: nothing is lost, because
	// SQLite replays the WAL at the next open (review 1158).
	var jobsDone <-chan struct{}
	closers = append(closers, func() error {
		step("close database")
		if jobsDone != nil {
			select {
			case <-jobsDone:
			default:
				return errors.New("serve: a background job is still running, so the database is left open " +
					"rather than closed under it; SQLite replays its WAL at the next start")
			}
		}
		return d.Close()
	})
	if err := d.Migrate(ctx); err != nil {
		return fail("migrating the database "+dbPath, err)
	}
	// The registration form is loaded once here so a stored schema that no
	// longer parses is said at startup, when the operator is looking, and
	// not first to an applicant. It is not fatal: members can still use the
	// forum, and only the apply and form routes answer 500 (TKT-01M29CP2KE).
	formSvc := &formsvc.Service{DB: d}
	if _, err := formSvc.Active(ctx, formsvc.Registration); err != nil {
		log.Error("the registration form cannot be loaded; applying to join and the form editor will fail",
			"form", formsvc.Registration, "err", err)
	}

	step("stores")
	// The metrics are collected whether or not metrics_listen serves them:
	// an atomic add per request and per fsync is cheaper than a second
	// code path (TKT-01M29CEE7S).
	ms := collect.New()
	cache, err := threadcache.New(cfg.DataDir, threadcache.Options{
		Bound: cfg.Storage.OpenThreads, Sync: cfg.SyncPolicy(), Archived: archived(d), Logger: log,
		SyncObserver: ms.ObserveSync,
	})
	if err != nil {
		return fail("opening the thread store", err)
	}
	closers = append(closers, func() error {
		step("close threads")
		return cache.Close()
	})
	if o.threads != nil {
		o.threads(cache)
	}

	step("subscribers")
	// run is what the background work runs under. It outlives the request
	// drain, so the projection keeps up with the last requests' appends, and
	// is cancelled once the bus has closed.
	run, stopRun := context.WithCancel(context.Background())
	closers = append(closers, func() error { stopRun(); return nil })

	reopen := func(_ context.Context, id threadid.ID) (int64, error) {
		var n int64
		err := cache.With(id, func(th *threadcache.Thread) error {
			n = th.Actor.LogLength()
			return nil
		})
		return n, err
	}
	var proj *projection.Writer
	proj, err = projection.New(projection.Options{
		DB: d, Root: cfg.DataDir, Logger: log,
		Sweep: func(ctx context.Context) error {
			_, err := proj.CatchUp(ctx, reopen)
			return err
		},
	})
	if err != nil {
		return fail("starting the projection", err)
	}
	bus := events.New(events.Options{Logger: log})
	if err := bus.Subscribe(proj, 0); err != nil {
		return fail("subscribing the projection", err)
	}
	// The live-update hub routes an event only once the projection has
	// applied it, which it learns from the projection row, so it needs
	// nothing but the database to subscribe (docs/02 "Live updates").
	hub := sse.NewHub(sse.HubOptions{Projections: threads.Projections{DB: d}, Logger: log,
		Limits: sse.Limits{Connections: cfg.Limits.SSEConnections, PerUser: cfg.Limits.SSEPerUser,
			Anonymous: cfg.Limits.SSEAnonymous}})
	if err := bus.Subscribe(hub, 0); err != nil {
		return fail("subscribing the live-update hub", err)
	}
	// The search index (docs/04 "search (FTS)"). Its queue is deep because a
	// burst of posts is what it batches (TKT-01M29CRJ3B); a drop costs a
	// catch-up, never a missed row.
	indexer, err := search.New(search.Options{
		DB: d, Root: cfg.DataDir, Threads: cache, Quarantined: proj.Quarantined, Logger: log,
	})
	if err != nil {
		return fail("starting the search indexer", err)
	}
	if err := bus.Subscribe(indexer, 4096); err != nil {
		return fail("subscribing the search indexer", err)
	}
	// One evaluator for the process: the notification generator decides
	// each recipient's access through it, sign-in promotes through it
	// (bootstrap), and its cache is the one every request reads.
	perms := perm.New(d)
	// The notification generator (docs/04 "notifications", TKT-01M29CRJ2T).
	// It projects a thread before reading it, and its sweep runs the
	// projection's first, so a thread whose events both dropped is still
	// found (review 1743).
	gen, err := notifier.New(notifier.Options{
		DB: d, Root: cfg.DataDir, Perm: perms, Quarantined: proj.Quarantined, Logger: log,
		Project: func(ctx context.Context, id threadid.ID, upTo int64) error {
			_, err := proj.ProjectThread(ctx, id, upTo)
			return err
		},
		Sweep: func(ctx context.Context) error {
			_, err := proj.CatchUp(ctx, reopen)
			return err
		},
	})
	if err != nil {
		return fail("starting the notification generator", err)
	}
	if err := bus.Subscribe(gen, 0); err != nil {
		return fail("subscribing the notification generator", err)
	}

	step("catch-up")
	report, err := proj.CatchUp(ctx, reopen)
	if err != nil {
		return fail("catching up the projection", err)
	}

	// The search index's version is settled before the listener opens: an
	// index another version built is emptied here, in one transaction, so
	// no request can reach a row of it (Terva review 1719). Indexing the
	// threads that trail is the slow part, and runs after the listener
	// opens.
	step("search")
	if err := indexer.Prepare(ctx); err != nil {
		return fail("preparing the search index", err)
	}
	// The runner's retries use the cache and the database, so shutdown waits
	// for it to return before closing either (review 470). Its closer is
	// pushed before the bus's, so it runs after: the bus delivers what is
	// queued under the run context first, and only then is that cancelled.
	ran := make(chan struct{})
	go func() {
		defer close(ran)
		proj.Run(run)
		if o.runReturned != nil {
			o.runReturned()
		}
	}()
	closers = append(closers, func() error {
		step("stop projection")
		stopRun()
		select {
		case <-ran:
			return nil
		case <-time.After(busDrain):
			return fmt.Errorf("serve: the projection runner did not stop within %s; "+
				"closing the store under it", busDrain)
		}
	})
	// The search catch-up and its retries use the cache and the database,
	// so shutdown waits for them as for the projection runner. Its closer is
	// pushed before the bus's, so it runs after the bus has delivered what is
	// queued.
	searched := make(chan struct{})
	go func() {
		defer close(searched)
		if o.searchHold != nil {
			o.searchHold(run)
		}
		// Run catches up first, and runs the catch-up again until one
		// finishes, then retries the threads that fail (Terva review 1750).
		indexer.Run(run)
	}()
	closers = append(closers, func() error {
		step("stop search")
		stopRun()
		select {
		case <-searched:
			return nil
		case <-time.After(busDrain):
			return fmt.Errorf("serve: the search indexer did not stop within %s; closing the store under it", busDrain)
		}
	})
	// The notification generator sweeps at once, beside the listener, as
	// the search indexer does: until it has, an alert for a post written
	// while the server was down is late, never lost or doubled. Its closer
	// is pushed before the bus's, so it runs after the bus has delivered
	// what is queued.
	step("notifications")
	notified := make(chan struct{})
	go func() {
		defer close(notified)
		gen.Run(run)
	}()
	closers = append(closers, func() error {
		step("stop notifications")
		stopRun()
		select {
		case <-notified:
			return nil
		case <-time.After(busDrain):
			return fmt.Errorf("serve: the notification generator did not stop within %s; closing the store under it",
				busDrain)
		}
	})
	if err := bus.Start(run); err != nil {
		return fail("starting the event bus", err)
	}
	closers = append(closers, func() error {
		step("close bus")
		// Close first, so the projection delivers what is queued; only then
		// cancel the context its deliveries and retries run under.
		bctx, cancel := context.WithTimeout(context.Background(), busDrain)
		defer cancel()
		err := bus.Close(bctx)
		stopRun()
		return err
	})

	// Sign-in: the key that seals a sign-in's state, and the providers,
	// discovered now so a broken IdP is a startup line rather than a
	// reader's failed sign-in hours later (internal/auth/oidc). With none
	// configured, nobody can sign in, and /auth/* says so.
	step("sign-in")
	key, keyWarning, err := seal.LoadOrCreateKey(cfg.CookieKeyPath())
	if err != nil {
		return fail("loading the cookie key", err)
	}
	if keyWarning != "" {
		log.Warn(keyWarning)
	}
	sealer, err := seal.New(key)
	if err != nil {
		return fail("loading the cookie key", err)
	}
	sessions := &session.Store{DB: d}
	// A group_map target no group has grants nothing. It is said here, once,
	// as well as at each sign-in that meets it, because only the database
	// can tell, so config check cannot (docs/02 "Configuration").
	policy := identity.PolicyFrom(cfg.OIDC.Providers)
	unknown, err := policy.UnknownTargets(ctx, d)
	if err != nil {
		return fail("checking the providers' group_map", err)
	}
	for _, u := range unknown {
		log.Warn("a provider's group_map names a group that does not exist, so it grants nothing",
			"provider", u.Provider, "group", u.Slug)
	}
	authh := &auth.Handler{Sealer: sealer, Sessions: sessions, HTTPClient: o.idpClient, Logger: log,
		Login: &identity.Service{DB: d, Sessions: sessions, Logger: log, Bootstrap: cfg.Bootstrap, Perm: perms,
			Policy: policy}}
	if len(cfg.OIDC.Providers) > 0 {
		reg, err := oidc.New(ctx, cfg, oidc.Options{Logger: log, HTTPClient: o.idpClient})
		if err != nil {
			return fail("discovering the identity providers", err)
		}
		reg.Start()
		closers = append(closers, func() error {
			step("close providers")
			return reg.Close()
		})
		authh.Providers = reg
	} else {
		log.Warn("no identity providers are configured, so nobody can sign in")
	}

	// The periodic jobs (docs/02 "Background work"). They start after
	// catch-up, so the collector's quarantine check reads what catch-up
	// found, and stop before the stores close under them. The collector
	// holds while any thread is quarantined: a thread whose log is missing
	// or short may name blobs no log on disk does.
	step("jobs")
	// The threads service is built here, before the listener, because the
	// auto-hide sweep holds it.
	// The one Writer every append goes through, which a backup snapshot
	// holds (TKT-01M29CXK35).
	writer := &posts.Writer{Publish: bus.Publish}
	threadSvc := &threads.Service{
		DB: d, Perm: perms, Threads: cache, Render: render.NewCacheMB(cfg.Storage.RenderedCacheMB),
		Responses:   respcache.NewMB(cfg.Storage.ResponseCacheMB),
		Quarantined: proj.Quarantined,
		Writer:      writer,
		IDs:         threadid.NewGenerator(),
		Project: func(ctx context.Context, id threadid.ID, logLength int64) error {
			_, err := proj.ProjectThread(ctx, id, logLength)
			return err
		},
		Logger: log,
		// GET /search says when results may be missing (TKT-01M29CTY8T).
		SearchIndexing: indexer.Indexing,
	}
	// The blob store (TKT-01M29CXK0K). Posting checks and counts
	// attachments through it, and it asks the threads service who may read
	// the posts that name a blob.
	blobSvc := &blobs.Service{DB: d, Root: cfg.DataDir, Perm: perms, Threads: threadSvc,
		Max: int64(cfg.Limits.AttachmentMB) << 20, Logger: log}
	threadSvc.Attachments = blobSvc
	gc := &jobs.BlobGC{DB: d, Root: cfg.DataDir, Hold: func() string {
		if n := proj.QuarantinedCount(); n > 0 {
			return fmt.Sprintf("%d thread(s) are quarantined", n)
		}
		return ""
	}}
	// The expiry job lifts suspensions through the evaluator the requests
	// use, so the lifted account's cached decisions clear with it.
	autoHide := jobs.AutoHideSweep(threadSvc)
	background := []jobs.Job{jobs.SessionSweep(sessions), gc.Job(),
		jobs.SuspensionExpiry(&account.Service{DB: d, Perm: perms}), autoHide, jobs.FlagAuditCatchUp(threadSvc),
		jobs.FinishMoves(threadSvc)}
	if o.job != nil {
		background = append(background, *o.job)
	}
	jobsWait := busDrain
	if o.jobsWait > 0 {
		jobsWait = o.jobsWait
	}
	// The auto-hide sweep runs once now as well as every minute, so a hide
	// owed from before a restart is not left to the scheduler's jittered
	// first run (docs/08 "Flags"). A failure is logged and serve goes on:
	// the flags still say what is owed, and the next run retries it.
	if n, err := threadSvc.SweepAutoHide(ctx); err != nil {
		log.Warn("the startup auto-hide sweep failed; the next run retries it", "hidden", n, "err", err)
	} else if n > 0 {
		log.Info("the startup auto-hide sweep hid posts", "hidden", n)
	}
	// A move of posts that a crash or a failed write left part-way is
	// finished from the logs before anything is served (docs/03 "How a move
	// is written", TKT-01M29CXJZZ): until then its posts are in neither
	// thread. A failure is logged and serve goes on, as the sweep above
	// does; the minute job retries it.
	if n, err := threadSvc.FinishMoves(ctx); err != nil {
		log.Warn("finishing unfinished moves at startup failed; the next run retries it", "finished", n, "err", err)
	} else if n > 0 {
		log.Info("finished moves left unfinished", "finished", n)
	}
	sched := jobs.New(log, background...)
	if err := sched.Start(run); err != nil {
		return fail("starting the background jobs", err)
	}
	jobsDone = sched.Done()
	closers = append(closers, func() error {
		step("stop jobs")
		return sched.Stop(jobsWait)
	})

	// The control socket `ketju backup` asks for a snapshot through (docs/02
	// "Backups"). Where a peer's uid cannot be checked, or the path is too
	// long for a socket, serve goes on and only an offline backup works. It
	// closes before the jobs, the bus and the stores, which a snapshot uses.
	step("control socket")
	ctl, err := backup.Listen(cfg.DataDir, backup.Options{Logger: log,
		Snapshot: func(ctx context.Context) (backup.Result, error) {
			return backup.Snapshot(ctx, backup.Deps{DataDir: cfg.DataDir, DB: d, Hold: writer.Hold,
				CatchUp: func(ctx context.Context) error {
					_, err := proj.CatchUp(ctx, reopen)
					return err
				},
				Cap: o.pauseCap, WhileHeld: o.snapshotHold})
		}})
	switch {
	case errors.Is(err, backup.ErrUnsupported), errors.Is(err, backup.ErrPathTooLong):
		log.Warn("no control socket, so ketju backup works only with serve stopped", "err", err)
		ctl = nil
	case err != nil:
		return fail("opening the control socket", err)
	}
	closers = append(closers, func() error {
		step("stop control socket")
		if ctl == nil {
			return nil
		}
		return ctl.Close(busDrain)
	})

	step("listener")
	userSvc := &users.Service{DB: d, Perm: perms, QueueCount: threadSvc.QueueCount, Quarantined: proj.QuarantinedIDs,
		VisibleNotifications: threadSvc.CountVisibleNotifications}
	forumSvc := &forums.Service{DB: d, Perm: perms, Logger: log}
	tmpl, err := spa.Shell()
	if err != nil {
		return fail("reading the client shell", err)
	}
	shell, err := web.NewShell(tmpl, log)
	if err != nil {
		return fail("parsing the client shell", err)
	}
	shell.SetPage(api.ShellPage(userSvc))

	// The data-directory probe runs in the background, the first probe
	// included: healthz reads its cached result and never touches the disk
	// itself. Startup waits up to busDrain for the first result, so the first
	// poll after the listener opens has an answer. Shutdown stops the loop
	// before the lock is released, but waits for it no longer than busDrain.
	// A probe stuck in an fsync cannot be interrupted, and an unbounded wait
	// at either end would hold the lock for as long as the disk is stuck
	// (reviews 1152 and 1155). Past a bound serve goes on, and healthz
	// reports the directory as failing until a probe completes. Each process
	// probes a file of its own, so one still in flight after its lock is
	// released cannot touch this one's (review 1153); what such a process,
	// or one that crashed mid-probe, left behind is cleared here, under the
	// lock and before the first probe.
	if err := health.RemoveStale(cfg.DataDir); err != nil {
		log.Warn("could not remove a stale health probe file", "err", err)
	}
	probe := health.NewDirProbe(cfg.DataDir, log)
	stopProbe := probe.Start(busDrain)
	closers = append(closers, func() error {
		step("stop health probe")
		return stopProbe()
	})
	if !probe.WaitFirst(busDrain) {
		log.Warn("the first health probe did not finish; healthz reports the data directory as failing until one does",
			"waited", busDrain)
	}

	ms.Register(collect.Sources{Responses: threadSvc.Responses, Render: threadSvc.Render, Threads: cache, Hub: hub,
		Bus: bus, Quarantined: proj.QuarantinedCount,
		Lag: &collect.ProjectionLag{DB: d, Root: cfg.DataDir, Log: log.Warn}})
	if o.metrics != nil {
		o.metrics(ms)
	}

	agentCreds := &agentcred.Store{DB: d}
	var root http.Handler = web.New(web.Groups{
		API: api.New(api.Deps{Threads: threadSvc, Users: userSvc, Forums: forumSvc, Forms: formSvc,
			Applications: &applications.Service{DB: d}, Perm: perms, Blobs: blobSvc, Logger: log,
			// The settings routes list and end sessions through the store the
			// session middleware reads, and count only the providers
			// configured now when a sign-in is removed (TKT-01M29CCC0X).
			Sessions: sessions, Providers: identity.ProviderNames(cfg.OIDC.Providers)}),
		Auth:   auth.New(authh),
		Blobs:  blobsweb.New(blobSvc, log),
		Events: &sse.Handler{Hub: hub, Threads: threadSvc, Sessions: sessions, Credentials: agentCreds, Logger: log},
		Assets: spa.Assets(),
		// The web app manifest, named by site_name, read per request as
		// /me reads it, so a rename shows on the browser's next check
		// (TKT-01M29CGBYY).
		Manifest: spa.ManifestHandler(func(ctx context.Context) (string, error) {
			return settings.SiteName.Get(ctx, d)
		}, log),
		Health: health.Handler(health.Options{
			DB: d, Dir: probe, Quarantined: proj.QuarantinedCount, Logger: log,
		}),
		// A member opening a profile under a handle a rename released is
		// redirected, as the API redirects (TKT-01M3NRP3W9).
		SPA: api.ProfileRedirects(userSvc, shell),
	}, web.Options{
		Logger:  log,
		Limits:  web.Limits{Upload: int64(cfg.Limits.AttachmentMB) << 20},
		SiteURL: cfg.SiteURL,
		// A bearer credential decides a request that carries one, and every
		// other request goes to the session middleware (TKT-01M29P9RSW).
		Session:  agentcred.Middleware(agentCreds, session.Middleware(sessions, log), log),
		Requests: ms.Requests,
	})
	if o.wrap != nil {
		root = o.wrap(root)
	}

	// The metrics listener (docs/02 "Observability"): /metrics and nothing
	// else, unauthenticated, on an address of its own, and only when one is
	// configured. It opens before the main listener so a failure to bind
	// stops the start before anyone is served, and it closes first, before
	// any store a scrape reads. A failure once it is serving is logged and
	// the forum goes on: losing metrics is not a reason to stop serving.
	if cfg.MetricsListen != "" {
		step("metrics")
		mln, err := net.Listen("tcp", cfg.MetricsListen)
		if err != nil {
			return fail("listening for metrics on "+cfg.MetricsListen, err)
		}
		msrv := &http.Server{Handler: ms.Registry.Handler(), ReadHeaderTimeout: 10 * time.Second,
			ErrorLog: slog.NewLogLogger(log.Handler(), slog.LevelWarn)}
		go func() {
			if err := msrv.Serve(mln); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Error("the metrics listener failed; the forum is still served", "err", err)
			}
		}()
		closers = append(closers, func() error {
			step("stop metrics")
			sctx, cancel := context.WithTimeout(context.Background(), busDrain)
			defer cancel()
			if err := msrv.Shutdown(sctx); err != nil {
				return errors.Join(fmt.Errorf("serve: stopping the metrics listener: %w", err), msrv.Close())
			}
			return nil
		})
		log.Info("serving metrics", "metrics_listen", mln.Addr().String())
		if o.metricsReady != nil {
			o.metricsReady(mln.Addr().String())
		}
	}

	ln, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return fail("listening on "+cfg.Listen, err)
	}
	if o.listen != nil {
		ln = o.listen(ln)
	}
	srv := &http.Server{Handler: root, ReadHeaderTimeout: 10 * time.Second, ErrorLog: slog.NewLogLogger(log.Handler(), slog.LevelWarn)}
	// A live-update stream never finishes by itself, and Shutdown waits for
	// every handler, so the hub ends them all as the drain begins.
	srv.RegisterOnShutdown(hub.Close)
	bi := buildinfo.Get()
	log.Info("ketju started",
		"version", bi.Version, "commit", bi.Commit,
		"listen", ln.Addr().String(), "data_dir", cfg.DataDir,
		"subscribers", bus.Subscribers(), "open_threads", cache.Bound(),
		"caught_up", report.Closed, "corrupt", len(report.Corrupt), "catch_up_elapsed", report.Elapsed,
		"search_indexing", indexer.Indexing())
	if o.ready != nil {
		o.ready(ln.Addr().String())
	}

	// A listener that fails stops accepting, but the requests it already
	// accepted are still running, so it shuts down the same way a signal
	// does: drain them before the closers take the stores away (review
	// 471). Its error is returned with anything shutdown reports.
	served := make(chan error, 1)
	go func() { served <- srv.Serve(ln) }()
	var serveErr error
	select {
	case err := <-served:
		serveErr = fail("serving", err)
		log.Error("the listener failed; shutting down", "err", err)
	case <-ctx.Done():
	}

	step("drain")
	dctx, cancel := context.WithTimeout(context.Background(), drain)
	defer cancel()
	// A failure here is returned, joined with anything the closers report,
	// and the closers still run: the actors flush whatever the listener did
	// (review 470).
	switch err := srv.Shutdown(dctx); {
	case errors.Is(err, context.DeadlineExceeded):
		// Requests still running past the drain are cut. The shutdown goes
		// on: the actors still flush, which is what a restart needs.
		log.Warn("requests still running after the drain; closing them", "drain", drain)
		if cerr := srv.Close(); cerr != nil {
			return errors.Join(serveErr, fail("closing the listener after the drain", cerr))
		}
	case err != nil:
		return errors.Join(serveErr, fail("shutting the listener down", err))
	default:
		log.Info("requests drained")
	}
	return serveErr
}

// archived is what the projection knows of a thread's archive, for the
// thread cache to seal it and evict it first: its row's flag and the log
// length projection_state says it has applied, read in one transaction so
// the two agree. The cache walks the log past that length itself, so a
// thread with no row, or a read that fails, costs a walk of its log and
// is never taken as live when its log says otherwise (TKT-01M29CRJ64).
func archived(d *db.DB) func(threadid.ID) threadcache.ArchiveState {
	return func(id threadid.ID) threadcache.ArchiveState {
		var st threadcache.ArchiveState
		_ = d.Read(context.Background(), func(q *dbq.Queries, _ *sql.Tx) error {
			ps, err := q.GetProjectionState(context.Background(), int64(id))
			if err != nil {
				return nil
			}
			row, err := q.GetThread(context.Background(), int64(id))
			if err != nil {
				return nil
			}
			st = threadcache.ArchiveState{Archived: row.Archived != 0, Projected: ps.LogLength}
			return nil
		})
		return st
	}
}
