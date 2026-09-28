// Package jobs runs ketju's periodic background work in process (docs/02
// "Background work"): the hourly session sweep and the daily blob garbage
// collection, and whatever later joins them.
//
// A job is a function run on an interval. A job that fails or panics is
// logged and runs again at its next interval; nothing a job does can stop
// the scheduler or another job.
package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"runtime/debug"
	"sync"
	"time"
)

// Result is what a run reports: how many things it removed, and anything
// else worth a line in the log, as slog key-value pairs.
type Result struct {
	Removed int64
	Attrs   []any
}

// Job is one piece of periodic work.
type Job struct {
	// Name identifies the job in the log.
	Name string

	// Every is the interval between the end of one run and the start of the
	// next. A run that takes longer than Every delays the next rather than
	// overlapping it.
	Every time.Duration

	// Run does the work. It should return promptly once ctx is cancelled.
	Run func(ctx context.Context) (Result, error)
}

// maxFirstDelay bounds the jittered delay before a job's first run. A
// restart runs every job soon, so a server restarted more often than a
// job's interval still runs it, and the jitter keeps the jobs from all
// starting in the same instant as the listener.
const maxFirstDelay = time.Minute

// Scheduler runs jobs until stopped.
type Scheduler struct {
	log  *slog.Logger
	jobs []Job

	// first is the delay before a job's first run; tests replace it.
	first func(every time.Duration) time.Duration

	mu      sync.Mutex
	started bool
	cancel  context.CancelFunc
	done    chan struct{}
}

// New returns a scheduler for jobs. It does nothing until Start.
func New(log *slog.Logger, jobs ...Job) *Scheduler {
	if log == nil {
		log = slog.Default()
	}
	return &Scheduler{log: log, jobs: jobs, first: jitter}
}

// jitter is a random delay in [0, min(every, maxFirstDelay)).
func jitter(every time.Duration) time.Duration {
	bound := min(every, maxFirstDelay)
	if bound <= 0 {
		return 0
	}
	return rand.N(bound)
}

// Start runs every job on its own goroutine. It returns at once.
func (s *Scheduler) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started {
		return errors.New("jobs: the scheduler is already started")
	}
	for _, j := range s.jobs {
		if j.Name == "" || j.Every <= 0 || j.Run == nil {
			return fmt.Errorf("jobs: job %q needs a name, a positive interval and a function", j.Name)
		}
	}
	s.started = true
	ctx, s.cancel = context.WithCancel(ctx)
	s.done = make(chan struct{})
	var wg sync.WaitGroup
	for _, j := range s.jobs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.loop(ctx, j)
		}()
	}
	go func() { wg.Wait(); close(s.done) }()
	return nil
}

// Done is closed once every job has returned after Stop, or at once if the
// scheduler never started. A caller whose Stop timed out reads it before
// closing anything a job still running could be using.
func (s *Scheduler) Done() <-chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.done == nil {
		c := make(chan struct{})
		close(c)
		return c
	}
	return s.done
}

// Stop cancels every job and waits up to wait for the running ones to
// return. A job still running past it is reported and left to finish on
// its own, with its context cancelled so every database call it makes
// fails; Stop does not wait longer, because a job stuck in the kernel
// would hold shutdown with it. See Done.
func (s *Scheduler) Stop(wait time.Duration) error {
	s.mu.Lock()
	cancel, done := s.cancel, s.done
	s.mu.Unlock()
	if cancel == nil {
		return nil
	}
	cancel()
	select {
	case <-done:
		return nil
	case <-time.After(wait):
		return fmt.Errorf("jobs: a job was still running %s after it was asked to stop", wait)
	}
}

func (s *Scheduler) loop(ctx context.Context, j Job) {
	t := time.NewTimer(s.first(j.Every))
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		s.runOnce(ctx, j)
		t.Reset(j.Every)
	}
}

// runOnce runs a job and logs the outcome. A panic is recovered here, so
// it costs this run and nothing else.
func (s *Scheduler) runOnce(ctx context.Context, j Job) {
	start := time.Now()
	var (
		res      Result
		err      error
		panicked bool
	)
	func() {
		defer func() {
			if p := recover(); p != nil {
				panicked = true
				err = fmt.Errorf("panic: %v", p)
				s.log.ErrorContext(ctx, "job panicked", "job", j.Name, "panic", fmt.Sprint(p),
					"stack", string(debug.Stack()))
			}
		}()
		res, err = j.Run(ctx)
	}()
	attrs := append([]any{"job", j.Name, "removed", res.Removed, "elapsed", time.Since(start)}, res.Attrs...)
	switch {
	case panicked:
		// Already logged, with the stack.
	case err != nil && ctx.Err() != nil:
		s.log.InfoContext(ctx, "job stopped by shutdown", append(attrs, "error", err)...)
	case err != nil:
		s.log.ErrorContext(ctx, "job failed", append(attrs, "error", err)...)
	default:
		s.log.InfoContext(ctx, "job finished", attrs...)
	}
}
