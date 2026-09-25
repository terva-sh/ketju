package threads

import (
	"testing"

	"github.com/terva-sh/ketju/internal/store/db/views"
)

// TestCursorRoundTrip: a cursor decodes, for its own sort only, to the key it
// was made from; the documented example (internal/dto's threads golden) is
// what encodeCursor writes.
func TestCursorRoundTrip(t *testing.T) {
	if got := encodeCursor(views.ByActivity, views.Key{At: 1757400000000, ID: 2}); got != "YXwxNzU3NDAwMDAwMDAwfDI" {
		t.Errorf("encoded %q", got)
	}
	for _, s := range []views.Sort{views.ByActivity, views.ByCreated} {
		for _, k := range []views.Key{{At: 0, ID: 1}, {At: -5, ID: 7}, {At: 1757400000000, ID: 1 << 62}} {
			c := encodeCursor(s, k)
			got, err := decodeCursor(s, c)
			if err != nil || got == nil || *got != k {
				t.Errorf("sort %d key %+v: %q decodes to %+v, %v", s, k, c, got, err)
			}
			other := views.ByCreated
			if s == views.ByCreated {
				other = views.ByActivity
			}
			if _, err := decodeCursor(other, c); err == nil {
				t.Errorf("sort %d key %+v: accepted by the other sort", s, k)
			}
		}
	}
	if k, err := decodeCursor(views.ByActivity, ""); k != nil || err != nil {
		t.Errorf("empty: %+v %v", k, err)
	}
}
