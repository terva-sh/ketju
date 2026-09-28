package jobs

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/auth/session"
	"github.com/terva-sh/ketju/internal/auth/session/sessiontest"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// logBuf is a log sink safe for the scheduler's goroutines.
type logBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *logBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *logBuf) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

func (l *logBuf) logger() *slog.Logger { return slog.New(slog.NewTextHandler(l, nil)) }

// lines returns the log lines that contain every one of parts.
func (l *logBuf) lines(parts ...string) []string {
	var out []string
next:
	for _, line := range strings.Split(l.String(), "\n") {
		for _, p := range parts {
			if !strings.Contains(line, p) {
				continue next
			}
		}
		out = append(out, line)
	}
	return out
}

// immediate starts a scheduler whose jobs first run at once.
func immediate(t *testing.T, log *slog.Logger, jobs ...Job) *Scheduler {
	t.Helper()
	s := New(log, jobs...)
	s.first = func(time.Duration) time.Duration { return 0 }
	if err := s.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Stop(time.Second) })
	return s
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

// TestSchedulerSurvivesErrorAndPanic is AC4: a job that fails and then
// panics runs again at its next interval, and a second job keeps its own
// schedule throughout.
func TestSchedulerSurvivesErrorAndPanic(t *testing.T) {
	var logs logBuf
	var flaky, steady atomic.Int32
	immediate(t, logs.logger(),
		Job{Name: "flaky", Every: 2 * time.Millisecond, Run: func(context.Context) (Result, error) {
			switch flaky.Add(1) {
			case 1:
				return Result{}, errors.New("disk on fire")
			case 2:
				panic("worse")
			default:
				return Result{Removed: 3}, nil
			}
		}},
		Job{Name: "steady", Every: 2 * time.Millisecond, Run: func(context.Context) (Result, error) {
			steady.Add(1)
			return Result{}, nil
		}},
	)
	waitFor(t, "the third run of the flaky job", func() bool { return flaky.Load() >= 3 })
	before := steady.Load()
	waitFor(t, "the steady job to keep ticking", func() bool { return steady.Load() > before+2 })

	for _, want := range [][]string{
		{"job failed", "job=flaky", "disk on fire"},
		{"job panicked", "job=flaky", "worse", "stack="},
		{"job finished", "job=flaky", "removed=3"},
		{"job finished", "job=steady"},
	} {
		if len(logs.lines(want...)) == 0 {
			t.Errorf("no log line with %q in\n%s", want, logs.String())
		}
	}
}

// TestSchedulerStops: after Stop, no job runs again.
func TestSchedulerStops(t *testing.T) {
	var runs atomic.Int32
	s := immediate(t, nil, Job{Name: "tick", Every: time.Millisecond, Run: func(context.Context) (Result, error) {
		runs.Add(1)
		return Result{}, nil
	}})
	waitFor(t, "a run", func() bool { return runs.Load() > 0 })
	if err := s.Stop(time.Second); err != nil {
		t.Fatal(err)
	}
	n := runs.Load()
	time.Sleep(20 * time.Millisecond)
	if runs.Load() != n {
		t.Fatal("a job ran after Stop returned")
	}
}

// TestStopIsBounded: a job that ignores cancellation does not hold shutdown
// past the bound; Stop says so instead.
func TestStopIsBounded(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{})
	s := immediate(t, nil, Job{Name: "stuck", Every: time.Hour, Run: func(context.Context) (Result, error) {
		close(started)
		<-release
		return Result{}, nil
	}})
	defer close(release)
	<-started
	if err := s.Stop(20 * time.Millisecond); err == nil {
		t.Fatal("Stop returned nil with a job still running")
	}
}

func TestStartRefusesABadJob(t *testing.T) {
	for _, j := range []Job{
		{Every: time.Second, Run: func(context.Context) (Result, error) { return Result{}, nil }},
		{Name: "x", Run: func(context.Context) (Result, error) { return Result{}, nil }},
		{Name: "x", Every: time.Second},
	} {
		if err := New(nil, j).Start(t.Context()); err == nil {
			t.Errorf("Start accepted %+v", j)
		}
	}
}

func TestFirstRunIsJitteredWithinAMinute(t *testing.T) {
	for range 100 {
		if d := jitter(time.Hour); d < 0 || d >= time.Minute {
			t.Fatalf("jitter(1h) = %v", d)
		}
		if d := jitter(10 * time.Millisecond); d < 0 || d >= 10*time.Millisecond {
			t.Fatalf("jitter(10ms) = %v", d)
		}
	}
}

// TestTheIntervalsAreTheDesigns: hourly and daily (docs/02).
func TestTheIntervalsAreTheDesigns(t *testing.T) {
	if got := SessionSweep(&session.Store{}).Every; got != time.Hour {
		t.Errorf("session sweep every %v, want 1h", got)
	}
	if got := (&BlobGC{}).Job().Every; got != 24*time.Hour {
		t.Errorf("blob gc every %v, want 24h", got)
	}
}

// TestBothJobsLogWhatTheyRemoved is AC3: each job's "job finished" line
// names it and counts what it removed.
func TestBothJobsLogWhatTheyRemoved(t *testing.T) {
	w := newGCWorld(t)
	w.now = t0.Add(30 * 24 * time.Hour)
	w.upload("one", t0)
	w.upload("two", t0)
	w.upload("young", w.now)

	var uid int64
	w.write(func(q *dbq.Queries, _ *sql.Tx) error {
		var err error
		uid, err = q.CreateUser(t.Context(), dbq.CreateUserParams{Handle: "alice", DisplayName: "Alice",
			Status: "active", Role: "member", Kind: "human", CreatedAt: 1, UpdatedAt: 1})
		return err
	})
	for range 3 {
		sessiontest.SignIn(t, w.db, uid, t0) // idle for 30 days by w.now
	}
	sessiontest.SignIn(t, w.db, uid, w.now)
	store := &session.Store{DB: w.db, Now: func() time.Time { return w.now }}

	var logs logBuf
	immediate(t, logs.logger(), SessionSweep(store), w.gc.Job())
	waitFor(t, "both jobs", func() bool { return len(logs.lines("job finished")) >= 2 })
	for _, want := range [][]string{
		{"job finished", `job="session sweep"`, "removed=3"},
		{"job finished", `job="blob gc"`, "removed=2", "candidates=2"},
	} {
		if len(logs.lines(want...)) != 1 {
			t.Errorf("want one line with %q in\n%s", want, logs.String())
		}
	}
}
