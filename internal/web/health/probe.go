package health

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

// Interval is how often the data-directory probe runs (settled in grooming:
// every 10 s, with healthz reading the cached result).
const Interval = 10 * time.Second

// ProbePrefix starts the name of the file a probe creates and removes in the
// data directory. The rest of the name is random and fixed for the life of a
// DirProbe, so each process probes a file of its own (review 1153): a probe
// from a process whose shutdown gave up waiting on it can still be inside a
// write after the lock has passed to the next process, and a shared name
// would let the two remove each other's file.
const ProbePrefix = ".healthz-probe-"

// DirProbe checks that a data directory is writable, off the request path.
//
// A probe creates its file, writes to it, fsyncs it and removes it: the
// failures that leave the process listening but useless are a read-only
// remount, a full disk and a lost permission, and each of them fails one of
// those steps. The result is kept in an atomic, so a request reads it without
// touching the disk.
//
// A result older than two intervals counts as failed. Without that bound a
// probe hung on a stuck disk would leave its last good answer in place for as
// long as it hung, which is the one failure a health check exists to report.
type DirProbe struct {
	dir      string
	path     string // dir/ProbePrefix+random: this probe's own file
	log      *slog.Logger
	interval time.Duration

	// now and probe are seams for tests: a fake clock for staleness, and an
	// injected fault for a directory that cannot be made unwritable (a test
	// run as root).
	now   func() time.Time
	probe func(path string) error

	last  atomic.Pointer[result]
	first chan struct{} // closed once the first Probe has finished
	once  sync.Once

	mu     sync.Mutex // serialises Probe, so transitions are logged once
	failed bool       // the last probe's outcome, for transition logging
}

type result struct {
	ok bool
	at time.Time
}

// NewDirProbe returns a probe of dir. It has no result until Probe or Run has
// run once, and reports failure until then.
func NewDirProbe(dir string, log *slog.Logger) *DirProbe {
	if log == nil {
		log = slog.Default()
	}
	return &DirProbe{dir: dir, path: filepath.Join(dir, ProbePrefix+rand.Text()), log: log,
		interval: Interval, now: time.Now, probe: writeProbe, first: make(chan struct{})}
}

// RemoveStale removes probe files left in dir by a process that died partway
// through a probe, or whose shutdown stopped waiting on one. Call it only
// while holding the data-directory lock and before starting a probe, as serve
// does: no other process is probing then, and a process that gave up its
// lock with a probe still in flight loses nothing by the removal but the
// last step of a probe whose answer nobody reads.
func RemoveStale(dir string) error {
	matches, err := filepath.Glob(filepath.Join(dir, ProbePrefix+"*"))
	if err != nil {
		return err
	}
	var errs []error
	for _, m := range matches {
		if err := os.Remove(m); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Probe runs one check now and records its result.
func (p *DirProbe) Probe() {
	p.mu.Lock()
	defer p.mu.Unlock()
	err := p.probe(p.path)
	p.last.Store(&result{ok: err == nil, at: p.now()})
	switch {
	case err != nil && !p.failed:
		p.log.Warn("health: the data directory is not writable", "err", err)
	case err == nil && p.failed:
		p.log.Info("health: the data directory is writable again")
	}
	p.failed = err != nil
	p.once.Do(func() { close(p.first) })
}

// WaitFirst waits until the first probe has finished, or for wait, whichever
// is sooner, and reports whether the probe finished. Serve calls it before
// the listener opens so the first poll has an answer, without letting a
// probe stuck on the disk hold startup: past wait, startup goes on and
// healthz reports the directory as failing until a probe completes.
func (p *DirProbe) WaitFirst(wait time.Duration) bool {
	t := time.NewTimer(wait)
	defer t.Stop()
	select {
	case <-p.first:
		return true
	case <-t.C:
		return false
	}
}

// Run probes on entry and then every interval until ctx is cancelled.
func (p *DirProbe) Run(ctx context.Context) {
	p.Probe()
	t := time.NewTicker(p.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			p.Probe()
		}
	}
}

// Start runs Run in a goroutine and returns the function that stops it.
// Every probe, the first included, runs in that goroutine, so none of them
// is outside the bound Stop places on waiting (review 1155).
//
// Stop cancels the loop and waits for it, but for no longer than wait. A
// probe blocked in a write or an fsync does not see a cancelled context, and
// a stuck disk is exactly when that happens; an unbounded wait would hang
// shutdown and never release the data-directory lock. Past wait, Stop
// returns an error saying so and leaves the probe to finish on its own.
func (p *DirProbe) Start(wait time.Duration) (stop func() error) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		p.Run(ctx)
	}()
	return func() error {
		cancel()
		t := time.NewTimer(wait)
		defer t.Stop()
		select {
		case <-done:
			return nil
		case <-t.C:
			return fmt.Errorf("health: the data-directory probe did not stop within %s; "+
				"a write or fsync in %s is stuck", wait, p.dir)
		}
	}
}

// OK reports whether the last probe passed and is recent enough to trust.
func (p *DirProbe) OK() bool {
	r := p.last.Load()
	return r != nil && r.ok && p.now().Sub(r.at) <= 2*p.interval
}

// writeProbe is the check itself: create, write, fsync, close, remove.
func writeProbe(path string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	_, werr := f.Write([]byte("ok\n"))
	serr := f.Sync()
	cerr := f.Close()
	rerr := os.Remove(path)
	return errors.Join(werr, serr, cerr, rerr)
}
