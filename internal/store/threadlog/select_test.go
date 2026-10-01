package threadlog

import (
	"math/rand/v2"
	"slices"
	"testing"
)

// randomIndex is an index of n posts with authors, times and flags drawn from
// r, so a filter over it has something to choose between.
func randomIndex(r *rand.Rand, n int) *Index {
	entries := make([]IndexEntry, n)
	for i := range entries {
		entries[i] = IndexEntry{
			Offset:    uint64(64 + i*100),
			CreatedMS: 1_700_000_000_000 + int64(r.IntN(1000)),
			Author:    uint64(r.IntN(5) + 1),
			ReplyTo:   uint32(r.IntN(i + 1)),
			Revision:  1,
			Flags:     EntryFlags(r.IntN(256)) & EntryFlagsKnown,
		}
	}
	return NewIndex(IndexHeader{ThreadID: 1, PostCount: uint32(n)}, entries)
}

// TestIndexSelectAscending is the lens framework's ordering promise
// (TKT-01M29CRJ1P): whatever keep answers, Select returns strictly ascending
// ordinals, each of which keep accepted, and every accepted one, which is
// the same answer as a filter written by hand over the entries.
func TestIndexSelectAscending(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	for round := range 300 {
		n := r.IntN(120)
		ix := randomIndex(r, n)
		// A keep that answers from the ordinal and the entry both, and one
		// that answers at random, so the property does not lean on a filter
		// with a neat shape.
		mod := uint32(r.IntN(4) + 1)
		author := uint64(r.IntN(5) + 1)
		coin := make([]bool, n+1)
		for i := range coin {
			coin[i] = r.IntN(2) == 0
		}
		keeps := map[string]func(uint32, IndexEntry) bool{
			"ordinal or author": func(o uint32, e IndexEntry) bool { return o%mod == 0 || e.Author == author },
			"visible":           func(_ uint32, e IndexEntry) bool { return e.Flags.Visible() },
			"random":            func(o uint32, _ IndexEntry) bool { return coin[o] },
			"none":              func(uint32, IndexEntry) bool { return false },
			"all":               func(uint32, IndexEntry) bool { return true },
		}
		for name, keep := range keeps {
			got := ix.Select(keep)
			var want []uint32
			for i := 1; i <= n; i++ {
				e, err := ix.Entry(uint32(i))
				if err != nil {
					t.Fatal(err)
				}
				if keep(uint32(i), e) {
					want = append(want, uint32(i))
				}
			}
			if !slices.Equal(got, want) {
				t.Fatalf("round %d, %s over %d posts: Select = %v, by hand %v", round, name, n, got, want)
			}
			for i := 1; i < len(got); i++ {
				if got[i] <= got[i-1] {
					t.Fatalf("round %d, %s: %v is not strictly ascending at %d", round, name, got, i)
				}
			}
		}
	}
}

// TestSelectWalksOnceInOrder: keep sees every post exactly once, lowest
// ordinal first, so a filter cannot be handed the posts in another order.
func TestSelectWalksOnceInOrder(t *testing.T) {
	ix := randomIndex(rand.New(rand.NewPCG(3, 4)), 50)
	var seen []uint32
	ix.Select(func(o uint32, _ IndexEntry) bool {
		seen = append(seen, o)
		return false
	})
	if len(seen) != 50 {
		t.Fatalf("keep saw %d posts of 50", len(seen))
	}
	for i, o := range seen {
		if o != uint32(i)+1 {
			t.Fatalf("keep saw %v: not each ordinal once, ascending", seen)
		}
	}
}

// TestByAuthorAndSinceUnchanged: the two lenses docs/03 measured keep
// their answers now that they are Select underneath.
func TestByAuthorAndSinceUnchanged(t *testing.T) {
	r := rand.New(rand.NewPCG(5, 6))
	for range 100 {
		ix := randomIndex(r, r.IntN(80))
		author := uint64(r.IntN(5) + 1)
		ms := 1_700_000_000_000 + int64(r.IntN(1000))
		var byAuthor, since []uint32
		for i, e := range ix.entries {
			if e.Author == author {
				byAuthor = append(byAuthor, uint32(i)+1)
			}
			if e.CreatedMS >= ms {
				since = append(since, uint32(i)+1)
			}
		}
		if got := ix.ByAuthor(author); !slices.Equal(got, byAuthor) {
			t.Fatalf("ByAuthor(%d) = %v, want %v", author, got, byAuthor)
		}
		if got := ix.Since(ms); !slices.Equal(got, since) {
			t.Fatalf("Since(%d) = %v, want %v", ms, got, since)
		}
	}
}
