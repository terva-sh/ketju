package sse

import (
	"testing"
	"time"
)

// TestStatsTakeNoLock: the metrics scrape reads Stats while a stream
// opening holds the hub's mutex, and does not wait for it; the counts it
// reads follow the streams (TKT-01M29CEE7S).
func TestStatsTakeNoLock(t *testing.T) {
	h := NewHub(HubOptions{Limits: Limits{Connections: 10, PerUser: 10, Anonymous: 10}})
	anon, err := h.open(nil, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.open(nil, nil, 7); err != nil {
		t.Fatal(err)
	}
	h.mu.Lock()
	done := make(chan Stats, 1)
	go func() { done <- h.Stats() }()
	select {
	case s := <-done:
		if s.Open != 2 || s.Anonymous != 1 {
			t.Errorf("Stats = %+v, want 2 open, 1 anonymous", s)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Stats waited on the hub's mutex")
	}
	h.mu.Unlock()
	h.release(anon)
	if s := h.Stats(); s.Open != 1 || s.Anonymous != 0 {
		t.Errorf("after a release Stats = %+v, want 1 open, 0 anonymous", s)
	}
}
