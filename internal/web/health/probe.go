package health

import (
	"context"
	"errors"
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

// ProbeFile is the file the probe creates and removes in the data directory.
const ProbeFile = ".healthz-probe"

// DirProbe checks that a data directory is writable, off the request path.
//
// A probe creates ProbeFile, writes to it, fsyncs it and removes it: the
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
	log      *slog.Logger
	interval time.Duration

	// now and probe are seams for tests: a fake clock for staleness, and an
	// injected fault for a directory that cannot be made unwritable (a test
	// run as root).
	now   func() time.Time
	probe func(dir string) error

	last atomic.Pointer[result]

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
	return &DirProbe{dir: dir, log: log, interval: Interval, now: time.Now, probe: writeProbe}
}

// Probe runs one check now and records its result.
func (p *DirProbe) Probe() {
	p.mu.Lock()
	defer p.mu.Unlock()
	err := p.probe(p.dir)
	p.last.Store(&result{ok: err == nil, at: p.now()})
	switch {
	case err != nil && !p.failed:
		p.log.Warn("health: the data directory is not writable", "err", err)
	case err == nil && p.failed:
		p.log.Info("health: the data directory is writable again")
	}
	p.failed = err != nil
}

// Run probes every interval until ctx is cancelled. It does not probe on
// entry; call Probe first when the first answer matters, as serve does
// before it opens the listener.
func (p *DirProbe) Run(ctx context.Context) {
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

// OK reports whether the last probe passed and is recent enough to trust.
func (p *DirProbe) OK() bool {
	r := p.last.Load()
	return r != nil && r.ok && p.now().Sub(r.at) <= 2*p.interval
}

// writeProbe is the check itself: create, write, fsync, close, remove. A
// leftover file from a probe that died partway is truncated, not refused.
func writeProbe(dir string) error {
	path := filepath.Join(dir, ProbeFile)
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
