package lens_test

import (
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/service/threads/lens"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// TestParseLensTotal is the decided grammar (docs/07 "What the thread
// endpoint does", 2026-10-01) and its malformed neighbours, each of which is
// the canonical page.
func TestParseLensTotal(t *testing.T) {
	// 2026-09-01T00:00:00Z.
	const sept1 = 1_788_220_800_000
	for _, c := range []struct {
		raw  string
		want lens.Spec
	}{
		{"unread", lens.Spec{Kind: lens.Unread, Bare: true}},
		{"unread:60", lens.Spec{Kind: lens.Unread, Ordinal: 60}},
		{"unread:0", lens.Spec{Kind: lens.Unread}},
		{"author:drew", lens.Spec{Kind: lens.Author, Handle: "drew"}},
		{"author:Drew", lens.Spec{Kind: lens.Author, Handle: "drew"}},
		{"author:deleted-user-42", lens.Spec{Kind: lens.Author, Handle: "deleted-user-42"}},
		{"chain:42", lens.Spec{Kind: lens.Chain, Ordinal: 42}},
		{"since:2026-09-01", lens.Spec{Kind: lens.Since, SinceMS: sept1}},
		{"since:2026-09-01T02:00:00+02:00", lens.Spec{Kind: lens.Since, SinceMS: sept1}},
		{"since:2026-09-01T00:00:00.250Z", lens.Spec{Kind: lens.Since, SinceMS: sept1 + 250}},
		{"since:1788220800000", lens.Spec{Kind: lens.Since, SinceMS: sept1}},
		{"since:0", lens.Spec{Kind: lens.Since}},
		{"since:1969-12-31", lens.Spec{Kind: lens.Since}},
		{"mentions", lens.Spec{Kind: lens.Mentions}},
		{"mod", lens.Spec{Kind: lens.Mod}},

		{"", lens.Spec{}},
		{"nonsense", lens.Spec{}},
		{"Unread", lens.Spec{}},
		{"unread:", lens.Spec{}},
		{"unread:-1", lens.Spec{}},
		{"unread:+1", lens.Spec{}},
		{"unread:4294967296", lens.Spec{}},
		{"unread:1:2", lens.Spec{}},
		{"chain:", lens.Spec{}},
		{"chain:0", lens.Spec{}},
		{"chain:abc", lens.Spec{}},
		{"chain:1.5", lens.Spec{}},
		{"chain", lens.Spec{}},
		{"author", lens.Spec{}},
		{"author:", lens.Spec{}},
		{"author:a", lens.Spec{}},
		{"author:-drew", lens.Spec{}},
		{"author:drew-", lens.Spec{}},
		{"author:dr ew", lens.Spec{}},
		{"author:dréw", lens.Spec{}},
		{"author:Kate", lens.Spec{}}, // the Kelvin sign, not a K
		{"since", lens.Spec{}},
		{"since:", lens.Spec{}},
		{"since:2026-13-01", lens.Spec{}},
		{"since:2026-9-1", lens.Spec{}},
		{"since:yesterday", lens.Spec{}},
		{"since:-5", lens.Spec{}},
		{"since:99999999999999999999", lens.Spec{}},
		{"mentions:", lens.Spec{}},
		{"mentions:me", lens.Spec{}},
		{"mod:all", lens.Spec{}},
		{"digest", lens.Spec{}},
		{":", lens.Spec{}},
	} {
		if got := lens.Parse(c.raw); got != c.want {
			t.Errorf("Parse(%q) = %+v, want %+v", c.raw, got, c.want)
		}
	}
}

// TestSpecString: the form an answer names opens the same lens.
func TestSpecString(t *testing.T) {
	for raw, want := range map[string]string{
		"unread":                     "unread",
		"unread:7":                   "unread:7",
		"unread:007":                 "unread:7",
		"author:Drew":                "author:drew",
		"chain:42":                   "chain:42",
		"since:2026-09-01":           "since:1788220800000",
		"mentions":                   "mentions",
		"mod":                        "mod",
		"nonsense":                   "",
		"since:2026-09-01T00:00:00Z": "since:1788220800000",
	} {
		if got := lens.Parse(raw).String(); got != want {
			t.Errorf("Parse(%q).String() = %q, want %q", raw, got, want)
		}
	}
}

// FuzzParseLens: Parse never panics, and what it returns round-trips through
// String.
func FuzzParseLens(f *testing.F) {
	for _, s := range []string{"", "unread", "unread:3", "author:drew", "chain:42", "since:2026-09-01",
		"since:2026-09-01T10:00:00+05:30", "since:1788220800000", "mentions", "mod", "chain:", "author:Drew",
		"since:0001-01-01", "since:9999-12-31T23:59:59Z", "author:deleted-user-9"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		s := lens.Parse(raw)
		if again := lens.Parse(s.String()); again != s {
			t.Fatalf("Parse(%q) = %+v, whose String %q parses to %+v", raw, s, s.String(), again)
		}
		if s.Kind == lens.Canonical && s != (lens.Spec{}) {
			t.Fatalf("Parse(%q) = %+v: a canonical spec with fields set", raw, s)
		}
		if s.SinceMS < 0 {
			t.Fatalf("Parse(%q) = %+v: a negative moment", raw, s)
		}
	})
}

