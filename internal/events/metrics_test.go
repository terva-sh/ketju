package events

import (
	"context"
	"testing"
	"time"
)

// TestStatsTakeNoLockOnceStarted: once the bus runs, the metrics scrape
// reads Stats while the bus's lock is held for writing, and does not wait
// for it (TKT-01M29CEE7S).
func TestStatsTakeNoLockOnceStarted(t *testing.T) {
	b := New(Options{})
	if err := b.Subscribe(SubscriberFunc{N: "x", F: func(context.Context, Batch) error { return nil }}, 0); err != nil {
		t.Fatal(err)
	}
	if err := b.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { b.Close(context.Background()) })
	b.mu.Lock()
	done := make(chan []Stat, 1)
	go func() { done <- b.Stats() }()
	select {
	case s := <-done:
		if len(s) != 1 || s[0].Name != "x" {
			t.Errorf("Stats = %+v, want the one subscriber", s)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Stats waited on the lock Publish takes")
	}
	b.mu.Unlock()
}
