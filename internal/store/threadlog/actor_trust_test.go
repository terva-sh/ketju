package threadlog

import (
	"bytes"
	"errors"
	"sync"
	"testing"
)

// The tests here are TKT-01M3VHSYFR's for the actor. The index header is what
// makes the next open trust every entry in the file, so it must never reach
// the file over an entry that is wrong there: one whose write failed, one
// whose fsync failed, or one a power cut can still lose.

// journalIndex is an index file that remembers what was made durable. Its
// durable image is the file as of the last Sync, and pending holds every
// write since, so a test can build each file a power cut could leave.
type journalIndex struct {
	rec *recorder

	mu      sync.Mutex
	data    []byte
	durable []byte
	pending []indexWrite

	// failEntries fails that many entry writes, the first ones after it is
	// set. failSync fails every Sync.
	failEntries int
	failSync    error
	boom        error
}

type indexWrite struct {
	off int64
	p   []byte
}

func newJournalIndex(rec *recorder, durable []byte) *journalIndex {
	return &journalIndex{rec: rec, data: bytes.Clone(durable), durable: bytes.Clone(durable)}
}

func writeInto(b []byte, off int64, p []byte) []byte {
	if need := int(off) + len(p); need > len(b) {
		b = append(b, make([]byte, need-len(b))...)
	}
	copy(b[off:], p)
	return b
}

func (j *journalIndex) WriteAt(p []byte, off int64) (int, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if off == 0 {
		j.rec.note("index.header")
	} else {
		j.rec.note("index.entry")
		if j.failEntries > 0 {
			j.failEntries--
			return 0, j.boom
		}
	}
	j.data = writeInto(j.data, off, p)
	j.pending = append(j.pending, indexWrite{off: off, p: bytes.Clone(p)})
	return len(p), nil
}

func (j *journalIndex) Sync() error {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.rec.note("index.sync")
	if j.failSync != nil {
		return j.failSync
	}
	j.durable, j.pending = bytes.Clone(j.data), nil
	return nil
}

func (j *journalIndex) bytes() []byte {
	j.mu.Lock()
	defer j.mu.Unlock()
	return bytes.Clone(j.data)
}

// crashImages returns every file a power cut could leave: the durable image
// with any subset of the pending writes on top, in the order they were made.
func (j *journalIndex) crashImages() [][]byte {
	j.mu.Lock()
	defer j.mu.Unlock()
	var out [][]byte
	for mask := 0; mask < 1<<len(j.pending); mask++ {
		img := bytes.Clone(j.durable)
		for i, w := range j.pending {
			if mask&(1<<i) != 0 {
				img = writeInto(img, w.off, w.p)
			}
		}
		out = append(out, img)
	}
	return out
}

