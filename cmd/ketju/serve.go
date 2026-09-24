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

	"github.com/terva-sh/ketju/internal/auth/session"
	"github.com/terva-sh/ketju/internal/config"
	"github.com/terva-sh/ketju/internal/events"
	"github.com/terva-sh/ketju/internal/events/projection"
	"github.com/terva-sh/ketju/internal/lock"
	"github.com/terva-sh/ketju/internal/logging"
	"github.com/terva-sh/ketju/internal/perm"
	"github.com/terva-sh/ketju/internal/render"
	"github.com/terva-sh/ketju/internal/service/threads"
	"github.com/terva-sh/ketju/internal/service/users"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadcache"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/web"
	"github.com/terva-sh/ketju/internal/web/api"
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

// configPath is --config, else KETJU_CONFIG, else ./config.toml (settled in
// grooming for TKT-01M29BYYJG).
func configPath(flagValue string, getenv func(string) string) string {
	switch {
	case flagValue != "":
		return flagValue
	case getenv("KETJU_CONFIG") != "":
		return getenv("KETJU_CONFIG")
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
	closers = append(closers, func() error {
		step("close database")
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
	go proj.Run(run)

	step("listener")
	perms := perm.New(d)
	userSvc := &users.Service{DB: d, Perm: perms}
	threadSvc := &threads.Service{
		DB: d, Perm: perms, Threads: cache, Render: render.NewCacheMB(cfg.Storage.RenderedCacheMB),
		Quarantined: proj.Quarantined,
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
	var root http.Handler = web.New(web.Groups{
		API:    api.New(api.Deps{Threads: threadSvc, Users: userSvc, Logger: log}),
		Assets: spa.Assets(),
		SPA:    shell,
	}, web.Options{
		Logger:  log,
		Limits:  web.Limits{Upload: int64(cfg.Limits.AttachmentMB) << 20},
		SiteURL: cfg.SiteURL,
		Session: session.Middleware(&session.Store{DB: d}, log),
	})
	if o.wrap != nil {
		root = o.wrap(root)
	}

	ln, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return fail("listening on "+cfg.Listen, err)
	}
	srv := &http.Server{Handler: root, ReadHeaderTimeout: 10 * time.Second, ErrorLog: slog.NewLogLogger(log.Handler(), slog.LevelWarn)}
	log.Info("ketju started",
		"listen", ln.Addr().String(), "data_dir", cfg.DataDir,
		"subscribers", bus.Subscribers(), "open_threads", cache.Bound(),
		"caught_up", report.Closed, "corrupt", len(report.Corrupt), "catch_up_elapsed", report.Elapsed)
	if o.ready != nil {
		o.ready(ln.Addr().String())
	}

	served := make(chan error, 1)
	go func() { served <- srv.Serve(ln) }()
	select {
	case err := <-served:
		return fail("serving", err)
	case <-ctx.Done():
	}

	step("drain")
	dctx, cancel := context.WithTimeout(context.Background(), drain)
	defer cancel()
	switch err := srv.Shutdown(dctx); {
	case errors.Is(err, context.DeadlineExceeded):
		// Requests still running past the drain are cut. The shutdown goes
		// on: the actors still flush, which is what a restart needs.
		log.Warn("requests still running after the drain; closing them", "drain", drain)
		srv.Close()
	case err != nil:
		log.Error("shutting the listener down", "err", err)
	default:
		log.Info("requests drained")
	}
	return nil
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
