package threadlog

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"
)

// recorder captures the ORDER of operations across both files.
//
// Both hard criteria on this ticket are about sequence and count rather than
// about a return value — one fsync per batch, and the index header written only
// after the log fsync returns — and neither can be shown by inspecting the
// files afterwards. A shared log of who did what, in what order, can show both.
type recorder struct {
	mu  sync.Mutex
	ops []string
}

func (r *recorder) note(format string, args ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ops = append(r.ops, fmt.Sprintf(format, args...))
}

func (r *recorder) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.ops...)
}

func (r *recorder) count(op string) int {
	n := 0
	for _, s := range r.snapshot() {
		if s == op {
			n++
		}
	}
	return n
}

// fakeLog is an append-only file that records and can be made to fail.
type fakeLog struct {
	rec  *recorder
	mu   sync.Mutex
	data []byte

	failWrite error
	failSync  error

	// gate, when non-nil, blocks the FIRST Sync until it is closed. That is how
	// a batch is formed deterministically: hold the actor inside a sync while
	// more appends queue behind it.
	gate     chan struct{}
	gateOnce sync.Once
}

func (f *fakeLog) Write(p []byte) (int, error) {
	f.rec.note("log.write")
	if f.failWrite != nil {
		return 0, f.failWrite
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.data = append(f.data, p...)
	return len(p), nil
}

func (f *fakeLog) Sync() error {
	if f.gate != nil {
		f.gateOnce.Do(func() { <-f.gate })
	}
	f.rec.note("log.sync")
	return f.failSync
}

func (f *fakeLog) bytes() []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]byte(nil), f.data...)
}

// fakeIndex is a sparse file that records every write with its offset.
type fakeIndex struct {
	rec  *recorder
	mu   sync.Mutex
	data []byte

	failWriteAt error
}

func (f *fakeIndex) WriteAt(p []byte, off int64) (int, error) {
	if off == 0 {
		f.rec.note("index.header")
	} else {
		f.rec.note("index.entry")
	}
	if f.failWriteAt != nil {
		return 0, f.failWriteAt
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if need := int(off) + len(p); need > len(f.data) {
		f.data = append(f.data, make([]byte, need-len(f.data))...)
	}
	copy(f.data[off:], p)
	return len(p), nil
}

func (f *fakeIndex) Sync() error {
	f.rec.note("index.sync")
	return nil
}

func (f *fakeIndex) bytes() []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]byte(nil), f.data...)
}

// newThread returns an actor over a fresh log holding only a file header.
func newThread(t *testing.T, policy SyncPolicy) (*Actor, *fakeLog, *fakeIndex, *recorder) {
	t.Helper()
	rec := &recorder{}
	lg := &fakeLog{rec: rec, data: NewFileHeader(1, 1_700_000_000_000, 7).AppendTo(nil)}
	ixf := &fakeIndex{rec: rec}
	ix := &Index{Header: IndexHeader{ThreadID: 1}}
	return NewActor(ix, int64(len(lg.data)), lg, ixf, policy), lg, ixf, rec
}

func post(src string) Append {
	return Append{Type: TypePost, Author: 42, Body: &PostBody{Source: src}}
}

// Criterion: concurrent appends to one thread produce a gapless ordinal
// sequence with no duplicates, under the race detector.
func TestConcurrentAppendsProduceGaplessOrdinals(t *testing.T) {
	const writers = 64
	a, lg, _, _ := newThread(t, SyncGroup)

	var wg sync.WaitGroup
	got := make([]uint32, writers)
	errs := make([]error, writers)

	for i := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := a.Append(post(fmt.Sprintf("post from writer %d", i)))
			got[i], errs[i] = res.Ordinal, err
		}()
	}
	wg.Wait()
	if err := a.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	for i, err := range errs {
		if err != nil {
			t.Fatalf("writer %d: %v", i, err)
		}
	}

	seen := append([]uint32(nil), got...)
	sort.Slice(seen, func(i, j int) bool { return seen[i] < seen[j] })
	for i, ord := range seen {
		if ord != uint32(i+1) {
			t.Fatalf("ordinals are not 1..%d exactly once: position %d is %d\n%v",
				writers, i, ord, seen)
		}
	}

	// And the log agrees with what the callers were told.
	rec, err := Open(bytes.NewReader(lg.bytes()), int64(len(lg.bytes())), nil)
	if err != nil {
		t.Fatalf("replaying what the actor wrote: %v", err)
	}
	if rec.Index.Len() != writers {
		t.Errorf("log holds %d posts, %d were acknowledged", rec.Index.Len(), writers)
	}
}

