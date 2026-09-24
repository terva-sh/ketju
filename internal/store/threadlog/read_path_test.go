package threadlog

import (
	"bytes"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
)

// TestLogLengthNeverOutrunsTheIndex: a reader that takes LogLength and then
// the index sees every post the length covers. Posts are appended from many
// goroutines while a reader checks, under the race detector, that the last
// indexed post ends at or beyond the length it read first.
func TestLogLengthNeverOutrunsTheIndex(t *testing.T) {
	a, _, _, _ := newThread(t, SyncNever)
	t.Cleanup(func() { a.Close() })
	start := a.LogLength()
	if start != FileHeaderSize {
		t.Fatalf("a fresh thread reports length %d, want %d", start, FileHeaderSize)
	}

	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Go(func() {
		for {
			select {
			case <-stop:
				return
			default:
			}
			l := a.LogLength()
			ix := a.Index()
			end := int64(FileHeaderSize)
			if n := ix.Len(); n > 0 {
				e, err := ix.Entry(uint32(n))
				if err != nil {
					t.Error(err)
					return
				}
				end = int64(e.Offset) + int64(e.FrameLength)
			}
			if end < l {
				t.Errorf("LogLength said %d while the index ended at %d", l, end)
				return
			}
		}
	})
	var writers sync.WaitGroup
	for range 8 {
		writers.Go(func() {
			for range 50 {
				res, err := a.Append(post("x"))
				if err != nil {
					t.Error(err)
					return
				}
				if got := a.LogLength(); got < res.LogLength {
					t.Errorf("after an append returned %d, LogLength is %d", res.LogLength, got)
				}
			}
		})
	}
	writers.Wait()
	close(stop)
	wg.Wait()
}

// TestHiddenReadsATombstone: Range returns a deleted post without its source,
// and Hidden, the moderator's read, returns the source anyway.
func TestHiddenReadsATombstone(t *testing.T) {
	lg, ix := threadWith(t, 3, Append{Type: TypeDelete, Author: 1, Ordinal: 2, Body: &DeleteBody{}})
	r := NewReader(bytes.NewReader(lg.bytes()), ix)
	got, err := r.Range(2, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !got[0].Tombstone || got[0].Source != "" {
		t.Fatalf("Range returned %+v for a deleted post", got[0])
	}
	h, err := r.Hidden(2)
	if err != nil {
		t.Fatal(err)
	}
	if h.Source != "body of post 2" || h.Ordinal != 2 {
		t.Fatalf("Hidden returned %+v", h)
	}
	if _, err := r.Hidden(9); err == nil {
		t.Fatal("Hidden of an ordinal past the end succeeded")
	}
}

// TestRevisionMSIsTheEditTime: an edited post reports the edit's timestamp as
// RevisionMS and keeps revision 1's as CreatedMS.
func TestRevisionMSIsTheEditTime(t *testing.T) {
	const edited = 1_800_000_000_000
	lg, ix := threadWith(t, 2, Append{Type: TypeEdit, Author: 100, Ordinal: 1, TimeMS: edited,
		Body: &EditBody{Source: "fixed"}})
	posts, err := NewReader(bytes.NewReader(lg.bytes()), ix).Range(1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if posts[0].RevisionMS != edited || posts[0].CreatedMS == edited || posts[0].Source != "fixed" {
		t.Fatalf("edited post: %+v", posts[0])
	}
	if posts[1].RevisionMS != posts[1].CreatedMS {
		t.Fatalf("unedited post: RevisionMS %d, CreatedMS %d", posts[1].RevisionMS, posts[1].CreatedMS)
	}
}

// TestReplyCounts counts direct replies for the asked ordinals only.
func TestReplyCounts(t *testing.T) {
	reply := func(to uint32) Append {
		return Append{Type: TypePost, Author: 1, Body: &PostBody{Source: "r", ReplyTo: to}}
	}
	_, ix := threadWith(t, 2, reply(1), reply(1), reply(2), reply(3))
	got := ix.ReplyCounts([]uint32{1, 2, 4, 5})
	want := map[uint32]int{1: 2, 2: 1, 4: 0, 5: 0}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("ordinal %d: %d replies, want %d", k, got[k], v)
		}
	}
}

