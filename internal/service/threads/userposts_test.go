package threads

import (
	"testing"

	"github.com/terva-sh/ketju/internal/store/db/views"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// TestHistoryShows is the history's rule for every entry state against
// every viewer: nothing in this build moves or hides a post, so the API
// tests cannot reach those two states, and this holds the rule for when
// something does.
func TestHistoryShows(t *testing.T) {
	const (
		deleted = threadlog.EntryDeleted
		moved   = threadlog.EntryMoved
		hidden  = threadlog.EntryHiddenByMod
	)
	for _, c := range []struct {
		name       string
		flags      threadlog.EntryFlags
		member     bool // another member
		own, staff bool
	}{
		{"visible", 0, true, true, true},
		{"edited", threadlog.EntryEdited, true, true, true},
		{"deleted", deleted, false, true, true},
		{"hidden", hidden, false, true, true},
		{"deleted and hidden", deleted | hidden, false, true, true},
		{"moved", moved, false, false, false},
		{"moved and deleted", moved | deleted, false, false, false},
	} {
		if got := historyShows(c.flags, false, false); got != c.member {
			t.Errorf("%s, another member: %t", c.name, got)
		}
		if got := historyShows(c.flags, false, true); got != c.own {
			t.Errorf("%s, its author: %t", c.name, got)
		}
		if got := historyShows(c.flags, true, false); got != c.staff {
			t.Errorf("%s, staff: %t", c.name, got)
		}
	}
}

// TestPostCursorRoundTrips: what the server writes, it reads back.
func TestPostCursorRoundTrips(t *testing.T) {
	for _, k := range []views.PostKey{{At: 0, Thread: 1, Ordinal: 1}, {At: 1757500000000, Thread: 115179520000000001, Ordinal: 212},
		{At: -5, Thread: 9, Ordinal: 4294967295}} {
		got, err := decodePostCursor(encodePostCursor(k))
		if err != nil || *got != k {
			t.Errorf("%+v came back %+v, %v", k, got, err)
		}
	}
	if got, err := decodePostCursor(""); got != nil || err != nil {
		t.Errorf("empty: %v, %v", got, err)
	}
}
