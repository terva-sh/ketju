// Package health serves /healthz: whether this process can still do its job,
// for a supervisor or a load balancer that polls every few seconds.
//
// A check that answered 200 whenever the listener was up would tell a
// supervisor nothing the TCP connection had not. This one checks the two
// failures that leave the process listening but useless -- the database cannot
// be queried, the data directory cannot be written -- and reports quarantined
// threads as degraded (docs/02 "Observability", docs/03 "Durability and
// recovery").
//
// The endpoint is unauthenticated, so what it says is fixed text: a status and
// one word per check. No version, path, error text, count, thread id or
// configuration value is in a response. The detail goes to the log, once per
// transition.
package health

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/terva-sh/ketju/internal/problem"
)

// Pinger is the database as the health check sees it: one statement, answered
// or not. *db.DB implements it.
type Pinger interface {
	Ping(ctx context.Context) error
}

// DefaultTimeout bounds the database check. A query that takes longer than
// this is a database that cannot be queried, as far as a supervisor is
// concerned.
const DefaultTimeout = time.Second

// Options configures Handler.
type Options struct {
	// DB is pinged once per request. Required.
	DB Pinger

	// Dir is the data-directory probe, read from its cache. Required.
	Dir *DirProbe

	// Quarantined is how many threads are quarantined
	// (projection.Writer.QuarantinedCount). Nil means none.
	Quarantined func() int

	// Logger receives the database check's transitions.
	Logger *slog.Logger

	// Timeout bounds the database check; zero means DefaultTimeout.
	Timeout time.Duration
}

// Status is the overall answer.
type Status string

const (
	// StatusOK is every check passing: 200.
	StatusOK Status = "ok"
	// StatusDegraded is a process that serves, with some threads quarantined:
	// 200. A 503 here would make a supervisor or a load balancer take down the
	// whole forum over one bad file, which is what quarantine exists to avoid,
	// and a restart cannot repair a quarantined thread.
	StatusDegraded Status = "degraded"
	// StatusUnavailable is the database or the data directory failing: 503.
	StatusUnavailable Status = "unavailable"
)

// Body is the response. Its key set is a contract with whoever polls it, and
// a golden test holds it.
type Body struct {
	Status Status `json:"status"`
	Checks Checks `json:"checks"`
}

// Checks is one word per check.
type Checks struct {
	Database string `json:"database"` // "ok" or "failing"
	DataDir  string `json:"data_dir"` // "ok" or "failing"
	Threads  string `json:"threads"`  // "ok" or "quarantined"
}

const (
	checkOK          = "ok"
	checkFailing     = "failing"
	checkQuarantined = "quarantined"
)

// Handler returns the /healthz handler.
func Handler(opts Options) http.Handler {
	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	h := &handler{db: opts.DB, dir: opts.Dir, quarantined: opts.Quarantined, log: log, timeout: timeout}
	return h
}

type handler struct {
	db          Pinger
	dir         *DirProbe
	quarantined func() int
	log         *slog.Logger
	timeout     time.Duration

	dbFailed atomic.Bool // for logging the database check's transitions once
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		problem.Write(w, r, problem.CodeMethodNotAllowed)
		return
	}
	b := h.check(r.Context())
	code := http.StatusOK
	if b.Status == StatusUnavailable {
		code = http.StatusServiceUnavailable
	}
	out, _ := json.Marshal(b)
	out = append(out, '\n')

	hd := w.Header()
	hd.Set("Content-Type", "application/json")
	hd.Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(out)
}

// check runs the checks: one database statement, one atomic read, one count.
func (h *handler) check(ctx context.Context) Body {
	b := Body{Status: StatusOK, Checks: Checks{Database: checkOK, DataDir: checkOK, Threads: checkOK}}

	if !h.pingDB(ctx) {
		b.Checks.Database = checkFailing
	}
	if h.dir == nil || !h.dir.OK() {
		b.Checks.DataDir = checkFailing
	}
	if h.quarantined != nil && h.quarantined() > 0 {
		b.Checks.Threads = checkQuarantined
	}

	switch {
	case b.Checks.Database != checkOK || b.Checks.DataDir != checkOK:
		b.Status = StatusUnavailable
	case b.Checks.Threads != checkOK:
		b.Status = StatusDegraded
	}
	return b
}

func (h *handler) pingDB(ctx context.Context) bool {
	if h.db == nil {
		return false
	}
	pctx, cancel := context.WithTimeout(ctx, h.timeout)
	defer cancel()
	err := h.db.Ping(pctx)
	switch {
	case err != nil && ctx.Err() != nil:
		// The poller hung up. That says nothing about the database, so it is
		// neither logged nor counted as a transition.
	case err != nil && h.dbFailed.CompareAndSwap(false, true):
		h.log.Warn("health: the database cannot be queried", "err", err)
	case err == nil && h.dbFailed.CompareAndSwap(true, false):
		h.log.Info("health: the database answers again")
	}
	return err == nil
}