// TestStableSeesExactlyOneLength: inside Stable, the length and the index are
// the same log, so with only posts appended the last indexed post ends exactly
// at the length: never short of it, and never past it, which is what a bare
// LogLength-then-index read allows while a batch is being applied.
func TestStableSeesExactlyOneLength(t *testing.T) {
	a, _, _, _ := newThread(t, SyncNever)
	t.Cleanup(func() { a.Close() })

	var wg sync.WaitGroup
	stop := make(chan struct{})
	var checks atomic.Int64
	// Several readers, because the window this closes is the few
	// microseconds a batch spends in Index.Apply.
	for range 4 {
		wg.Go(func() { stableReader(t, a, stop, &checks) })
	}
	var writers sync.WaitGroup
	for range 16 {
		writers.Go(func() {
			for range 250 {
				if _, err := a.Append(post("x")); err != nil {
					t.Error(err)
					return
				}
			}
		})
	}
	writers.Wait()
	close(stop)
	wg.Wait()
	if checks.Load() == 0 {
		t.Fatal("no reader ever ran")
	}
}

func stableReader(t *testing.T, a *Actor, stop chan struct{}, checks *atomic.Int64) {
	for {
		select {
		case <-stop:
			return
		default:
		}
		var l, end int64
		a.Stable(func(logLength int64) {
			l, end = logLength, int64(FileHeaderSize)
			if n := a.Index().Len(); n > 0 {
				e, err := a.Index().Entry(uint32(n))
				if err != nil {
					t.Error(err)
					return
				}
				end = int64(e.Offset) + int64(e.FrameLength)
			}
		})
		if end != l {
			t.Errorf("Stable gave length %d with an index ending at %d", l, end)
			return
		}
		checks.Add(1)
	}
}

// TestReadEntriesReadsTheSnapshot: entries taken before an edit still read
// the revision they name, so a page built from a snapshot shows the posts as
// of that snapshot.
func TestReadEntriesReadsTheSnapshot(t *testing.T) {
	// threadWith is deterministic, so the same two posts without the edit
	// give the entries a reader held before it.
	_, ix := threadWith(t, 2)
	before := ix.Entries(1, 2)
	lg, edited := threadWith(t, 2, Append{Type: TypeEdit, Author: 1, Ordinal: 2, Body: &EditBody{Source: "edited"}})
	r := NewReader(bytes.NewReader(lg.bytes()), edited)
	old, err := r.ReadEntries(1, before)
	if err != nil {
		t.Fatal(err)
	}
	now, err := r.Range(1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if old[1].Revision != 1 || old[1].Source == "edited" || now[1].Revision != 2 || now[1].Source != "edited" {
		t.Fatalf("through the snapshot: %+v; live: %+v", old[1], now[1])
	}
}

// TestHistoryIsBoundedAndOrdered: History returns a post's revisions in log
// order with actor, time, reason and source, only up to the length asked for,
// and refuses an ordinal the index does not hold.
func TestHistoryIsBoundedAndOrdered(t *testing.T) {
	lg, ix := threadWith(t, 3,
		Append{Type: TypeEdit, Author: 7, Ordinal: 2, TimeMS: 1_800_000_000_000, Body: &EditBody{Source: "two, again", Reason: "clarity"}},
		Append{Type: TypeEdit, Author: 8, Ordinal: 1, TimeMS: 1_800_000_000_001, Body: &EditBody{Source: "one, again"}},
		Append{Type: TypeEdit, Author: 9, Ordinal: 2, TimeMS: 1_800_000_000_002, Body: &EditBody{Source: "two, third"}},
	)
	r := NewReader(bytes.NewReader(lg.bytes()), ix)
	all, err := r.History(2, int64(len(lg.bytes())))
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 || all[0].Source != "body of post 2" || all[0].Revision != 1 ||
		all[1].Actor != 7 || all[1].Reason != "clarity" || all[1].TimeMS != 1_800_000_000_000 ||
		all[2].Source != "two, third" || all[2].Revision != 3 {
		t.Fatalf("history of 2: %+v", all)
	}
	// Bounded just before the last edit: two revisions.
	e, err := ix.Entry(2)
	if err != nil {
		t.Fatal(err)
	}
	if early, err := r.History(2, int64(e.Offset)); err != nil || len(early) != 2 {
		t.Fatalf("history of 2 up to %d: %+v, %v", e.Offset, early, err)
	}
	if _, err := r.History(9, int64(len(lg.bytes()))); !errors.Is(err, ErrNoSuchOrdinal) {
		t.Fatalf("history of a post past the end: %v", err)
	}
}
