package projection

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadcache"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// syncBuffer is a log sink the race detector is content with.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// logs sends the writer's log lines to a buffer the test can read.
func (f *fixture) logs() *syncBuffer {
	buf := &syncBuffer{}
	f.w.log = slog.New(slog.NewTextHandler(buf, nil))
	return buf
}

// reopen is the Recover serve supplies: open the thread through a cache,
// which repairs a torn tail, and report its length. It counts its calls.
func reopenWith(c *threadcache.Cache, calls *atomic.Int64) Recover {
	return func(_ context.Context, id threadid.ID) (int64, error) {
		calls.Add(1)
		var n int64
		err := c.With(id, func(th *threadcache.Thread) error {
			n = th.Actor.LogLength()
			return nil
		})
		return n, err
	}
}

// restart closes the fixture's cache, as a process exit does, and returns a
// fresh one over the same logs.
func (f *fixture) restart() *threadcache.Cache {
	f.t.Helper()
	if err := f.cache.Close(); err != nil {
		f.t.Fatal(err)
	}
	c, err := threadcache.New(f.root, threadcache.Options{Sync: threadlog.SyncNever})
	if err != nil {
		f.t.Fatal(err)
	}
	f.t.Cleanup(func() { c.Close() })
	f.cache = c
	return c
}

