package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/terva-sh/ketju/internal/auth/oidc"
	"github.com/terva-sh/ketju/internal/auth/seal"
	"github.com/terva-sh/ketju/internal/auth/session"
	"github.com/terva-sh/ketju/internal/config"
	"github.com/terva-sh/ketju/internal/events"
	"github.com/terva-sh/ketju/internal/events/projection"
	"github.com/terva-sh/ketju/internal/jobs"
	"github.com/terva-sh/ketju/internal/lock"
	"github.com/terva-sh/ketju/internal/logging"
	"github.com/terva-sh/ketju/internal/perm"
	"github.com/terva-sh/ketju/internal/render"
	"github.com/terva-sh/ketju/internal/service/forums"
	"github.com/terva-sh/ketju/internal/service/identity"
	"github.com/terva-sh/ketju/internal/service/posts"
	"github.com/terva-sh/ketju/internal/service/respcache"
	"github.com/terva-sh/ketju/internal/service/threads"
	"github.com/terva-sh/ketju/internal/service/users"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadcache"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/web"
	"github.com/terva-sh/ketju/internal/web/api"
	"github.com/terva-sh/ketju/internal/web/auth"
	"github.com/terva-sh/ketju/internal/web/health"
	"github.com/terva-sh/ketju/internal/web/spa"
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

	// wrap wraps the root handler, so a test can mount a handler of its own
	// under the real middleware.
	wrap func(http.Handler) http.Handler

	// threads is handed the thread cache once it is open, so a test can
	// append through the same actors serve will flush.
	threads func(*threadcache.Cache)

	// runReturned is called as the projection runner returns, which is how
	// a test holds it back to see that shutdown waits for it.
	runReturned func()

	// listen wraps the listener, so a test can make accepting fail.
	listen func(net.Listener) net.Listener

	// idpClient reaches the identity providers; nil is http.DefaultClient. A
	// test passes one that trusts its in-process IdP.
	idpClient *http.Client

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

	step("database")
	for _, sub := range []string{threadid.ThreadsDir, "blobs"} {
		if err := os.MkdirAll(filepath.Join(cfg.DataDir, sub), 0o750); err != nil {
			return fail("creating "+sub, err)
		}
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

	step("stores")
	cache, err := threadcache.New(cfg.DataDir, threadcache.Options{
		Bound: cfg.Storage.OpenThreads, Sync: cfg.SyncPolicy(), Archived: archived(d),
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

	step("catch-up")
	report, err := proj.CatchUp(ctx, reopen)
	if err != nil {
		return fail("catching up the projection", err)
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
	// One evaluator for the process: sign-in promotes through it (bootstrap),
	// and its cache is the one every request reads.
	perms := perm.New(d)
	authh := &auth.Handler{Sealer: sealer, Sessions: sessions, HTTPClient: o.idpClient, Logger: log,
		Login: &identity.Service{DB: d, Sessions: sessions, Logger: log, Bootstrap: cfg.Bootstrap, Perm: perms}}
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
	gc := &jobs.BlobGC{DB: d, Root: cfg.DataDir, Hold: func() string {
		if n := proj.QuarantinedCount(); n > 0 {
			return fmt.Sprintf("%d thread(s) are quarantined", n)
		}
		return ""
	}}
	background := []jobs.Job{jobs.SessionSweep(sessions), gc.Job()}
	if o.job != nil {
		background = append(background, *o.job)
	}
	jobsWait := busDrain
	if o.jobsWait > 0 {
		jobsWait = o.jobsWait
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

	step("listener")
	userSvc := &users.Service{DB: d, Perm: perms}
	forumSvc := &forums.Service{DB: d, Perm: perms, Logger: log}
	threadSvc := &threads.Service{
		DB: d, Perm: perms, Threads: cache, Render: render.NewCacheMB(cfg.Storage.RenderedCacheMB),
		Responses:   respcache.NewMB(cfg.Storage.ResponseCacheMB),
		Quarantined: proj.Quarantined,
		Writer:      &posts.Writer{Publish: bus.Publish},
		IDs:         threadid.NewGenerator(),
		Project: func(ctx context.Context, id threadid.ID, logLength int64) error {
			_, err := proj.ProjectThread(ctx, id, logLength)
			return err
		},
	}
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

	var root http.Handler = web.New(web.Groups{
		API:    api.New(api.Deps{Threads: threadSvc, Users: userSvc, Forums: forumSvc, Perm: perms, Logger: log}),
		Auth:   auth.New(authh),
		Assets: spa.Assets(),
		Health: health.Handler(health.Options{
			DB: d, Dir: probe, Quarantined: proj.QuarantinedCount, Logger: log,
		}),
		SPA: shell,
	}, web.Options{
		Logger:  log,
		Limits:  web.Limits{Upload: int64(cfg.Limits.AttachmentMB) << 20},
		SiteURL: cfg.SiteURL,
		Session: session.Middleware(sessions, log),
	})
	if o.wrap != nil {
		root = o.wrap(root)
	}

	ln, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return fail("listening on "+cfg.Listen, err)
	}
	if o.listen != nil {
		ln = o.listen(ln)
	}
	srv := &http.Server{Handler: root, ReadHeaderTimeout: 10 * time.Second, ErrorLog: slog.NewLogLogger(log.Handler(), slog.LevelWarn)}
	log.Info("ketju started",
		"listen", ln.Addr().String(), "data_dir", cfg.DataDir,
		"subscribers", bus.Subscribers(), "open_threads", cache.Bound(),
		"caught_up", report.Closed, "corrupt", len(report.Corrupt), "catch_up_elapsed", report.Elapsed)
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

// archived reports whether a thread is archived, from its projection row,
// for the thread cache to evict it first. A thread with no row, or a read
// that fails, is taken as live: evicting a live thread early costs a reopen,
// while pinning an archived one costs only a slot.
func archived(d *db.DB) func(threadid.ID) bool {
	return func(id threadid.ID) bool {
		var a bool
		_ = d.Read(context.Background(), func(q *dbq.Queries, _ *sql.Tx) error {
			row, err := q.GetThread(context.Background(), int64(id))
			a = err == nil && row.Archived != 0
			return nil
		})
		return a
	}
}
