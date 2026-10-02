package threadlog

import (
	"bytes"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
)

// threadWith builds a log of n posts through the actor, so the reader is tested
// against bytes the writer actually produced rather than against a fixture that
// agrees with it by construction.
func threadWith(t *testing.T, n int, extra ...Append) (*fakeLog, *Index) {
	t.Helper()
	rec := &recorder{}
	lg := &fakeLog{rec: rec, data: NewFileHeader(1, 1_700_000_000_000, 7).AppendTo(nil)}
	ix := &Index{Header: IndexHeader{ThreadID: 1}}
	a := NewActor(ix, int64(len(lg.data)), lg, &fakeIndex{rec: rec}, SyncNever)

	for i := range n {
		if _, err := a.Append(Append{
			Type: TypePost, Author: uint64(100 + i%3),
			Body: &PostBody{Source: fmt.Sprintf("body of post %d", i+1)},
		}); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}
	for _, e := range extra {
		if _, err := a.Append(e); err != nil {
			t.Fatalf("extra append: %v", err)
		}
	}
	if err := a.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	return lg, ix
}

// countingAt wraps a byte reader and counts ReadAt calls and bytes.
type countingAt struct {
	r     *bytes.Reader
	calls atomic.Int64
	bytes atomic.Int64
}

func (c *countingAt) ReadAt(p []byte, off int64) (int, error) {
	c.calls.Add(1)
	c.bytes.Add(int64(len(p)))
	return c.r.ReadAt(p, off)
}

// Criterion: a page of contiguous posts is fetched with one ReadAt, counted in
// a test.
func TestContiguousPageCostsOneReadAt(t *testing.T) {
	const posts = 25
	lg, ix := threadWith(t, posts)

	c := &countingAt{r: bytes.NewReader(lg.bytes())}
	got, err := NewReader(c, ix).Range(1, posts)
	if err != nil {
		t.Fatalf("Range: %v", err)
	}

	if len(got) != posts {
		t.Fatalf("read %d posts, want %d", len(got), posts)
	}
	if n := c.calls.Load(); n != 1 {
		t.Errorf("a page of %d contiguous posts cost %d ReadAt calls, want exactly 1", posts, n)
	}
	for i, p := range got {
		want := fmt.Sprintf("body of post %d", i+1)
		if p.Source != want {
			t.Errorf("post %d source = %q, want %q", i+1, p.Source, want)
		}
		if p.Ordinal != uint32(i+1) {
			t.Errorf("post at position %d has ordinal %d", i, p.Ordinal)
		}
	}
}

// An edited post's current revision lives where the Edit was appended, so it
// breaks the contiguous run. That is correct rather than a shortcoming, and the
// runs either side of it must still coalesce.
func TestAnEditBreaksTheRunButTheRestStillCoalesces(t *testing.T) {
	lg, ix := threadWith(t, 10, Append{
		Type: TypeEdit, Ordinal: 5, Author: 1,
		Body: &EditBody{Source: "post 5, corrected"},
	})

	c := &countingAt{r: bytes.NewReader(lg.bytes())}
	got, err := NewReader(c, ix).Range(1, 10)
	if err != nil {
		t.Fatalf("Range: %v", err)
	}

	// Runs: 1-4, then 5 alone, then 6-10. Three reads, not ten.
	if n := c.calls.Load(); n != 3 {
		t.Errorf("ReadAt calls = %d, want 3 (before the edit, the edit, after it)", n)
	}
	if got[4].Source != "post 5, corrected" {
		t.Errorf("post 5 source = %q, want the edited text", got[4].Source)
	}
	if got[4].Revision != 2 {
		t.Errorf("post 5 revision = %d, want 2", got[4].Revision)
	}
	if !got[4].Flags.Has(EntryEdited) {
		t.Error("post 5 should carry the edited flag")
	}
	if got[3].Source != "body of post 4" || got[5].Source != "body of post 6" {
		t.Error("the posts either side of the edit did not survive the split runs")
	}
}