// index is an in-memory index of n posts, alice's (1) and bob's (2)
// alternating.
func index(n int) *threadlog.Index {
	entries := make([]threadlog.IndexEntry, n)
	for i := range entries {
		entries[i] = threadlog.IndexEntry{Offset: uint64(64 + i), Author: uint64(i%2 + 1), Revision: 1}
	}
	return threadlog.NewIndex(threadlog.IndexHeader{ThreadID: 1, PostCount: uint32(n)}, entries)
}

func notFound(err error) bool {
	return err != nil && problem.FromError(err) == problem.CodeNotFound
}

// TestSelectionPaging covers the page size, the empty selection's one empty
// page, a page past the end, and a permalink to a kept ordinal, a skipped
// one, and one past the last kept.
func TestSelectionPaging(t *testing.T) {
	sel := lens.Apply(index(25), lens.Odd) // 1, 3, ..., 25: thirteen posts
	if sel.Len() != 13 || sel.Hidden() != 12 || sel.Posts() != 25 {
		t.Fatalf("len %d hidden %d posts %d", sel.Len(), sel.Hidden(), sel.Posts())
	}
	if got := sel.TotalPages(5); got != 3 {
		t.Fatalf("total pages %d at 5 a page, want 3", got)
	}
	for n, want := range map[int][]uint32{1: {1, 3, 5, 7, 9}, 2: {11, 13, 15, 17, 19}, 3: {21, 23, 25}} {
		got, err := sel.Page(n, 5)
		if err != nil || !slices.Equal(got, want) {
			t.Errorf("page %d = %v, %v; want %v", n, got, err, want)
		}
	}
	for _, n := range []int{0, 4, 99} {
		if _, err := sel.Page(n, 5); !notFound(err) {
			t.Errorf("page %d: %v, want not-found", n, err)
		}
	}
	for o, want := range map[uint32]int{1: 1, 9: 1, 10: 2, 11: 2, 20: 3, 25: 3, 26: 3, 1000: 3} {
		if got := sel.PageOf(o, 5); got != want {
			t.Errorf("PageOf(%d) = %d, want %d", o, got, want)
		}
	}

	// A page handed out is a copy: changing it changes no other page.
	p, _ := sel.Page(1, 5)
	slices.Reverse(p)
	if again, _ := sel.Page(1, 5); !slices.Equal(again, []uint32{1, 3, 5, 7, 9}) {
		t.Fatalf("reversing a page reordered the selection: %v", again)
	}

	empty := lens.Apply(index(4), lens.Ordinals())
	if empty.TotalPages(25) != 1 || empty.Len() != 0 || empty.Hidden() != 4 {
		t.Fatalf("empty: %d pages, len %d, hidden %d", empty.TotalPages(25), empty.Len(), empty.Hidden())
	}
	if got, err := empty.Page(1, 25); err != nil || len(got) != 0 {
		t.Fatalf("the empty selection's page 1: %v, %v", got, err)
	}
	if _, err := empty.Page(2, 25); !notFound(err) {
		t.Fatalf("the empty selection's page 2: %v", err)
	}
	if got := empty.PageOf(3, 25); got != 1 {
		t.Fatalf("PageOf on the empty selection = %d", got)
	}
}

// TestOrdinalsFilter: the order the ordinals are given in changes nothing,
// and an ordinal the thread does not have is not selected.
func TestOrdinalsFilter(t *testing.T) {
	sel := lens.Apply(index(10), lens.Ordinals(9, 2, 7, 2, 40, 0))
	if got, _ := sel.Page(1, 25); !slices.Equal(got, []uint32{2, 7, 9}) {
		t.Fatalf("Ordinals(9, 2, 7, 2, 40, 0) selected %v", got)
	}
}