// Criterion: appends arriving within the group window issue one fsync,
// verified by counting syscalls.
//
// The batch is formed deterministically rather than by hoping the scheduler
// cooperates: the first Sync blocks on a gate, more appends queue behind it,
// and releasing the gate lets the actor drain them all into one commit.
func TestQueuedAppendsShareOneFsync(t *testing.T) {
	rec := &recorder{}
	gate := make(chan struct{})
	lg := &fakeLog{rec: rec, data: NewFileHeader(1, 1, 1).AppendTo(nil), gate: gate}
	ixf := &fakeIndex{rec: rec}
	a := NewActor(&Index{Header: IndexHeader{ThreadID: 1}}, int64(len(lg.data)), lg, ixf, SyncGroup)

	const queued = 16
	var wg sync.WaitGroup

	// One append to occupy the actor; it blocks inside the gated Sync.
	wg.Add(1)
	go func() { defer wg.Done(); a.Append(post("first")) }() //nolint:errcheck

	// The rest pile up behind it. They are all sends on an unbuffered channel,
	// so once each Append has entered the send it is queued in the runtime and
	// the drain will collect it.
	for i := range queued {
		wg.Add(1)
		go func() { defer wg.Done(); a.Append(post(fmt.Sprintf("queued %d", i))) }() //nolint:errcheck
	}

	close(gate)
	wg.Wait()
	if err := a.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	syncs := rec.count("log.sync")
	if syncs > queued {
		t.Errorf("%d appends cost %d log fsyncs; drain-batching is not collapsing them",
			queued+1, syncs)
	}
	if syncs < 1 {
		t.Fatal("no fsync at all")
	}
	// One write per fsync: the batch is written in a single call, not one per
	// record, so that the fsync covers contiguous bytes.
	if w := rec.count("log.write"); w != syncs {
		t.Errorf("%d log writes against %d fsyncs; each commit should write once and sync once", w, syncs)
	}
	t.Logf("%d appends committed in %d batches", queued+1, syncs)

	// SyncAlways is the control: no batching, one fsync each.
	rec2 := &recorder{}
	lg2 := &fakeLog{rec: rec2, data: NewFileHeader(1, 1, 1).AppendTo(nil)}
	a2 := NewActor(&Index{Header: IndexHeader{ThreadID: 1}}, int64(len(lg2.data)), lg2, &fakeIndex{rec: rec2}, SyncAlways)
	for i := range 4 {
		if _, err := a2.Append(post(fmt.Sprintf("n %d", i))); err != nil {
			t.Fatal(err)
		}
	}
	if err := a2.Close(); err != nil {
		t.Fatal(err)
	}
	if n := rec2.count("log.sync"); n != 4 {
		t.Errorf("SyncAlways issued %d fsyncs for 4 appends, want 4", n)
	}
}

// Criterion: the index header is written only after the log fsync returns,
// proven by a test that injects a failure between the two.
//
// Two halves. First the order is asserted on a successful append. Then the log
// fsync is made to fail, and the index must show no sign of having run.
func TestIndexHeaderFollowsTheLogFsync(t *testing.T) {
	t.Run("order on success", func(t *testing.T) {
		a, _, _, rec := newThread(t, SyncGroup)
		if _, err := a.Append(post("hello")); err != nil {
			t.Fatal(err)
		}
		if err := a.Close(); err != nil {
			t.Fatal(err)
		}

		ops := rec.snapshot()
		sync := indexOf(ops, "log.sync")
		if sync < 0 {
			t.Fatalf("no log fsync in %v", ops)
		}
		for i, op := range ops {
			if (op == "index.header" || op == "index.entry") && i < sync {
				t.Errorf("%s at position %d ran before the log fsync at %d: %v", op, i, sync, ops)
			}
		}
		if w := indexOf(ops, "log.write"); w > sync {
			t.Errorf("log written after its own fsync: %v", ops)
		}
	})

	t.Run("failure injected at the fsync", func(t *testing.T) {
		rec := &recorder{}
		boom := errors.New("disk went away")
		lg := &fakeLog{rec: rec, data: NewFileHeader(1, 1, 1).AppendTo(nil), failSync: boom}
		ixf := &fakeIndex{rec: rec}
		a := NewActor(&Index{Header: IndexHeader{ThreadID: 1}}, int64(len(lg.data)), lg, ixf, SyncGroup)

		_, err := a.Append(post("never durable"))
		if !errors.Is(err, boom) {
			t.Fatalf("append error = %v, want the injected failure", err)
		}
		_ = a.Close()

		for _, op := range rec.snapshot() {
			if op == "index.header" || op == "index.entry" {
				t.Fatalf("the index was touched although the log fsync failed: %v", rec.snapshot())
			}
		}
		if len(ixf.bytes()) != 0 {
			t.Errorf("index file has %d bytes after a failed fsync, want 0", len(ixf.bytes()))
		}
	})

	t.Run("failure injected at the log write", func(t *testing.T) {
		rec := &recorder{}
		boom := errors.New("no space")
		lg := &fakeLog{rec: rec, data: NewFileHeader(1, 1, 1).AppendTo(nil), failWrite: boom}
		ixf := &fakeIndex{rec: rec}
		a := NewActor(&Index{Header: IndexHeader{ThreadID: 1}}, int64(len(lg.data)), lg, ixf, SyncGroup)

		if _, err := a.Append(post("x")); !errors.Is(err, boom) {
			t.Fatalf("append error = %v, want the injected failure", err)
		}
		_ = a.Close()

		if rec.count("log.sync") != 0 {
			t.Error("fsynced a write that failed")
		}
		if rec.count("index.entry") != 0 {
			t.Error("wrote an index entry for a record that never reached the log")
		}
	})
}

