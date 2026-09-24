package threadlog

import (
	"bytes"
	"sync"
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