// TestSelectionPagesStableUnderAppend: a post appended to the thread leaves
// every earlier lens page's ordinals as they were, whether the lens keeps the
// new post or not.
func TestSelectionPagesStableUnderAppend(t *testing.T) {
	r := rand.New(rand.NewPCG(7, 8))
	for range 50 {
		n, size := r.IntN(60)+1, r.IntN(9)+1
		coin := make([]bool, n+2)
		var keep []uint32
		for i := 1; i < len(coin); i++ {
			if coin[i] = r.IntN(3) > 0; coin[i] {
				keep = append(keep, uint32(i))
			}
		}
		f := lens.Ordinals(keep...)
		before := lens.Apply(index(n), f)
		after := lens.Apply(index(n+1), f)
		if after.Len() < before.Len() || after.Hidden() < before.Hidden() {
			t.Fatalf("an append shrank the lens: %d/%d then %d/%d", before.Len(), before.Hidden(), after.Len(), after.Hidden())
		}
		// Every full page before the append is unchanged, and the last,
		// possibly partial, page only grows at its end.
		for p := 1; p <= before.TotalPages(size); p++ {
			was, _ := before.Page(p, size)
			now, err := after.Page(p, size)
			if err != nil {
				t.Fatal(err)
			}
			if len(was) == size && !slices.Equal(was, now) || !slices.Equal(was, now[:len(was)]) {
				t.Fatalf("page %d at %d a page was %v, after an append %v", p, size, was, now)
			}
		}
	}
}

// TestSelectionGaps covers a leading gap, an inner one, one that crosses a
// page boundary, the thread's tail, and the empty selection.
func TestSelectionGaps(t *testing.T) {
	// A thread of 20; the lens keeps 3, 4, 8, 9, 10, 15.
	sel := lens.Apply(index(20), lens.Ordinals(3, 4, 8, 9, 10, 15))
	g := func(pairs ...int) []dto.LensGap {
		out := []dto.LensGap{}
		for i := 0; i+1 < len(pairs); i += 2 {
			out = append(out, dto.LensGap{After: pairs[i], Count: pairs[i+1]})
		}
		return out
	}
	for _, c := range []struct {
		page, size int
		want       []dto.LensGap
	}{
		{1, 25, g(0, 2, 4, 3, 10, 4, 15, 5)},
		{1, 3, g(0, 2, 4, 3)},   // 3, 4, 8: leading and inner
		{2, 3, g(10, 4, 15, 5)}, // 9, 10, 15: 9 follows 8, so no gap before it
		{1, 2, g(0, 2, 4, 3)},   // 3, 4: the gap to page 2's 8 is shown here
		{2, 2, g(4, 3)},         // 8, 9: and here
		{3, 2, g(10, 4, 15, 5)},
		{4, 2, g()}, // past the last page
	} {
		if got := sel.Gaps(c.page, c.size); !slices.Equal(got, c.want) {
			t.Errorf("page %d at %d a page: gaps %v, want %v", c.page, c.size, got, c.want)
		}
	}

	if got := lens.Apply(index(6), lens.Ordinals()).Gaps(1, 25); !slices.Equal(got, g(0, 6)) {
		t.Errorf("the empty selection's gaps: %v", got)
	}
	if got := lens.Apply(index(0), lens.Ordinals()).Gaps(1, 25); got == nil || len(got) != 0 {
		t.Errorf("an empty thread's gaps: %#v, want []", got)
	}
	if got := lens.Apply(index(5), lens.Ordinals(1, 2, 3, 4, 5)).Gaps(1, 25); got == nil || len(got) != 0 {
		t.Errorf("a lens that kept everything: gaps %#v, want []", got)
	}
}

// TestSelectionAgreesWithItsGaps: on random selections, a page's posts and
// its gaps account for exactly the run of the thread the page spans, and
// shown plus hidden is the thread.
func TestSelectionAgreesWithItsGaps(t *testing.T) {
	r := rand.New(rand.NewPCG(9, 10))
	for range 200 {
		n, size := r.IntN(50), r.IntN(7)+1
		var keep []uint32
		for i := 1; i <= n; i++ {
			if r.IntN(2) == 0 {
				keep = append(keep, uint32(i))
			}
		}
		sel := lens.Apply(index(n), lens.Ordinals(keep...))
		if sel.Len()+sel.Hidden() != n {
			t.Fatalf("shown %d + hidden %d != %d", sel.Len(), sel.Hidden(), n)
		}
		skipped := 0
		for p := 1; p <= sel.TotalPages(size); p++ {
			posts, _ := sel.Page(p, size)
			for _, gap := range sel.Gaps(p, size) {
				for o := gap.After + 1; o <= gap.After+gap.Count; o++ {
					if slices.Contains(keep, uint32(o)) {
						t.Fatalf("gap %+v covers kept #%d", gap, o)
					}
				}
				if p == 1 || len(posts) == 0 || gap.After >= int(posts[0]) {
					skipped += gap.Count
				}
			}
		}
		if skipped != sel.Hidden() {
			t.Fatalf("keep %v of %d at %d a page: the gaps account for %d skipped posts, hidden is %d",
				keep, n, size, skipped, sel.Hidden())
		}
	}
}