// An index write failure must NOT fail the append: the record is already
// durable in the log, which is the source of truth. It is reported at Close and
// the header is left stale so the next open rebuilds.
func TestIndexFailureDoesNotFailADurableAppend(t *testing.T) {
	rec := &recorder{}
	boom := errors.New("index volume full")
	lg := &fakeLog{rec: rec, data: NewFileHeader(1, 1, 1).AppendTo(nil)}
	ixf := &fakeIndex{rec: rec, failWriteAt: boom}
	a := NewActor(&Index{Header: IndexHeader{ThreadID: 1}}, int64(len(lg.data)), lg, ixf, SyncGroup)

	res, err := a.Append(post("durable regardless"))
	if err != nil {
		t.Fatalf("an append whose record reached the log must succeed: %v", err)
	}
	if res.Ordinal != 1 {
		t.Errorf("ordinal = %d, want 1", res.Ordinal)
	}

	if err := a.Close(); !errors.Is(err, boom) {
		t.Errorf("Close error = %v, want the index failure surfaced here", err)
	}

	// The record really is in the log, and a reader recovers it.
	got, err := Open(bytes.NewReader(lg.bytes()), int64(len(lg.bytes())), ixf.bytes())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if got.Index.Len() != 1 {
		t.Errorf("recovered %d posts, want 1", got.Index.Len())
	}
	if got.Outcome == Trusted {
		t.Error("the index header was written despite the entry failing; " +
			"a stale header is the safe outcome here")
	}
}

// Criterion: an append returns ordinal, revision, and the new log length.
func TestAppendReturnsOrdinalRevisionAndLogLength(t *testing.T) {
	a, lg, _, _ := newThread(t, SyncGroup)

	first, err := a.Append(post("one"))
	if err != nil {
		t.Fatal(err)
	}
	if first.Ordinal != 1 || first.Revision != 1 {
		t.Errorf("first post = ordinal %d revision %d, want 1 and 1", first.Ordinal, first.Revision)
	}
	if first.LogLength != int64(len(lg.bytes())) {
		t.Errorf("LogLength = %d, the log is %d bytes", first.LogLength, len(lg.bytes()))
	}

	second, err := a.Append(post("two"))
	if err != nil {
		t.Fatal(err)
	}
	if second.Ordinal != 2 {
		t.Errorf("second post ordinal = %d, want 2", second.Ordinal)
	}
	if second.LogLength <= first.LogLength {
		t.Errorf("log length did not grow: %d then %d", first.LogLength, second.LogLength)
	}

	// The actor owns revisions too: an edit gets the next one without the
	// caller having to know or guess.
	edit, err := a.Append(Append{Type: TypeEdit, Ordinal: 1, Author: 42,
		Body: &EditBody{Source: "one, corrected", Reason: "typo"}})
	if err != nil {
		t.Fatal(err)
	}
	if edit.Revision != 2 {
		t.Errorf("edit revision = %d, want 2", edit.Revision)
	}
	again, err := a.Append(Append{Type: TypeEdit, Ordinal: 1, Author: 42,
		Body: &EditBody{Source: "one, corrected twice"}})
	if err != nil {
		t.Fatal(err)
	}
	if again.Revision != 3 {
		t.Errorf("second edit revision = %d, want 3", again.Revision)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
}

// Several posts in ONE batch must still get consecutive ordinals. An actor that
// read only the committed index would hand every post in a batch the same
// number, which is precisely the bug the single-writer design claims to make
// impossible.
func TestPostsInOneBatchGetConsecutiveOrdinals(t *testing.T) {
	rec := &recorder{}
	gate := make(chan struct{})
	lg := &fakeLog{rec: rec, data: NewFileHeader(1, 1, 1).AppendTo(nil), gate: gate}
	a := NewActor(&Index{Header: IndexHeader{ThreadID: 1}}, int64(len(lg.data)), lg, &fakeIndex{rec: rec}, SyncGroup)

	const n = 12
	var wg sync.WaitGroup
	ords := make([]uint32, n)
	wg.Add(1)
	go func() { defer wg.Done(); r, _ := a.Append(post("gate holder")); ords[0] = r.Ordinal }()
	for i := 1; i < n; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); r, _ := a.Append(post("batched")); ords[i] = r.Ordinal }()
	}
	close(gate)
	wg.Wait()
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}

	sorted := append([]uint32(nil), ords...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	for i, o := range sorted {
		if o != uint32(i+1) {
			t.Fatalf("batched ordinals are not 1..%d: %v", n, sorted)
		}
	}
}

