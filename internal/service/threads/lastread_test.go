package threads

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/service/respcache"
	"github.com/terva-sh/ketju/internal/store/threadid"
)

// lastReadAt is the viewer block's last_read_at for v, or -1 for null.
func lastReadAt(t *testing.T, s *Service, v domain.Viewer, id threadid.ID) int64 {
	t.Helper()
	res, err := s.View(t.Context(), v, id, Query{Page: 1}, "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Body.Viewer.LastReadAt == nil {
		return -1
	}
	return int64(*res.Body.Viewer.LastReadAt)
}

func memberViewer(id int64) domain.Viewer {
	return domain.Viewer{UserID: id, Role: domain.RoleMember, Active: true, SessionKey: "s"}
}

// TestThreadViewerLastReadAt: the viewer block carries when the viewer's
// place in the thread was last set: null for an anonymous viewer and for a
// member with no position, then the time of each read beacon, including one
// that does not move the ordinal, and the time of a mark-unread
// (docs/06 "Landing on return", TKT-01M3S3ZS0H).
func TestThreadViewerLastReadAt(t *testing.T) {
	w := newRepliesWorld(t)
	c := w.cache()
	id := threadid.ID(uint64(1_757_500_000_000)<<16 | 9)
	starterThread(t, w, c, id)
	s := w.service(c, nil)
	clock := time.UnixMilli(1_757_600_000_000)
	s.Now = func() time.Time { return clock }
	alice := memberViewer(w.author)

	if got := lastReadAt(t, s, domain.Anonymous(), id); got != -1 {
		t.Errorf("an anonymous viewer's last_read_at is %d, want null", got)
	}
	if got := lastReadAt(t, s, alice, id); got != -1 {
		t.Errorf("with no position, last_read_at is %d, want null", got)
	}

	if _, err := s.MarkRead(t.Context(), alice, id, 1); err != nil {
		t.Fatal(err)
	}
	if got := lastReadAt(t, s, alice, id); got != clock.UnixMilli() {
		t.Errorf("after a read, last_read_at is %d, want %d", got, clock.UnixMilli())
	}

	clock = clock.Add(time.Hour)
	if pos, err := s.MarkRead(t.Context(), alice, id, 1); err != nil || pos.LastReadOrdinal != 1 {
		t.Fatalf("a second beacon at #1: %+v, %v", pos, err)
	}
	if got := lastReadAt(t, s, alice, id); got != clock.UnixMilli() {
		t.Errorf("after a beacon that does not move the ordinal, last_read_at is %d, want the later %d", got, clock.UnixMilli())
	}

	clock = clock.Add(time.Hour)
	if pos, err := s.MarkUnread(t.Context(), alice, id, nil); err != nil || pos.LastReadOrdinal != 0 {
		t.Fatalf("mark unread: %+v, %v", pos, err)
	}
	if got := lastReadAt(t, s, alice, id); got != clock.UnixMilli() {
		t.Errorf("after mark unread, last_read_at is %d, want the mark's %d", got, clock.UnixMilli())
	}
}

// TestThreadViewerLastReadAtIsPerViewer: two members who set their place at
// different times are served one cached copy of the page's posts, and each
// gets their own last_read_at, which never enters what the response cache
// holds.
func TestThreadViewerLastReadAtIsPerViewer(t *testing.T) {
	w := newRepliesWorld(t)
	c := w.cache()
	id := threadid.ID(uint64(1_757_500_000_000)<<16 | 10)
	bob := memberViewer(starterThread(t, w, c, id))
	alice := memberViewer(w.author)
	s := w.service(c, nil)
	s.Responses = respcache.New(1 << 20)
	clock := time.UnixMilli(1_757_600_000_000)
	s.Now = func() time.Time { return clock }

	if _, err := s.MarkRead(t.Context(), alice, id, 2); err != nil {
		t.Fatal(err)
	}
	aliceAt := clock.UnixMilli()
	clock = clock.Add(90 * time.Minute)
	if _, err := s.MarkRead(t.Context(), bob, id, 2); err != nil {
		t.Fatal(err)
	}
	bobAt := clock.UnixMilli()

	view := func(v domain.Viewer) dto.ThreadPage {
		t.Helper()
		res, err := s.View(t.Context(), v, id, Query{Page: 1}, "")
		if err != nil {
			t.Fatal(err)
		}
		return res.Body
	}
	a := view(alice)
	hits := s.Responses.Stats().Hits
	b := view(bob)
	if s.Responses.Stats().Hits != hits+1 {
		t.Fatalf("bob's page was not served from the cache alice's filled; stats %+v", s.Responses.Stats())
	}
	ap, _ := json.Marshal(a.Posts)
	bp, _ := json.Marshal(b.Posts)
	if !bytes.Equal(ap, bp) {
		t.Fatalf("the two members were served different posts\nalice %s\n  bob %s", ap, bp)
	}
	if a.Viewer.LastReadAt == nil || int64(*a.Viewer.LastReadAt) != aliceAt {
		t.Errorf("alice's last_read_at is %v, want %d", a.Viewer.LastReadAt, aliceAt)
	}
	if b.Viewer.LastReadAt == nil || int64(*b.Viewer.LastReadAt) != bobAt {
		t.Errorf("bob's last_read_at is %v, want %d, not alice's", b.Viewer.LastReadAt, bobAt)
	}
	if bytes.Contains(ap, []byte("last_read")) {
		t.Errorf("the cached posts carry a read position: %s", ap)
	}
}