// projected is every projection_state row, by thread.
func (f *fixture) projected() map[threadid.ID]int64 {
	f.t.Helper()
	out := map[threadid.ID]int64{}
	err := f.db.Read(f.t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
		rows, err := q.ListProjectionLengths(f.t.Context())
		for _, r := range rows {
			out[threadid.ID(r.ThreadID)] = r.LogLength
		}
		return err
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return out
}

func logSize(t *testing.T, root string, id threadid.ID) int64 {
	t.Helper()
	fi, err := os.Stat(id.LogPath(root))
	if err != nil {
		t.Fatal(err)
	}
	return fi.Size()
}

// TestCatchUpClosesGap: a thread projected part of the way, one never
// projected at all, and one already current. Catch-up brings every
// projection_state to its log's length, opening only the two with a gap, and
// the rows it leaves are the rows projecting from scratch would.
func TestCatchUpClosesGap(t *testing.T) {
	f := newFixture(t)
	buf := f.logs()

	partial, created := f.thread("Partial")
	f.project(partial, created)
	f.post(partial, 5, 0)
	f.post(partial, 6, 1, 5)

	never, _ := f.thread("Never")
	f.post(never, 5, 0)

	current, _ := f.thread("Current")
	end := f.post(current, 5, 0)
	f.project(current, end)

	c := f.restart()
	if _, ok := f.w.LastCatchUp(); ok {
		t.Error("a report before any catch-up ran")
	}
	var calls atomic.Int64
	rep, err := f.w.CatchUp(t.Context(), reopenWith(c, &calls))
	if err != nil {
		t.Fatal(err)
	}
	if last, ok := f.w.LastCatchUp(); !ok || last.Closed != rep.Closed || last.Elapsed != rep.Elapsed {
		t.Errorf("last report = %+v, %t; want the one just returned", last, ok)
	}
	if rep.Scanned != 3 || rep.Closed != 2 || len(rep.Corrupt) != 0 || len(rep.Failed) != 0 {
		t.Fatalf("report = %+v", rep)
	}
	if calls.Load() != 2 {
		t.Errorf("opened %d threads, want only the 2 with a gap", calls.Load())
	}
	wantBytes := logSize(t, f.root, partial) - created + logSize(t, f.root, never) - threadlog.FileHeaderSize
	if rep.Bytes != wantBytes {
		t.Errorf("bytes replayed = %d, want %d", rep.Bytes, wantBytes)
	}
	for id, n := range f.projected() {
		if size := logSize(t, f.root, id); n != size {
			t.Errorf("thread %s projected to %d, log is %d", id, n, size)
		}
	}
	f.agreesWithIndex(partial)
	f.agreesWithIndex(never)

	got := f.dump()
	f.clear()
	for _, id := range []threadid.ID{partial, never, current} {
		f.project(id, logSize(t, f.root, id))
	}
	if want := f.dump(); got != want {
		t.Errorf("catch-up left different rows from a projection from scratch\n got: %s\nwant: %s", got, want)
	}

	line := buf.String()
	for _, part := range []string{"projection catch-up finished", "scanned=3", "closed=2", "elapsed="} {
		if !strings.Contains(line, part) {
			t.Errorf("the summary line does not carry %q:\n%s", part, line)
		}
	}
}

// TestLogShorterThanProjectionIsCorruption is AC1: a log shorter than its
// projection is reported and quarantined, its row is left as it was, and the
// other threads are untouched. A projection whose log is gone is the same
// case. Nothing is opened to find either.
func TestLogShorterThanProjectionIsCorruption(t *testing.T) {
	f := newFixture(t)
	buf := f.logs()

	short, _ := f.thread("Short")
	f.post(short, 5, 0)
	shortEnd := f.post(short, 5, 0)
	f.project(short, shortEnd)

	gone, goneEnd := f.thread("Gone")
	f.project(gone, goneEnd)

	fine, fineEnd := f.thread("Fine")
	f.project(fine, fineEnd)

	c := f.restart()
	if err := os.Truncate(short.LogPath(f.root), shortEnd-10); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(gone.LogPath(f.root)); err != nil {
		t.Fatal(err)
	}
	before := f.dump()

	var calls atomic.Int64
	rep, err := f.w.CatchUp(t.Context(), reopenWith(c, &calls))
	if err != nil {
		t.Fatal(err)
	}
	want := []threadid.ID{short, gone}
	slices.Sort(want)
	if !slices.Equal(rep.Corrupt, want) {
		t.Fatalf("corrupt = %v, want %v", rep.Corrupt, want)
	}
	if calls.Load() != 0 {
		t.Errorf("opened %d threads to find corruption the sizes already showed", calls.Load())
	}
	if f.dump() != before {
		t.Error("catch-up rewrote the rows of a corrupt thread")
	}
	if !f.w.Quarantined(short) || !f.w.Quarantined(gone) || f.w.Quarantined(fine) {
		t.Errorf("quarantined: short %t, gone %t, fine %t", f.w.Quarantined(short), f.w.Quarantined(gone), f.w.Quarantined(fine))
	}
	logged := buf.String()
	for _, id := range want {
		if !strings.Contains(logged, "level=ERROR") || !strings.Contains(logged, "thread="+id.String()) {
			t.Errorf("no error line names thread %s:\n%s", id, logged)
		}
	}
	if !strings.Contains(logged, `fsync is \"never\"`) {
		t.Errorf("the error does not say what makes this ordinary rather than corrupt:\n%s", logged)
	}

	// Repaired, the next catch-up lets it out of quarantine.
	f.exec(`DELETE FROM projection_state WHERE thread_id IN (?, ?)`, int64(short), int64(gone))
	f.exec(`DELETE FROM threads WHERE id = ?`, int64(gone))
	if _, err := f.w.CatchUp(t.Context(), reopenWith(c, &calls)); err != nil {
		t.Fatal(err)
	}
	if f.w.Quarantined(short) || f.w.Quarantined(gone) {
		t.Error("a repaired thread stayed quarantined")
	}
}

// TestTornTailIsNotAGap: a torn final frame makes the file longer than any
// whole record, but it is not a gap. The open through the cache truncates it,
// and the projection is left where the last whole frame ends, not failed on
// the torn bytes and not reported as corrupt.
func TestTornTailIsNotAGap(t *testing.T) {
	f := newFixture(t)
	current, end := f.thread("Current")
	f.project(current, end)

	behind, created := f.thread("Behind")
	f.project(behind, created)
	behindEnd := f.post(behind, 5, 0)

	c := f.restart()
	tear := func(id threadid.ID) {
		lf, err := os.OpenFile(id.LogPath(f.root), os.O_WRONLY|os.O_APPEND, 0)
		if err != nil {
			t.Fatal(err)
		}
		defer lf.Close()
		// A length prefix promising far more than follows: a write the crash
		// cut short.
		if _, err := lf.Write([]byte{200, 0, 0, 0, 1, 2, 3}); err != nil {
			t.Fatal(err)
		}
	}
	tear(current)
	tear(behind)

	var calls atomic.Int64
	rep, err := f.w.CatchUp(t.Context(), reopenWith(c, &calls))
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Corrupt) != 0 || len(rep.Failed) != 0 || rep.Closed != 1 {
		t.Fatalf("report = %+v, want the behind thread closed and nothing corrupt or failed", rep)
	}
	p := f.projected()
	if p[current] != end || p[behind] != behindEnd {
		t.Errorf("projected current %d (want %d), behind %d (want %d)", p[current], end, p[behind], behindEnd)
	}
	if got := logSize(t, f.root, behind); got != behindEnd {
		t.Errorf("the torn tail was not truncated by the open: log is %d, want %d", got, behindEnd)
	}
}

