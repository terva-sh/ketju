package threads

import (
	"testing"

	"github.com/terva-sh/ketju/internal/store/db/views"
	"github.com/terva-sh/ketju/internal/store/threadid"
)

// fixtureCursor is internal/dto's threads golden's next_cursor: the page
// after its second row, in forum 1, whose first page pinned its first row.
func fixtureCursor(t *testing.T) cursor {
	t.Helper()
	first, err1 := threadid.Parse("3yymw47g00001")
	second, err2 := threadid.Parse("3yymw47g00002")
	if err1 != nil || err2 != nil {
		t.Fatal(err1, err2)
	}
	return cursor{Sort: views.ByActivity, Forum: 1, Key: views.Key{At: 1757400000000, ID: int64(second)},
		Pinned: []int64{int64(first)}}
}

// TestCursorRoundTrip: a cursor decodes, for its own sort and forum only, to
// what it was made from; the golden's example is what encode writes.
func TestCursorRoundTrip(t *testing.T) {
	if got, want := fixtureCursor(t).encode(), "YXwxfDE3NTc0MDAwMDAwMDB8NDU3NDE0MDA0MDg1NDExMDIxMHw0NTc0MTQwMDQwODU0MTEwMjA5"; got != want {
		t.Errorf("the golden's cursor is %q; encode writes %q", want, got)
	}
	for _, s := range []views.Sort{views.ByActivity, views.ByCreated} {
		for _, c := range []cursor{
			{Forum: 1, Key: views.Key{At: 0, ID: 1}},
			{Forum: 7, Key: views.Key{At: -5, ID: 7}, Pinned: []int64{3}},
			{Forum: 1 << 40, Key: views.Key{At: 1757400000000, ID: 1 << 62}, Pinned: []int64{1, 2, 3}},
		} {
			c.Sort = s
			text := c.encode()
			got, err := decodeCursor(s, c.Forum, text)
			if err != nil || got == nil || got.encode() != text || got.Key != c.Key || len(got.Pinned) != len(c.Pinned) {
				t.Errorf("%+v: %q decodes to %+v, %v", c, text, got, err)
			}
			other := views.ByCreated
			if s == views.ByCreated {
				other = views.ByActivity
			}
			if _, err := decodeCursor(other, c.Forum, text); err == nil {
				t.Errorf("%+v: accepted by the other sort", c)
			}
			if _, err := decodeCursor(s, c.Forum+1, text); err == nil {
				t.Errorf("%+v: accepted by another forum", c)
			}
		}
	}
	if c, err := decodeCursor(views.ByActivity, 1, ""); c != nil || err != nil {
		t.Errorf("empty: %+v %v", c, err)
	}
}