// Criterion: tombstoned and moved posts return from the index without a log
// read.
func TestTombstonedPostsCostNoLogRead(t *testing.T) {
	lg, ix := threadWith(t, 6,
		Append{Type: TypeDelete, Ordinal: 2, Author: 9, Body: &DeleteBody{Reason: "spam"}},
		Append{Type: TypeDelete, Ordinal: 3, Author: 9, Body: &DeleteBody{Reason: "spam"}},
	)

	// Read only the deleted pair: it must cost nothing at all.
	c := &countingAt{r: bytes.NewReader(lg.bytes())}
	got, err := NewReader(c, ix).Range(2, 3)
	if err != nil {
		t.Fatalf("Range: %v", err)
	}
	if n := c.calls.Load(); n != 0 {
		t.Errorf("reading two tombstoned posts cost %d reads (%d bytes), want none",
			n, c.bytes.Load())
	}
	for _, p := range got {
		if !p.Tombstone {
			t.Errorf("ordinal %d should be a tombstone", p.Ordinal)
		}
		if p.Source != "" {
			t.Errorf("ordinal %d returned body text for a deleted post: %q", p.Ordinal, p.Source)
		}
	}

	// A page spanning them reads the survivors only, in two runs.
	c2 := &countingAt{r: bytes.NewReader(lg.bytes())}
	page, err := NewReader(c2, ix).Range(1, 6)
	if err != nil {
		t.Fatalf("Range: %v", err)
	}
	if len(page) != 6 {
		t.Fatalf("page has %d posts, want 6", len(page))
	}
	if n := c2.calls.Load(); n != 2 {
		t.Errorf("ReadAt calls = %d, want 2 (post 1, then posts 4-6)", n)
	}
	if !page[1].Tombstone || !page[2].Tombstone {
		t.Error("posts 2 and 3 should be tombstones")
	}
	if page[0].Source != "body of post 1" || page[5].Source != "body of post 6" {
		t.Error("the visible posts around the tombstones did not read back")
	}

	// A moved post is the same promise.
	e, _ := ix.Entry(4)
	e.Flags |= EntryMoved
	ix.mu.Lock()
	ix.entries[3] = e
	ix.mu.Unlock()

	c3 := &countingAt{r: bytes.NewReader(lg.bytes())}
	one, err := NewReader(c3, ix).One(4)
	if err != nil {
		t.Fatalf("One: %v", err)
	}
	if !one.Tombstone || c3.calls.Load() != 0 {
		t.Errorf("a moved post cost %d reads and Tombstone=%v, want 0 and true",
			c3.calls.Load(), one.Tombstone)
	}
}

// Criterion: a record failing its CRC returns an error naming the ordinal
// rather than returning bytes.
func TestCorruptRecordNamesTheOrdinal(t *testing.T) {
	lg, ix := threadWith(t, 5)

	e, err := ix.Entry(3)
	if err != nil {
		t.Fatal(err)
	}
	damaged := bytes.Clone(lg.bytes())
	damaged[int(e.Offset)+FrameFixedSize+2] ^= 0xff // inside post 3's body

	_, err = NewReader(bytes.NewReader(damaged), ix).Range(1, 5)
	if err == nil {
		t.Fatal("a corrupt record was read without complaint")
	}
	if !errors.Is(err, ErrChecksum) {
		t.Errorf("error = %v, want ErrChecksum", err)
	}

	var re *ReadError
	if !errors.As(err, &re) {
		t.Fatalf("error %v is not a *ReadError, so it does not name an ordinal", err)
	}
	if re.Ordinal != 3 {
		t.Errorf("ReadError names ordinal %d, want 3", re.Ordinal)
	}

	// Reading a range that excludes the damage still works: one bad record
	// must not make the whole thread unreadable.
	ok, err := NewReader(bytes.NewReader(damaged), ix).Range(4, 5)
	if err != nil {
		t.Errorf("an undamaged range failed: %v", err)
	}
	if len(ok) != 2 {
		t.Errorf("read %d posts from the clean range, want 2", len(ok))
	}
}

