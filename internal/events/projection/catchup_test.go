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
	inner := reopenWith(c, &calls)
	rep, err := f.w.CatchUp(t.Context(), func(ctx context.Context, id threadid.ID) (int64, error) {
		if id == bad {
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