// A log cut below its projection can still end in a torn frame that makes
// the file longer than the projection. The sizes alone read that as a gap;
// the open removes the torn frame and shows a log shorter than its
// projection, which is corruption and must not be "caught up" to.
func TestCutLogWithATornTailIsCorruption(t *testing.T) {
	f := newFixture(t)
	f.logs()
	id, _ := f.thread("Cut")
	kept := f.post(id, 5, 0)
	end := f.post(id, 5, 0)
	f.project(id, end)
	c := f.restart()

	if err := os.Truncate(id.LogPath(f.root), kept); err != nil {
		t.Fatal(err)
	}
	lf, err := os.OpenFile(id.LogPath(f.root), os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	junk := make([]byte, end-kept+20)
	junk[0] = 200 // a length prefix promising more than the file holds
	if _, err := lf.Write(junk); err != nil {
		t.Fatal(err)
	}
	lf.Close()
	if logSize(t, f.root, id) <= end {
		t.Fatal("the setup did not make the file longer than the projection")
	}
	// Without its index the open replays the log, which is where the cut
	// shows. An index still claiming the old length is recovery's to judge,
	// not catch-up's.
	if err := os.Remove(id.IndexPath(f.root)); err != nil {
		t.Fatal(err)
	}

	var calls atomic.Int64
	rep, err := f.w.CatchUp(t.Context(), reopenWith(c, &calls))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(rep.Corrupt, []threadid.ID{id}) || rep.Closed != 0 {
		t.Fatalf("report = %+v, want the thread corrupt and nothing closed", rep)
	}
	if !f.w.Quarantined(id) {
		t.Error("not quarantined")
	}
	if got := f.projected()[id]; got != end {
		t.Errorf("projection_state moved to %d; it must stay at %d", got, end)
	}
}

// TestCatchUpIdempotentAfterPartialRun is AC2: a catch-up that stops partway,
// as a process killed during it does, leaves each thread either closed or
// still showing its gap, and the next run finishes the job with the same
// rows as one uninterrupted run.
func TestCatchUpIdempotentAfterPartialRun(t *testing.T) {
	f := newFixture(t)
	var ids []threadid.ID
	for _, title := range []string{"One", "Two", "Three", "Four", "Five"} {
		id, _ := f.thread(title)
		f.post(id, 5, 0)
		f.post(id, 6, 1)
		ids = append(ids, id)
	}
	slices.Sort(ids)
	c := f.restart()

	// The run dies after closing two threads: the context is cancelled from
	// inside the third open, as a kill would stop it there.
	ctx, cancel := context.WithCancel(t.Context())
	var calls atomic.Int64
	inner := reopenWith(c, &calls)
	_, err := f.w.CatchUp(ctx, func(ctx context.Context, id threadid.ID) (int64, error) {
		if calls.Load() == 2 {
			cancel()
			return 0, ctx.Err()
		}
		return inner(ctx, id)
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("an interrupted catch-up returned %v", err)
	}
	p := f.projected()
	for i, id := range ids {
		closed := p[id] == logSize(t, f.root, id)
		if closed != (i < 2) {
			t.Errorf("thread %d: closed %t after a run stopped at the third", i, closed)
		}
	}

	if _, err := f.w.CatchUp(t.Context(), reopenWith(c, &calls)); err != nil {
		t.Fatal(err)
	}
	resumed := f.dump()

	f.clear()
	if _, err := f.w.CatchUp(t.Context(), reopenWith(c, &calls)); err != nil {
		t.Fatal(err)
	}
	if clean := f.dump(); resumed != clean {
		t.Errorf("a resumed catch-up differs from an uninterrupted one\nresumed: %s\n  clean: %s", resumed, clean)
	}
}

// A thread with a gap that cannot be opened is reported and left behind; the
// catch-up goes on to the others and still succeeds, because one bad thread
// must not keep the forum from starting.
func TestUnopenableGapIsReportedNotFatal(t *testing.T) {
	f := newFixture(t)
	buf := f.logs()
	bad, _ := f.thread("Bad")
	good, _ := f.thread("Good")
	c := f.restart()

	var calls atomic.Int64
	var readable atomic.Bool
	inner := reopenWith(c, &calls)
	rep, err := f.w.CatchUp(t.Context(), func(ctx context.Context, id threadid.ID) (int64, error) {
		if id == bad && !readable.Load() {
			return 0, errors.New("mid-log damage")
		}
		return inner(ctx, id)
	})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(rep.Failed, []threadid.ID{bad}) || rep.Closed != 1 || len(rep.Corrupt) != 0 {
		t.Fatalf("report = %+v", rep)
	}
	if f.projected()[good] != logSize(t, f.root, good) {
		t.Error("the good thread was not caught up")
	}
	if f.w.Quarantined(bad) {
		t.Error("an unopenable thread was quarantined; that is for a log shorter than its projection")
	}
	if !strings.Contains(buf.String(), "mid-log damage") {
		t.Errorf("the failure was not logged with its cause:\n%s", buf.String())
	}

	// Held, not forgotten: Lag reports it, and Run's retry closes it once
	// the log can be read (review 448).
	size := logSize(t, f.root, bad)
	if l := f.w.Lag(); l.Threads != 1 || l.Bytes != size-threadlog.FileHeaderSize {
		t.Errorf("lag = %+v, want the unopenable thread held with its whole gap", l)
	}
	if err := f.w.Retry(t.Context()); err == nil {
		t.Error("a retry while the thread still cannot be opened succeeded")
	}
	if l := f.w.Lag(); l.Threads != 1 {
		t.Errorf("lag after a failed retry = %+v, want the thread still held", l)
	}
	readable.Store(true)
	if err := f.w.Retry(t.Context()); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if got := f.projected()[bad]; got != size {
		t.Errorf("after a retry the thread is projected to %d of %d", got, size)
	}
	if l := f.w.Lag(); l.Threads != 0 {
		t.Errorf("lag after the retry = %+v", l)
	}
}

// A thread whose projection fails once opened joins the behind set, so the
// writer's Run retries it; catch-up does not have to be run again.
func TestFailedProjectionStaysBehind(t *testing.T) {
	f := newFixture(t)
	f.logs()
	id, _ := f.thread("Hello")
	f.post(id, 5, 0)
	// The thread opens, but its log cannot be read for projecting.
	c := f.restart()
	f.w.open = func(string) (logFile, error) { return nil, errors.New("disk on fire") }

	var calls atomic.Int64
	rep, err := f.w.CatchUp(t.Context(), reopenWith(c, &calls))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(rep.Failed, []threadid.ID{id}) {
		t.Fatalf("failed = %v", rep.Failed)
	}
	if l := f.w.Lag(); l.Threads != 1 {
		t.Errorf("lag = %+v, want the thread held for Run to retry", l)
	}
}

func TestCatchUpReportsStrays(t *testing.T) {
	f := newFixture(t)
	id, end := f.thread("Hello")
	f.project(id, end)
	c := f.restart()

	top := filepath.Join(f.root, threadid.ThreadsDir)
	other := threadid.ID(uint64(0x7f)<<56 | 1)
	wrongShard := filepath.Join(id.Dir(f.root), other.Hex()+threadid.LogExt)
	strays := []string{
		filepath.Join(top, "README"),
		filepath.Join(id.Dir(f.root), other.Hex()+threadid.IndexExt),
		filepath.Join(id.Dir(f.root), "not-an-id"+threadid.IndexExt),
		filepath.Join(top, id.Hex()[0:2], "stray"),
		filepath.Join(id.Dir(f.root), "notes.txt"),
		wrongShard,
	}
	for _, p := range strays {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var calls atomic.Int64
	rep, err := f.w.CatchUp(t.Context(), reopenWith(c, &calls))
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(strays)
	got := slices.Clone(rep.Strays)
	slices.Sort(got)
	if !slices.Equal(got, strays) {
		t.Errorf("strays = %v, want %v", got, strays)
	}
	if rep.Scanned != 1 || len(rep.Corrupt) != 0 || len(rep.Failed) != 0 {
		t.Errorf("report = %+v; a stray is not a thread", rep)
	}
}

func TestCatchUpEdges(t *testing.T) {
	f := newFixture(t)
	if _, err := f.w.CatchUp(t.Context(), nil); err == nil {
		t.Error("a catch-up with no way to open a thread ran")
	}
	var calls atomic.Int64
	rep, err := f.w.CatchUp(t.Context(), reopenWith(f.cache, &calls))
	if err != nil || rep.Scanned != 0 {
		t.Errorf("no threads directory yet: %+v, %v", rep, err)
	}

	id, _ := f.thread("Hello")
	f.restart()
	if err := os.Chmod(id.Dir(f.root), 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(id.Dir(f.root), 0o755) })
	if os.Geteuid() != 0 {
		if _, err := f.w.CatchUp(t.Context(), reopenWith(f.cache, &calls)); err == nil {
			t.Error("a walk that could not read a shard directory succeeded")
		}
	}
}

// tenThousand lays out n threads whose logs are a bare file header, each
// with a projection_state row that matches: a board with nothing to catch
// up. It returns a counter of every log the writer opens.
func (f *fixture) tenThousand(n int) *atomic.Int64 {
	f.t.Helper()
	var ids []threadid.ID
	for i := range n {
		id := threadid.ID(uint64(1_700_000_000_000+int64(i)*3_600_000)<<16 | uint64(i))
		if err := os.MkdirAll(id.Dir(f.root), 0o755); err != nil {
			f.t.Fatal(err)
		}
		hdr := threadlog.NewFileHeader(uint64(id), 1_700_000_000_000, 7).AppendTo(nil)
		if err := os.WriteFile(id.LogPath(f.root), hdr, 0o644); err != nil {
			f.t.Fatal(err)
		}
		ids = append(ids, id)
	}
	err := f.db.Write(f.t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
		for _, id := range ids {
			if err := q.SetProjectionState(f.t.Context(), dbq.SetProjectionStateParams{
				ThreadID: int64(id), LogLength: threadlog.FileHeaderSize, UpdatedAt: 1}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		f.t.Fatal(err)
	}
	var opens atomic.Int64
	open := f.w.open
	f.w.open = func(path string) (logFile, error) {
		opens.Add(1)
		return open(path)
	}
	return &opens
}

// TestCatchUpTenThousandNoDrift is AC4's "without opening every log": ten
// thousand threads with nothing to catch up are all scanned and none is
// opened, either to project or to recover. The time bound is held apart, in
// catchup_bound_test.go, because -race says nothing about speed.
func TestCatchUpTenThousandNoDrift(t *testing.T) {
	if testing.Short() {
		t.Skip("writes ten thousand logs")
	}
	f := newFixture(t)
	opens := f.tenThousand(10_000)
	var reopens atomic.Int64
	rep, err := f.w.CatchUp(t.Context(), reopenWith(f.cache, &reopens))
	if err != nil {
		t.Fatal(err)
	}
	if rep.Scanned != 10_000 || rep.Closed != 0 || len(rep.Corrupt) != 0 || len(rep.Failed) != 0 || len(rep.Strays) != 0 {
		t.Fatalf("report = %+v", rep)
	}
	if opens.Load() != 0 || reopens.Load() != 0 {
		t.Errorf("opened %d logs and recovered %d threads with no drift anywhere", opens.Load(), reopens.Load())
	}
}

// flaky is a Recover that fails until it is let through, standing in for an
// open that fails once and then works.
func flaky(c *threadcache.Cache) (Recover, *atomic.Bool) {
	var ok atomic.Bool
	var calls atomic.Int64
	inner := reopenWith(c, &calls)
	return func(ctx context.Context, id threadid.ID) (int64, error) {
		if !ok.Load() {
			return 0, errors.New("transient")
		}
		return inner(ctx, id)
	}, &ok
}

func tear(t *testing.T, root string, id threadid.ID) {
	t.Helper()
	lf, err := os.OpenFile(id.LogPath(root), os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer lf.Close()
	if _, err := lf.Write([]byte{200, 0, 0, 0, 1, 2, 3}); err != nil {
		t.Fatal(err)
	}
}

// A thread held because its open failed, whose log ends in a torn frame, is
// retried to the length the open reports once it works, not to the file's
// size with the torn bytes in it (review 453).
func TestHeldTornTailRetriesToTheRecoveredLength(t *testing.T) {
	f := newFixture(t)
	f.logs()
	id, created := f.thread("Behind")
	f.project(id, created)
	end := f.post(id, 5, 0)
	c := f.restart()
	tear(t, f.root, id)

	reopen, ok := flaky(c)
	if _, err := f.w.CatchUp(t.Context(), reopen); err != nil {
		t.Fatal(err)
	}
	if l := f.w.Lag(); l.Threads != 1 {
		t.Fatalf("lag = %+v, want the thread held", l)
	}
	ok.Store(true)
	if err := f.w.Retry(t.Context()); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if got := f.projected()[id]; got != end {
		t.Errorf("retried to %d, want the recovered length %d", got, end)
	}
	if l := f.w.Lag(); l.Threads != 0 {
		t.Errorf("lag after the retry = %+v", l)
	}
}

// A held gap that a later look shows was only a torn tail leaves the behind
// set, whether a catch-up or a retry is the later look (review 453).
func TestHeldGapThatWasATornTailIsSettled(t *testing.T) {
	for _, later := range []string{"catch-up", "retry", "catch-up after another open"} {
		t.Run(later, func(t *testing.T) {
			f := newFixture(t)
			f.logs()
			id, end := f.thread("Current")
			f.project(id, end)
			c := f.restart()
			tear(t, f.root, id)

			reopen, ok := flaky(c)
			if _, err := f.w.CatchUp(t.Context(), reopen); err != nil {
				t.Fatal(err)
			}
			if l := f.w.Lag(); l.Threads != 1 {
				t.Fatalf("lag = %+v, want the thread held", l)
			}
			var err error
			switch later {
			case "catch-up":
				ok.Store(true)
				_, err = f.w.CatchUp(t.Context(), reopen)
			case "retry":
				ok.Store(true)
				err = f.w.Retry(t.Context())
			default:
				// Something else opened the thread and cut the torn tail, so
				// the next catch-up finds the sizes equal without opening it.
				var calls atomic.Int64
				if _, err := reopenWith(c, &calls)(t.Context(), id); err != nil {
					t.Fatal(err)
				}
				_, err = f.w.CatchUp(t.Context(), reopen)
			}
			if err != nil {
				t.Fatal(err)
			}
			if l := f.w.Lag(); l.Threads != 0 {
				t.Errorf("lag = %+v after the %s found nothing to project", l, later)
			}
			if got := f.projected()[id]; got != end {
				t.Errorf("projection moved to %d from %d", got, end)
			}
		})
	}
}

// A held thread whose reopen shows its log shorter than its projection is
// quarantined by the retry, not retried forever.
func TestRetryQuarantinesACutLog(t *testing.T) {
	f := newFixture(t)
	f.logs()
	id, _ := f.thread("Cut")
	kept := f.post(id, 5, 0)
	end := f.post(id, 5, 0)
	f.project(id, end)
	c := f.restart()
	tear(t, f.root, id)

	reopen, ok := flaky(c)
	if _, err := f.w.CatchUp(t.Context(), reopen); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(id.LogPath(f.root), kept); err != nil {
		t.Fatal(err)
	}
	os.Remove(id.IndexPath(f.root))
	ok.Store(true)
	f.w.Retry(t.Context())
	if !f.w.Quarantined(id) {
		t.Error("a retry that found the log shorter than its projection did not quarantine it")
	}
	if l := f.w.Lag(); l.Threads != 0 {
		t.Errorf("lag = %+v; a quarantined thread is not behind", l)
	}
	if got := f.projected()[id]; got != end {
		t.Errorf("projection moved to %d from %d", got, end)
	}
}

// A held thread that a later catch-up finds cut below its projection is
// quarantined and leaves the behind set.
func TestCatchUpQuarantinesAHeldThread(t *testing.T) {
	f := newFixture(t)
	f.logs()
	id, _ := f.thread("Cut")
	kept := f.post(id, 5, 0)
	end := f.post(id, 5, 0)
	f.project(id, end)
	c := f.restart()
	tear(t, f.root, id)

	reopen, _ := flaky(c)
	if _, err := f.w.CatchUp(t.Context(), reopen); err != nil {
		t.Fatal(err)
	}
	if l := f.w.Lag(); l.Threads != 1 {
		t.Fatalf("lag = %+v, want the thread held", l)
	}
	if err := os.Truncate(id.LogPath(f.root), kept); err != nil {
		t.Fatal(err)
	}
	if _, err := f.w.CatchUp(t.Context(), reopen); err != nil {
		t.Fatal(err)
	}
	if !f.w.Quarantined(id) {
		t.Error("not quarantined")
	}
	if l := f.w.Lag(); l.Threads != 0 {
		t.Errorf("lag = %+v; a quarantined thread is not behind", l)
	}
}

// A log holding only its file header, which the projection has never seen,
// is a thread whose creation never finished: reported, and neither counted
// as a closed gap nor held, because there is no frame to project and no
// state a projection would record (review 456).
func TestHeaderOnlyLogIsUnfinished(t *testing.T) {
	f := newFixture(t)
	buf := f.logs()
	id := threadid.ID(uint64(1_700_000_000_000)<<16 | 99)
	th, err := f.cache.Create(id, 7, 1_700_000_000_000)
	if err != nil {
		t.Fatal(err)
	}
	f.cache.Release(th)
	c := f.restart()
	if logSize(t, f.root, id) != threadlog.FileHeaderSize {
		t.Fatal("the setup did not leave a header-only log")
	}

	var calls atomic.Int64
	rep, err := f.w.CatchUp(t.Context(), reopenWith(c, &calls))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(rep.Unfinished, []threadid.ID{id}) || len(rep.Failed) != 0 || len(rep.Corrupt) != 0 || rep.Closed != 0 {
		t.Fatalf("report = %+v", rep)
	}
	if l := f.w.Lag(); l.Threads != 0 {
		t.Errorf("lag = %+v; an unfinished thread is not behind", l)
	}
	if _, ok := f.projected()[id]; ok {
		t.Error("a projection_state row was written for a thread that was never created")
	}
	if !strings.Contains(buf.String(), "creation never finished") {
		t.Errorf("not logged:\n%s", buf.String())
	}
}

// A sweep that finds a thread's sizes equal must not clear a hold made after
// its look, for bytes appended since (review 462). Stood in for directly:
// catch-up looked when the log was `created` long, then an append grew it
// and a failed projection held it, and only then does the settle run.
func TestSettleKeepsAHoldNewerThanTheLook(t *testing.T) {
	f := newFixture(t)
	f.logs()
	grower, created := f.thread("Grower")
	f.project(grower, created)
	grown := f.post(grower, 5, 0)
	f.w.hold(grower, created, grown)

	f.w.settle(grower, created) // the look saw the old, equal size
	if l := f.w.Lag(); l.Threads != 1 {
		t.Errorf("lag = %+v: a hold newer than the look was cleared", l)
	}

	// With the log no longer than the look, the same settle clears it.
	f.w.settle(grower, logSize(t, f.root, grower))
	if l := f.w.Lag(); l.Threads != 0 {
		t.Errorf("lag = %+v: a hold no newer than the look was kept", l)
	}
}

// A gap held while the log had a torn first frame, which another open then
// cut back to the bare header, is cleared when catch-up reports the thread
// unfinished (review 462).
func TestUnfinishedSettlesAHeldGap(t *testing.T) {
	f := newFixture(t)
	f.logs()
	id := threadid.ID(uint64(1_700_000_000_000)<<16 | 98)
	th, err := f.cache.Create(id, 7, 1_700_000_000_000)
	if err != nil {
		t.Fatal(err)
	}
	f.cache.Release(th)
	c := f.restart()
	tear(t, f.root, id)

	reopen, _ := flaky(c)
	if _, err := f.w.CatchUp(t.Context(), reopen); err != nil {
		t.Fatal(err)
	}
	if l := f.w.Lag(); l.Threads != 1 {
		t.Fatalf("lag = %+v, want the torn thread held", l)
	}
	var calls atomic.Int64
	if _, err := reopenWith(c, &calls)(t.Context(), id); err != nil { // another open cuts the tail
		t.Fatal(err)
	}
	rep, err := f.w.CatchUp(t.Context(), reopen)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(rep.Unfinished, []threadid.ID{id}) {
		t.Fatalf("report = %+v", rep)
	}
	if l := f.w.Lag(); l.Threads != 0 {
		t.Errorf("lag = %+v: the held gap outlived the log becoming unfinished", l)
	}
}

// An append whose projection fails while Retry is reopening a thread holds
// it at a newer target, and Retry projects to that, not to the length it
// recovered before the append (review 464).
func TestRetryKeepsAHoldMadeWhileReopening(t *testing.T) {
	f := newFixture(t)
	f.logs()
	id, created := f.thread("Racing")
	f.project(id, created)
	c := f.restart()
	tear(t, f.root, id)

	reopen, ok := flaky(c)
	if _, err := f.w.CatchUp(t.Context(), reopen); err != nil {
		t.Fatal(err)
	}
	ok.Store(true)

	var grown int64
	var calls atomic.Int64
	inner := reopenWith(c, &calls)
	f.w.mu.Lock()
	f.w.reopen = func(ctx context.Context, tid threadid.ID) (int64, error) {
		n, err := inner(ctx, tid) // cuts the torn tail: n is the old length
		// Meanwhile a post lands and its projection fails, holding the
		// thread at the new length.
		grown = f.post(tid, 5, 0)
		f.w.hold(tid, created, grown)
		return n, err
	}
	f.w.mu.Unlock()

	if err := f.w.Retry(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := f.projected()[id]; got != grown {
		t.Errorf("retried to %d; the post appended during the reopen, to %d, was dropped", got, grown)
	}
	if l := f.w.Lag(); l.Threads != 0 {
		t.Errorf("lag = %+v", l)
	}
}

// CatchUp and Retry take turns: a catch-up started while a retry is inside
// its reopen waits for the retry to finish (review 464).
func TestCatchUpWaitsForRetry(t *testing.T) {
	f := newFixture(t)
	f.logs()
	id, created := f.thread("Held")
	f.project(id, created)
	c := f.restart()
	tear(t, f.root, id)
	reopen, ok := flaky(c)
	if _, err := f.w.CatchUp(t.Context(), reopen); err != nil {
		t.Fatal(err)
	}
	ok.Store(true)

	var calls atomic.Int64
	inner := reopenWith(c, &calls)
	overlapped := make(chan bool, 1)
	f.w.mu.Lock()
	f.w.reopen = func(ctx context.Context, tid threadid.ID) (int64, error) {
		done := make(chan struct{})
		go func() { f.w.CatchUp(ctx, inner); close(done) }()
		select {
		case <-done:
			overlapped <- true
		case <-time.After(200 * time.Millisecond):
			overlapped <- false
		}
		return inner(ctx, tid)
	}
	f.w.mu.Unlock()

	if err := f.w.Retry(t.Context()); err != nil {
		t.Fatal(err)
	}
	if <-overlapped {
		t.Error("a catch-up ran to completion inside a retry")
	}
}

// interruptAt is a Recover that cancels the sweep from inside the open of
// thread at, as a live sweep's context ends when the bus stops, after
// running during; every other thread opens through inner.
func interruptAt(inner Recover, at threadid.ID, cancel context.CancelFunc, during func()) Recover {
	return func(ctx context.Context, id threadid.ID) (int64, error) {
		if id != at {
			return inner(ctx, id)
		}
		if during != nil {
			during()
		}
		cancel()
		return 0, ctx.Err()
	}
}

// A live sweep interrupted after it has found a held thread corrupt returns
// with that thread quarantined, not dropped from the behind set and left
// out of the quarantine too, which would serve a projection longer than its
// log (review 468). The quarantine is also visible to another goroutine
// while the sweep is still running, as /healthz and the blob collector read
// it.
func TestInterruptedSweepKeepsTheQuarantine(t *testing.T) {
	f := newFixture(t)
	f.logs()
	cut, _ := f.thread("Cut")
	kept := f.post(cut, 5, 0)
	end := f.post(cut, 5, 0)
	f.project(cut, end)
	gap, created := f.thread("Gap")
	f.project(gap, created)
	f.post(gap, 5, 0)
	after, afterEnd := f.thread("After")
	f.project(after, afterEnd)
	if !(cut < gap && gap < after) {
		t.Fatal("the setup relies on the scan meeting cut, gap and after in that order")
	}
	c := f.restart()
	tear(t, f.root, cut)

	// The first sweep cannot open anything, so both gaps are held.
	reopen, ok := flaky(c)
	if _, err := f.w.CatchUp(t.Context(), reopen); err != nil {
		t.Fatal(err)
	}
	if l := f.w.Lag(); l.Threads != 2 {
		t.Fatalf("lag = %+v, want cut and gap held", l)
	}
	ok.Store(true)
	if err := os.Truncate(cut.LogPath(f.root), kept); err != nil {
		t.Fatal(err)
	}

	// The second is cancelled inside gap's open, after cut was found short
	// and before after is reached. From another goroutine, mid-sweep, the
	// quarantine already names cut.
	var seen, count atomic.Int64
	during := func() {
		done := make(chan struct{})
		go func() {
			defer close(done)
			if f.w.Quarantined(cut) {
				seen.Store(1)
			}
			count.Store(int64(f.w.QuarantinedCount()))
		}()
		<-done
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	_, err := f.w.CatchUp(ctx, interruptAt(reopen, gap, cancel, during))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("an interrupted sweep returned %v", err)
	}
	if seen.Load() != 1 || count.Load() != 1 {
		t.Errorf("mid-sweep: Quarantined(cut) %t, QuarantinedCount %d; want true, 1", seen.Load() == 1, count.Load())
	}

	if !f.w.Quarantined(cut) {
		t.Error("an interrupted sweep returned with the corrupt thread it found not quarantined")
	}
	if n := f.w.QuarantinedCount(); n != 1 {
		t.Errorf("QuarantinedCount = %d, want 1", n)
	}
	f.w.mu.Lock()
	_, cutHeld := f.w.behind[cut]
	_, gapHeld := f.w.behind[gap]
	f.w.mu.Unlock()
	if cutHeld || !gapHeld {
		t.Errorf("behind: cut %t, gap %t; want only gap, whose open was interrupted", cutHeld, gapHeld)
	}
	if l := f.w.Lag(); l.Threads != 1 {
		t.Errorf("lag = %+v; a quarantined thread is not behind", l)
	}
	if got := f.projected()[cut]; got != end {
		t.Errorf("projection moved to %d from %d", got, end)
	}
}

// Only a sweep that finishes may replace the quarantine. One interrupted
// after passing a repaired thread leaves it quarantined, because it has not
// looked at every thread; the next that finishes lets it out.
func TestInterruptedSweepDoesNotReleaseTheQuarantine(t *testing.T) {
	f := newFixture(t)
	f.logs()
	fixed, _ := f.thread("Fixed")
	kept := f.post(fixed, 5, 0)
	end := f.post(fixed, 5, 0)
	f.project(fixed, end)
	gap, created := f.thread("Gap")
	f.project(gap, created)
	f.post(gap, 5, 0)
	after, afterEnd := f.thread("After")
	f.project(after, afterEnd)
	if !(fixed < gap && gap < after) {
		t.Fatal("the setup relies on the scan meeting fixed, gap and after in that order")
	}
	c := f.restart()
	if err := os.Truncate(fixed.LogPath(f.root), kept); err != nil {
		t.Fatal(err)
	}

	var calls atomic.Int64
	inner := reopenWith(c, &calls)
	// Found corrupt by a sweep interrupted at gap: quarantined.
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	if _, err := f.w.CatchUp(ctx, interruptAt(inner, gap, cancel, nil)); !errors.Is(err, context.Canceled) {
		t.Fatalf("an interrupted sweep returned %v", err)
	}
	if !f.w.Quarantined(fixed) {
		t.Fatal("not quarantined")
	}

	// An operator repairs it: the projection is brought back to the log.
	f.exec(`UPDATE projection_state SET log_length = ? WHERE thread_id = ?`, kept, int64(fixed))

	// A sweep interrupted after passing it lets nothing out.
	ctx2, cancel2 := context.WithCancel(t.Context())
	defer cancel2()
	if _, err := f.w.CatchUp(ctx2, interruptAt(inner, gap, cancel2, nil)); !errors.Is(err, context.Canceled) {
		t.Fatalf("an interrupted sweep returned %v", err)
	}
	if !f.w.Quarantined(fixed) {
		t.Error("an interrupted sweep released a thread from quarantine")
	}

	// A sweep that finishes does.
	rep, err := f.w.CatchUp(t.Context(), inner)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Corrupt) != 0 || f.w.Quarantined(fixed) || f.w.QuarantinedCount() != 0 {
		t.Errorf("after a complete sweep: corrupt %v, Quarantined(fixed) %t, count %d",
			rep.Corrupt, f.w.Quarantined(fixed), f.w.QuarantinedCount())
	}
}