// Criterion: reads run concurrently with appends to the same thread under the
// race detector, with no reader ever blocking on the actor.
//
// The "never blocks" half is what the gate proves. The actor is held inside a
// blocked fsync for the whole test; if a reader needed anything from the actor
// it would sit there until the gate opened. Instead every read completes first.
func TestReadsRunWhileTheActorIsBlockedInFsync(t *testing.T) {
	rec := &recorder{}
	gate := make(chan struct{})
	lg := &fakeLog{rec: rec, data: NewFileHeader(1, 1, 1).AppendTo(nil)}
	ix := &Index{Header: IndexHeader{ThreadID: 1}}
	a := NewActor(ix, int64(len(lg.data)), lg, &fakeIndex{rec: rec}, SyncGroup)

	// Seed some posts to read, before the gate is armed.
	for i := range 8 {
		if _, err := a.Append(Append{Type: TypePost, Author: 1,
			Body: &PostBody{Source: fmt.Sprintf("seed %d", i)}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}

	// New actor over the same files, this time with a gated fsync.
	lg.gate = gate
	lg.gateOnce = sync.Once{}
	a2 := NewActor(ix, int64(len(lg.bytes())), lg, &fakeIndex{rec: rec}, SyncGroup)

	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		a2.Append(Append{Type: TypePost, Author: 1, Body: &PostBody{Source: "blocked"}}) //nolint:errcheck
	}()

	// The writer is now stuck inside Sync. Readers must not care.
	readsDone := make(chan error, 8)
	for range 8 {
		go func() {
			r := NewReader(bytes.NewReader(lg.bytes()), ix)
			for range 20 {
				got, err := r.Range(1, 8)
				if err != nil {
					readsDone <- err
					return
				}
				if len(got) != 8 {
					readsDone <- fmt.Errorf("read %d posts, want 8", len(got))
					return
				}
			}
			readsDone <- nil
		}()
	}
	for range 8 {
		if err := <-readsDone; err != nil {
			t.Fatalf("a read failed while the actor was blocked: %v", err)
		}
	}

	// Only now let the writer finish. If any reader had needed the actor, the
	// loop above would have deadlocked rather than reaching here.
	close(gate)
	<-writerDone
	if err := a2.Close(); err != nil {
		t.Fatal(err)
	}
}

// Reads and appends interleaved, for the race detector to chew on.
func TestConcurrentReadsAndAppends(t *testing.T) {
	rec := &recorder{}
	lg := &fakeLog{rec: rec, data: NewFileHeader(1, 1, 1).AppendTo(nil)}
	ix := &Index{Header: IndexHeader{ThreadID: 1}}
	a := NewActor(ix, int64(len(lg.data)), lg, &fakeIndex{rec: rec}, SyncNever)

	if _, err := a.Append(Append{Type: TypePost, Author: 1, Body: &PostBody{Source: "first"}}); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range 40 {
				a.Append(Append{Type: TypePost, Author: 2, //nolint:errcheck
					Body: &PostBody{Source: fmt.Sprintf("concurrent %d", i)}})
			}
		}()
	}
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 40 {
				// The index and the log both move under the reader. Reading
				// only what the index currently claims must always be safe.
				n := uint32(ix.Len())
				if n == 0 {
					continue
				}
				_ = ix.ByAuthor(2)
				_, _ = ix.Tombstoned(1)
				_ = ix.Entries(1, n)
			}
		}()
	}
	wg.Wait()
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}

	if ix.Len() != 161 {
		t.Errorf("index holds %d posts, want 161", ix.Len())
	}
	got, err := Open(bytes.NewReader(lg.bytes()), int64(len(lg.bytes())), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Index.Len() != ix.Len() {
		t.Errorf("log replays to %d posts, the live index says %d", got.Index.Len(), ix.Len())
	}
}

func TestRangeClampsAndOneRefusesAMissingOrdinal(t *testing.T) {
	lg, ix := threadWith(t, 3)
	r := NewReader(bytes.NewReader(lg.bytes()), ix)

	if got, _ := r.Range(2, 99); len(got) != 2 {
		t.Errorf("Range(2,99) returned %d posts, want 2", len(got))
	}
	if got, _ := r.Range(9, 12); got != nil {
		t.Errorf("Range past the end returned %+v, want nil", got)
	}
	if _, err := r.One(9); !errors.Is(err, ErrNoSuchOrdinal) {
		t.Errorf("One(9) error = %v, want ErrNoSuchOrdinal", err)
	}
}

// Criterion (TKT-01M3VRFRFW): an entry that points at another post's frame,
// which a filesystem showing stale blocks where a lost entry was could
// leave, is a read error naming the ordinal, never the other post's text.
func TestAnEntryPointingAtAnotherPostsFrameIsAReadError(t *testing.T) {
	lg, ix := threadWith(t, 3)
	e2, err := ix.Entry(2)
	if err != nil {
		t.Fatal(err)
	}
	entries := ix.Entries(1, 3)
	entries[2].Offset, entries[2].FrameLength = e2.Offset, e2.FrameLength
	wrong := NewIndex(ix.Header, entries)
	r := NewReader(bytes.NewReader(lg.bytes()), wrong)

	check := func(what string, posts []Post, err error) {
		t.Helper()
		var re *ReadError
		if !errors.As(err, &re) || re.Ordinal != 3 || !errors.Is(err, ErrMalformed) {
			t.Errorf("%s: %+v, %v; want a ReadError for ordinal 3 wrapping ErrMalformed", what, posts, err)
		}
	}
	posts, err := r.Range(1, 3)
	check("Range(1, 3)", posts, err)
	p, err := r.One(3)
	check("One(3)", []Post{p}, err)
	p, err = r.Hidden(3)
	check("Hidden(3)", []Post{p}, err)
	posts, err = r.ReadEntries(3, wrong.Entries(3, 3))
	check("ReadEntries(3, ...)", posts, err)

	// The posts the index describes rightly still read.
	if posts, err := r.Range(1, 2); err != nil || len(posts) != 2 {
		t.Errorf("Range(1, 2): %d posts, %v", len(posts), err)
	}
}