// A bad request fails alone. It must not take the rest of its batch with it,
// and must not leave anything on disk.
func TestABadRequestFailsWithoutPoisoningTheBatch(t *testing.T) {
	a, lg, _, _ := newThread(t, SyncGroup)

	if _, err := a.Append(Append{Type: TypeEdit, Ordinal: 99, Body: &EditBody{Source: "x"}}); err == nil {
		t.Error("an edit to a post that does not exist should fail")
	}
	if _, err := a.Append(Append{Type: TypeEdit, Ordinal: 0, Body: &EditBody{Source: "x"}}); !errors.Is(err, ErrMalformed) {
		t.Error("an edit naming no ordinal should be ErrMalformed")
	}

	res, err := a.Append(post("still works"))
	if err != nil {
		t.Fatalf("a good append after two bad ones: %v", err)
	}
	if res.Ordinal != 1 {
		t.Errorf("ordinal = %d, want 1: the failed requests must not have consumed a number", res.Ordinal)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}

	got, err := Open(bytes.NewReader(lg.bytes()), int64(len(lg.bytes())), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Index.Len() != 1 {
		t.Errorf("log holds %d posts, want exactly the one that succeeded", got.Index.Len())
	}
}

func TestAppendAfterCloseIsRefused(t *testing.T) {
	a, _, _, _ := newThread(t, SyncGroup)
	if _, err := a.Append(post("before")); err != nil {
		t.Fatal(err)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Append(post("after")); !errors.Is(err, ErrActorClosed) {
		t.Errorf("append after close = %v, want ErrActorClosed", err)
	}
	if err := a.Close(); err != nil {
		t.Errorf("Close twice: %v", err)
	}
}

// End to end on a real filesystem: what the actor wrote reopens as a trusted
// index with no replay, which is the pair of files actually agreeing rather
// than two in-memory structures agreeing.
func TestActorOutputReopensAsTrusted(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "t.log")
	idxPath := filepath.Join(dir, "t.idx")

	if err := os.WriteFile(logPath, NewFileHeader(9, 1_700_000_000_000, 3).AppendTo(nil), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(idxPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	lf, err := os.OpenFile(logPath, os.O_RDWR|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer lf.Close()
	xf, err := os.OpenFile(idxPath, os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer xf.Close()

	fi, err := lf.Stat()
	if err != nil {
		t.Fatal(err)
	}

	a := NewActor(&Index{Header: IndexHeader{ThreadID: 9}}, fi.Size(), lf, xf, SyncGroup)
	for i := range 20 {
		if _, err := a.Append(post(fmt.Sprintf("post %d", i))); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := a.Append(Append{Type: TypeDelete, Ordinal: 3, Author: 1,
		Body: &DeleteBody{Reason: "spam"}}); err != nil {
		t.Fatal(err)
	}
	if err := a.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	idx, err := os.ReadFile(idxPath)
	if err != nil {
		t.Fatal(err)
	}
	logFile, err := os.Open(logPath)
	if err != nil {
		t.Fatal(err)
	}
	defer logFile.Close()
	st, err := logFile.Stat()
	if err != nil {
		t.Fatal(err)
	}

	rec, err := Open(logFile, st.Size(), idx)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if rec.Outcome != Trusted {
		t.Errorf("outcome = %s, want trusted: the actor should leave an index that needs no replay", rec.Outcome)
	}
	if rec.Index.Len() != 20 {
		t.Errorf("recovered %d posts, want 20", rec.Index.Len())
	}
	dead, err := rec.Index.Tombstoned(3)
	if err != nil {
		t.Fatal(err)
	}
	if !dead {
		t.Error("post 3 was deleted and the reopened index does not say so")
	}

	// And a full rebuild from the log alone agrees with what the actor left.
	scratch, err := Open(logFile, st.Size(), nil)
	if err != nil {
		t.Fatal(err)
	}
	sameIndex(t, rec.Index, scratch.Index, "actor against replay")
}

func indexOf(ops []string, want string) int {
	for i, op := range ops {
		if op == want {
			return i
		}
	}
	return -1
}
