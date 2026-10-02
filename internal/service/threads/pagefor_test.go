package threads

import (
	"testing"

	"github.com/terva-sh/ketju/internal/problem"
)

// TestPageForPastTheLastOrdinal: a permalink past the thread's last post is
// not-found, the largest ordinal included. ?ordinal= reaches PageFor from a
// URL, and on a 32-bit build int(4294967295) is -1, which passed the bound
// and served page 0 (TKT-01M3VVZE4G). CI runs this test under GOARCH=386.
func TestPageForPastTheLastOrdinal(t *testing.T) {
	for _, o := range []uint32{4, 1 << 31, 1<<32 - 1} {
		page, err := PageFor(Query{Ordinal: o}, 3, 25, false, 0)
		if problem.FromError(err) != problem.CodeNotFound {
			t.Errorf("ordinal %d of 3 posts: page %d, %v; want not-found", o, page, err)
		}
	}
	if page, err := PageFor(Query{Ordinal: 3}, 3, 25, false, 0); err != nil || page != 1 {
		t.Errorf("ordinal 3 of 3 posts: page %d, %v; want page 1", page, err)
	}
}