// twoPostThread is a log holding posts 1 and 2 and the index an actor left
// for it after a clean Close, which is durable.
func twoPostThread(t *testing.T) (*recorder, *fakeLog, []byte) {
	t.Helper()
	a, lg, ixf, rec := newThread(t, SyncGroup)
	for _, s := range []string{"one", "two"} {
		if _, err := a.Append(post(s)); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	return rec, lg, ixf.bytes()
}

// reopenActor starts an actor over the log and an index file holding idx,
// the way the thread cache does with a trusted index.
func reopenActor(t *testing.T, lg *fakeLog, ixf *journalIndex, policy SyncPolicy) *Actor {
	t.Helper()
	raw := lg.bytes()
	rec, err := Open(bytes.NewReader(raw), int64(len(raw)), ixf.bytes())
	if err != nil {
		t.Fatal(err)
	}
	if rec.Outcome != Trusted {
		t.Fatalf("the starting index opened %v, want trusted", rec.Outcome)
	}
	return NewActor(rec.Index, rec.LogLength, lg, ixf, policy)
}

var deleteOne = Append{Type: TypeDelete, Ordinal: 1, Author: 42, Body: &DeleteBody{}}

// hiddenWhateverTheIndex fails unless post 1 opens hidden from the log and
// idx, whether the open trusted idx or replayed past it.
func hiddenWhateverTheIndex(t *testing.T, lg *fakeLog, idx []byte, what string) {
	t.Helper()
	raw := lg.bytes()
	rec, err := Open(bytes.NewReader(raw), int64(len(raw)), idx)
	if err != nil {
		t.Fatalf("%s: Open: %v", what, err)
	}
	e, err := rec.Index.Entry(1)
	if err != nil {
		t.Fatalf("%s: %v", what, err)
	}
	if e.Flags.Visible() {
		t.Errorf("%s: opened %v with post 1 visible (flags %#x) after its Delete", what, rec.Outcome, e.Flags)
	}
}

// Criterion: no header reaches the index after an entry write fails, for the
// rest of the actor's life, Close included, and Close reports the failure.
func TestAFailedEntryWriteLeavesTheHeaderUnwritten(t *testing.T) {
	rec, lg, idx := twoPostThread(t)
	boom := errors.New("index volume full")
	ixf := newJournalIndex(rec, idx)
	a := reopenActor(t, lg, ixf, SyncGroup)
	start := len(rec.snapshot())

	ixf.mu.Lock()
	ixf.failEntries, ixf.boom = 1, boom
	ixf.mu.Unlock()
	if _, err := a.Append(deleteOne); err != nil {
		t.Fatalf("a Delete that reached the log must succeed: %v", err)
	}
	// The next entry writes succeed; the header must still not be written.
	if _, err := a.Append(post("three")); err != nil {
		t.Fatal(err)
	}
	if err := a.Close(); !errors.Is(err, boom) {
		t.Errorf("Close = %v, want the entry failure", err)
	}
	if ops := rec.snapshot()[start:]; indexOf(ops, "index.header") >= 0 {
		t.Errorf("a header was written after the entry write failed: %v", ops)
	}
	hiddenWhateverTheIndex(t, lg, ixf.bytes(), "after the failed entry write")
	raw := lg.bytes()
	if r, _ := Open(bytes.NewReader(raw), int64(len(raw)), ixf.bytes()); r.Outcome == Trusted {
		t.Error("the index is trusted although an entry it covers was never written")
	}
}

// Criterion: no header reaches the index after an index fsync fails.
func TestAFailedIndexSyncLeavesTheHeaderUnwritten(t *testing.T) {
	rec, lg, idx := twoPostThread(t)
	boom := errors.New("index fsync failed")
	ixf := newJournalIndex(rec, idx)
	ixf.failSync = boom
	a := reopenActor(t, lg, ixf, SyncGroup)
	start := len(rec.snapshot())

	if _, err := a.Append(deleteOne); err != nil {
		t.Fatalf("a Delete that reached the log must succeed: %v", err)
	}
	if _, err := a.Append(post("three")); err != nil {
		t.Fatal(err)
	}
	if err := a.Close(); !errors.Is(err, boom) {
		t.Errorf("Close = %v, want the sync failure", err)
	}
	ops := rec.snapshot()[start:]
	if s := indexOf(ops, "index.sync"); s < 0 {
		t.Fatalf("the Delete's batch never synced the index: %v", ops)
	}
	if indexOf(ops, "index.header") >= 0 {
		t.Errorf("a header was written after the index fsync failed: %v", ops)
	}
	for _, img := range ixf.crashImages() {
		hiddenWhateverTheIndex(t, lg, img, "a crash after the failed sync")
	}
}

// Criterion: a batch that changes an existing entry makes it durable before
// the header that covers it is written, and a batch of new posts adds no
// index fsync. Every file a power cut could then leave opens with the
// deleted post hidden.
func TestAMutationIsSyncedToTheIndexBeforeTheHeaderCoversIt(t *testing.T) {
	for _, policy := range []SyncPolicy{SyncGroup, SyncAlways} {
		t.Run(policy.String(), func(t *testing.T) {
			rec, lg, idx := twoPostThread(t)
			ixf := newJournalIndex(rec, idx)
			a := reopenActor(t, lg, ixf, policy)

			start := len(rec.snapshot())
			if _, err := a.Append(post("three")); err != nil {
				t.Fatal(err)
			}
			if n := countIn(rec.snapshot()[start:], "index.sync"); n != 0 {
				t.Errorf("a batch of new posts synced the index %d times, want 0", n)
			}

			start = len(rec.snapshot())
			if _, err := a.Append(deleteOne); err != nil {
				t.Fatal(err)
			}
			ops := rec.snapshot()[start:]
			e, s, h := indexOf(ops, "index.entry"), indexOf(ops, "index.sync"), indexOf(ops, "index.header")
			if e < 0 || s < 0 || h < 0 || !(e < s && s < h) {
				t.Fatalf("the Delete's batch did %v; want its entry, then an index fsync, then the header", ops)
			}

			// The power cut lands now, with the actor still running: any
			// subset of the writes since the last fsync may have survived.
			for i, img := range ixf.crashImages() {
				hiddenWhateverTheIndex(t, lg, img, "crash image "+itoa(i))
			}
			if err := a.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
	t.Run("never", func(t *testing.T) {
		rec, lg, idx := twoPostThread(t)
		ixf := newJournalIndex(rec, idx)
		a := reopenActor(t, lg, ixf, SyncNever)
		start := len(rec.snapshot())
		if _, err := a.Append(deleteOne); err != nil {
			t.Fatal(err)
		}
		if err := a.Close(); err != nil {
			t.Fatal(err)
		}
		if n := countIn(rec.snapshot()[start:], "index.sync"); n != 0 {
			t.Errorf("SyncNever synced the index %d times, want 0", n)
		}
	})
}

func countIn(ops []string, want string) int {
	n := 0
	for _, op := range ops {
		if op == want {
			n++
		}
	}
	return n
}
